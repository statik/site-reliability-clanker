package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// heldRunner answers a job only when the test releases it: with one output for each read,
// or with err. A job whose context ends first fails with the error of the context.
type heldRunner struct {
	started chan struct{}
	release chan struct{}
	err     error
}

func newHeldRunner() *heldRunner {
	return &heldRunner{started: make(chan struct{}), release: make(chan struct{})}
}

func (h *heldRunner) Run(ctx context.Context, job jobspec.Job) (jobspec.Result, error) {
	close(h.started)
	select {
	case <-h.release:
	case <-ctx.Done():
		return jobspec.Result{}, ctx.Err()
	}
	if h.err != nil {
		return jobspec.Result{}, h.err
	}
	outputs := make([]jobspec.CommandOutput, len(job.Spec.Reads))
	for i := range outputs {
		outputs[i].Output = fmt.Sprintf("output of read %d", i)
	}
	return jobspec.Result{Status: jobspec.StatusOK, Commands: outputs}, nil
}

// emptyIssuer gives an empty credential, as the local target does for a read-only grant.
type emptyIssuer struct{}

func (emptyIssuer) Issue(context.Context, broker.Grant, time.Duration) (broker.Credential,
	error) {
	return broker.Credential{}, nil
}

func (emptyIssuer) Withdraw(context.Context, broker.Grant) error { return nil }

// withDebugSessions returns the handler of a demo server whose debug sessions run on the
// runner, and its orchestrator. The user carol is an admin.
func withDebugSessions(t *testing.T, runner *heldRunner) (http.Handler,
	*orchestrator.Orchestrator) {
	t.Helper()
	db := storetest.Open(t)
	if _, err := db.Exec(`INSERT INTO users (name, display, role, email) VALUES
		('carol', 'Carol Reyes', 'admin', 'carol@example.com')`); err != nil {
		t.Fatal(err)
	}
	clock := fixedClock{runbooksNow}
	key, err := broker.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := broker.New(db, clock, key, emptyIssuer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetLayerOutputs(db, "local", pack.TargetsLayer,
		map[string]any{pack.TargetsOutput: map[string]any{"application": "shop",
			"data": "shop-data"}}, store.FormatTime(runbooksNow)); err != nil {
		t.Fatal(err)
	}
	log := audit.New(db, clock)
	f := forge.New(db)
	orch := &orchestrator.Orchestrator{DB: db, Audit: log, Forge: f, Broker: b, Clock: clock,
		App: packtest.Example(t), Runners: runner,
		Envs: map[string]orchestrator.EnvConfig{"local": {Name: "local", Cloud: "local"}}}
	handler, err := (&Server{Forge: f, Orch: orch, Audit: log}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler, orch
}

// waitForEvent waits for the first event of a type that passes the test. The test waits for
// the last event that a session writes, so that no write follows the end of the test.
func waitForEvent(t *testing.T, orch *orchestrator.Orchestrator, eventType string,
	test func(audit.Event) bool) audit.Event {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range eventsOf(t, orch, eventType) {
			if test(e) {
				return e
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no %s event in 10 s", eventType)
	return audit.Event{}
}

// startDebugSession sends a request for a debug session and cancels its context as soon as
// the answer is there, as a client that goes away does. It fails when the answer waits for
// the runner.
func startDebugSession(t *testing.T, handler http.Handler,
	runner *heldRunner) orchestrator.WorkItem {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest("POST", "/api/v1/debug-sessions",
		strings.NewReader(`{"env":"local","reason":"slow pages"}`)).WithContext(ctx)
	request.Header.Set("X-Clanker-User", "carol")
	answer := httptest.NewRecorder()
	answered := make(chan struct{})
	go func() {
		handler.ServeHTTP(answer, request)
		close(answered)
	}()
	select {
	case <-answered:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("the answer waited for the end of the session")
	}
	cancel()
	if answer.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", answer.Code, answer.Body)
	}
	var wi orchestrator.WorkItem
	if err := json.Unmarshal(answer.Body.Bytes(), &wi); err != nil {
		t.Fatal(err)
	}
	if wi.ID == "" || wi.Kind != "debug-session" || wi.Env != "local" {
		t.Fatalf("answer = %s, want the work item of the session", answer.Body)
	}
	select {
	case <-runner.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not reach the runner")
	}
	return wi
}

func TestADebugSessionAnswersAtOnceAndEndsAfterItsRequest(t *testing.T) {
	runner := newHeldRunner()
	handler, orch := withDebugSessions(t, runner)
	wi := startDebugSession(t, handler, runner)
	close(runner.release)
	waitForEvent(t, orch, "workitem.state_changed", func(e audit.Event) bool {
		return e.WorkItem == wi.ID && e.Data["to"] == orchestrator.StateDone
	})
	jobs, err := orch.Jobs(wi.ID)
	if err != nil || len(jobs) != 1 || jobs[0].Kind != "debug" || jobs[0].Status != "succeeded" {
		t.Fatalf("jobs = %+v, %v; want one debug job that succeeded", jobs, err)
	}
	transcript, err := orch.JobLog(jobs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "$ kubectl get -n shop pods -o wide\noutput of read 0\n"
	if !strings.Contains(transcript, want) {
		t.Errorf("the log of the debug job has no %q: %q", want, transcript)
	}
}

func TestAFailedDebugSessionEndsFailedWithTheReason(t *testing.T) {
	runner := newHeldRunner()
	runner.err = errors.New("the runner task did not start in time")
	handler, orch := withDebugSessions(t, runner)
	wi := startDebugSession(t, handler, runner)
	close(runner.release)
	failed := waitForEvent(t, orch, "task.failed", func(e audit.Event) bool {
		return strings.Contains(e.Summary, "debug session "+wi.ID)
	})
	if !strings.Contains(failed.Summary, "did not start in time") {
		t.Errorf("task.failed = %q, want the reason", failed.Summary)
	}
	ended, err := orch.Get(wi.ID)
	if err != nil || ended.State != orchestrator.StateFailed {
		t.Errorf("state = %s, %v; want Failed", ended.State, err)
	}
	changes := eventsOf(t, orch, "workitem.state_changed")
	last := changes[len(changes)-1]
	if reason, _ := last.Data["reason"].(string); !strings.Contains(reason,
		"did not start in time") {
		t.Errorf("the change to Failed has the reason %q, want the error", reason)
	}
}
