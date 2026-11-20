package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// application is a server that answers the paths of the second pack. The status of each
// path can change during a test.
type application struct {
	status map[string]int
	seen   map[string]int
}

func (a *application) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.seen[r.URL.Path]++
	if r.URL.Path == "/api/version" {
		_, _ = w.Write([]byte(`{"version":"3.4.0"}`))
		return
	}
	status, known := a.status[r.URL.Path]
	if !known {
		status = http.StatusNotFound
	}
	w.WriteHeader(status)
}

// deployed returns a monitor of the second pack with its application at a test server.
func deployed(t *testing.T, app pack.Pack, status map[string]int) (*Monitor, *application) {
	t.Helper()
	served := &application{status: status, seen: map[string]int{}}
	server := httptest.NewServer(served)
	t.Cleanup(server.Close)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	m := New(storetest.Open(t), fixedClock{now})
	m.App = app
	err := store.SetLayerOutputs(m.DB, "local", app.Layer, map[string]any{
		app.URLOutput: server.URL, app.VersionVariable: "3.4.0"}, store.FormatTime(now))
	if err != nil {
		t.Fatal(err)
	}
	return m, served
}

func TestVerifyPassesWhenEachCheckOfThePackPasses(t *testing.T) {
	app := packtest.Second(t)
	m, served := deployed(t, app, map[string]int{"/api/healthz": 200, "/v2/berths": 200})
	result, err := m.Verify(context.Background(), orchestrator.WorkItem{Env: "local"})
	if err != nil || !result.OK {
		t.Fatalf("Verify: %+v, %v", result, err)
	}
	checks := strings.Join(result.Checks, "; ")
	for _, want := range []string{"check ready answers 200", "running version is 3.4.0",
		"40 of 40 berth requests"} {
		if !strings.Contains(checks, want) {
			t.Errorf("the checks do not have %q: %s", want, checks)
		}
	}
	if served.seen["/v2/berths"] != 40 || served.seen["/api/healthz"] != 1 {
		t.Errorf("requests: %v", served.seen)
	}
}

func TestTheCanaryFailsWhenThePrimaryCheckFails(t *testing.T) {
	m, _ := deployed(t, packtest.Second(t), map[string]int{"/api/healthz": 200,
		"/v2/berths": 500})
	result, err := m.Verify(context.Background(), orchestrator.WorkItem{Env: "local"})
	if err != nil || result.OK || !strings.Contains(result.Reason, "canary failed") {
		t.Errorf("Verify: %+v, %v", result, err)
	}
}

func TestACheckThatIsNotPrimaryMustPassBeforeTheCanary(t *testing.T) {
	m, served := deployed(t, packtest.Second(t), map[string]int{"/api/healthz": 503,
		"/v2/berths": 200})
	base := m.AppURL("local")
	if got := m.failedCheck(context.Background(), base); got != "check ready answers 503" {
		t.Errorf("failedCheck = %q", got)
	}
	served.status["/api/healthz"] = 200
	if got := m.failedCheck(context.Background(), base); got != "" {
		t.Errorf("failedCheck = %q after the check recovered", got)
	}
	if served.seen["/v2/berths"] != 0 {
		t.Error("failedCheck sent a request to the primary check")
	}
}

func TestACheckPassesWithTheStatusThatThePackGives(t *testing.T) {
	app := packtest.Second(t)
	for index := range app.Checks {
		app.Checks[index].Status = 204
	}
	m, _ := deployed(t, app, map[string]int{"/api/healthz": 204, "/v2/berths": 204})
	sample, err := m.Probe(context.Background(), "local", 5)
	if err != nil || sample.OK != 5 || sample.Version != "3.4.0" {
		t.Errorf("Probe with the status 204: %+v, %v", sample, err)
	}
	if got := m.failedCheck(context.Background(), m.AppURL("local")); got != "" {
		t.Errorf("failedCheck = %q", got)
	}
	m, _ = deployed(t, app, map[string]int{"/api/healthz": 200, "/v2/berths": 200})
	if sample, _ := m.Probe(context.Background(), "local", 5); sample.OK != 0 {
		t.Errorf("the status 200 passed a check that wants 204: %+v", sample)
	}
}

func TestVerifyPassesWhenTheApplicationLayerHasNoOutputs(t *testing.T) {
	m := New(storetest.Open(t), fixedClock{time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)})
	m.App = packtest.Second(t)
	result, err := m.Verify(context.Background(), orchestrator.WorkItem{Env: "local"})
	if err != nil || !result.OK || len(result.Checks) != 1 {
		t.Errorf("Verify: %+v, %v", result, err)
	}
}

func TestVerifyFailsWhenTheApplicationLayerHasOutputsAndNoAddress(t *testing.T) {
	app := packtest.Second(t)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for _, outputs := range []map[string]any{{app.VersionVariable: "3.4.0"},
		{app.VersionVariable: "3.4.0", app.URLOutput: ""}} {
		m := New(storetest.Open(t), fixedClock{now})
		m.App = app
		err := store.SetLayerOutputs(m.DB, "local", app.Layer, outputs, store.FormatTime(now))
		if err != nil {
			t.Fatal(err)
		}
		result, err := m.Verify(context.Background(), orchestrator.WorkItem{Env: "local"})
		if err != nil || result.OK || !strings.Contains(result.Reason, app.Layer) ||
			!strings.Contains(result.Reason, "no output "+app.URLOutput) {
			t.Errorf("Verify with the outputs %v: %+v, %v", outputs, result, err)
		}
	}
}
