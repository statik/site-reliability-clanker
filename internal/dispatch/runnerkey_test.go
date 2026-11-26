package dispatch_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/dispatch"
)

// refusals keeps what Dispatcher.Refused learns.
type refusals struct {
	mu   sync.Mutex
	seen []dispatch.Refusal
}

func (r *refusals) add(refusal dispatch.Refusal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, refusal)
}

func (r *refusals) list() []dispatch.Refusal {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]dispatch.Refusal(nil), r.seen...)
}

// The routes after the first fetch, each with a request that works for the runner of the
// job.
var runnerRoutes = []struct {
	method, path, body string
	want               int
}{
	{http.MethodGet, "job/checkout", "", http.StatusOK},
	{http.MethodGet, "job/plan", "", http.StatusOK},
	{http.MethodPut, "job/plan", "a plan file", http.StatusNoContent},
	{http.MethodPut, "job/plan-json", `{"resource_changes":[]}`, http.StatusNoContent},
	{http.MethodPost, "job/log", "hello carol\n", http.StatusNoContent},
	{http.MethodPost, "job/heartbeat", "", http.StatusNoContent},
	{http.MethodPost, "job/result", `{"status":"ok"}`, http.StatusNoContent},
}

func TestEveryRouteAfterTheFetchNeedsTheRunnerKeyOfThatFetch(t *testing.T) {
	for _, route := range runnerRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			d, l, server := setup(t)
			l.silent = true
			seen := &refusals{}
			d.Refused = seen.add
			done := runInBackground(d, runbook(t, "hello", &bytes.Buffer{}))
			token := waitForLaunch(t, l)
			send := func(runnerKey string) int {
				status, _, _ := callWithKey(t, server, token, runnerKey, route.method, route.path,
					route.body)
				return status
			}

			if status := send(""); status != http.StatusForbidden {
				t.Errorf("before the fetch, without a key: status %d, want 403", status)
			}
			if status := send("a-key-of-its-own"); status != http.StatusForbidden {
				t.Errorf("before the fetch, with a made-up key: status %d, want 403", status)
			}
			runnerKey := fetch(t, server, token)
			if status, _ := call(t, server, token, http.MethodGet, "job", ""); status !=
				http.StatusConflict {
				t.Errorf("a second fetch: status %d, want 409", status)
			}
			if status := send(""); status != http.StatusForbidden {
				t.Errorf("after the fetch, without a key: status %d, want 403", status)
			}
			if status := send(runnerKey + "x"); status != http.StatusForbidden {
				t.Errorf("after the fetch, with a wrong key: status %d, want 403", status)
			}
			if status := send(runnerKey); status != route.want {
				t.Errorf("after the fetch, with the key: status %d, want %d", status, route.want)
			}
			if route.path != "job/result" {
				status, _, _ := callWithKey(t, server, token, runnerKey, http.MethodPost,
					"job/result", `{"status":"ok"}`)
				if status != http.StatusNoContent {
					t.Fatalf("the result of the runner: status %d", status)
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			checkRefusals(t, seen.list(), route.method+" /runner/v1/"+route.path, token,
				runnerKey)
		})
	}
}

// checkRefusals checks that the refusals of one route are the two refused requests before
// the fetch, the second fetch and the two refused requests after it, in order, and that none
// holds a secret.
func checkRefusals(t *testing.T, seen []dispatch.Refusal, request, token, runnerKey string) {
	t.Helper()
	want := []struct{ request, reason string }{
		{request, "the job was not fetched yet"},
		{request, "the job was not fetched yet"},
		{"GET /runner/v1/job", "the job was given to a runner already"},
		{request, "the request has no runner key"},
		{request, "the runner key is not the one that the first fetch of the job gave"},
	}
	if len(seen) != len(want) {
		t.Fatalf("Refused learned of %d requests, want %d: %+v", len(seen), len(want), seen)
	}
	for index, refusal := range seen {
		if refusal.Request != want[index].request || refusal.Reason != want[index].reason {
			t.Errorf("refusal %d = %q: %q, want %q: %q", index, refusal.Request, refusal.Reason,
				want[index].request, want[index].reason)
		}
		if refusal.Kind != "runbook" || refusal.Env != "local" ||
			refusal.CheckoutID != "runbooks/abc" || refusal.Remote == "" {
			t.Errorf("refusal %d does not name the job and the caller: %+v", index, refusal)
		}
		text := strings.Join([]string{refusal.Request, refusal.Remote, refusal.Reason}, " ")
		if strings.Contains(text, token) || strings.Contains(text, runnerKey) {
			t.Errorf("refusal %d holds the token or the runner key: %+v", index, refusal)
		}
	}
}

// A holder of the token alone cannot keep a job alive whose runner went quiet, before or
// after the first fetch.
func TestARefusedRequestIsNoSignOfLifeOfTheRunner(t *testing.T) {
	cases := []struct {
		name       string
		fetchFirst bool
		want       string
	}{
		{"before the fetch", false, "made no request"},
		{"after the fetch", true, "said nothing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, l, server := setup(t)
			l.silent = true
			d.Silence, d.StartGrace = 1500*time.Millisecond, 1500*time.Millisecond
			done := runInBackground(d, runbook(t, "hello", &bytes.Buffer{}))
			token := waitForLaunch(t, l)
			if c.fetchFirst {
				fetch(t, server, token)
			}
			ctx, stop := context.WithTimeout(context.Background(), 6*time.Second)
			defer stop()
			for {
				select {
				case err := <-done:
					if err == nil || !strings.Contains(err.Error(), c.want) {
						t.Fatalf("error = %v, want %q", err, c.want)
					}
					return
				case <-ctx.Done():
					t.Fatal("heartbeats without the runner key kept the job alive")
				case <-time.After(200 * time.Millisecond):
					call(t, server, token, http.MethodPost, "job/heartbeat", "")
				}
			}
		})
	}
}

func TestRefusedLearnsOfAtMostTenRefusalsOfAJob(t *testing.T) {
	d, l, server := setup(t)
	l.silent = true
	seen := &refusals{}
	d.Refused = seen.add
	done := runInBackground(d, runbook(t, "hello", &bytes.Buffer{}))
	token := waitForLaunch(t, l)
	runnerKey := fetch(t, server, token)
	for range 15 {
		if status, _ := call(t, server, token, http.MethodPost, "job/heartbeat", ""); status !=
			http.StatusForbidden {
			t.Fatalf("a heartbeat without the key: status %d, want 403", status)
		}
	}
	status, _, _ := callWithKey(t, server, token, runnerKey, http.MethodPost, "job/result",
		`{"status":"ok"}`)
	if status != http.StatusNoContent {
		t.Fatalf("the result of the runner: status %d", status)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := len(seen.list()); got != 10 {
		t.Fatalf("Refused learned of %d refusals, want 10", got)
	}
}
