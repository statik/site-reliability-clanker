package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// gated returns the handler of a server where people sign in. who is the answer of the
// sign-in for every request.
func gated(t *testing.T, who func() (orchestrator.User, error)) http.Handler {
	t.Helper()
	server := &Server{Forge: forge.New(storetest.Open(t)),
		Identify: func(*http.Request) (orchestrator.User, error) { return who() },
		SignIn: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}),
		Runners: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})}
	handler, err := server.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func ask(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	// With a sign-in, the header of the demo must mean nothing.
	request.Header.Set("X-Clanker-User", "carol")
	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, request)
	return answer
}

func TestWithoutASignInTheGateRefusesEverythingButTheOpenPaths(t *testing.T) {
	handler := gated(t, func() (orchestrator.User, error) { return orchestrator.User{}, ErrNotSignedIn })
	cases := []struct {
		method, path string
		want         int
	}{
		{"GET", "/healthz", http.StatusOK},
		{"GET", "/auth/login", http.StatusTeapot},
		{"POST", "/runner/v1/job", http.StatusNoContent},
		{"GET", "/api/v1/auth", http.StatusUnauthorized},
		{"GET", "/api/v1/work-items", http.StatusUnauthorized},
		{"GET", "/api/v1/events/stream", http.StatusUnauthorized},
		{"POST", "/api/v1/sim/scenes/drift", http.StatusUnauthorized},
		{"GET", "/git/runbooks.git/info/refs?service=git-upload-pack", http.StatusUnauthorized},
		{"POST", "/git/runbooks.git/git-receive-pack", http.StatusUnauthorized},
		{"POST", "/", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		if got := ask(handler, tc.method, tc.path).Code; got != tc.want {
			t.Errorf("%s %s answered %d, want %d", tc.method, tc.path, got, tc.want)
		}
	}
	page := ask(handler, "GET", "/work-items/WI-0001?tab=plan")
	if page.Code != http.StatusFound ||
		page.Header().Get("Location") != "/auth/login?return=%2Fwork-items%2FWI-0001%3Ftab%3Dplan" {
		t.Errorf("a page answered %d with Location %q", page.Code, page.Header().Get("Location"))
	}
}

func TestTheGateRefusesASignedInPersonWithNoUser(t *testing.T) {
	handler := gated(t, func() (orchestrator.User, error) {
		return orchestrator.User{}, forbidden(errors.New("mallory@example.com has no role here"))
	})
	answer := ask(handler, "GET", "/api/v1/auth")
	if answer.Code != http.StatusForbidden || !strings.Contains(answer.Body.String(), "has no role") {
		t.Errorf("a person with no user: %d %s", answer.Code, answer.Body)
	}
	if got := ask(handler, "GET", "/work-items").Code; got != http.StatusForbidden {
		t.Errorf("a page for a person with no user answered %d", got)
	}
}

func TestTheGateLetsAUserThroughAndChecksTheRole(t *testing.T) {
	viewer := orchestrator.User{Name: "dave@example.com", Role: "viewer"}
	handler := gated(t, func() (orchestrator.User, error) { return viewer, nil })
	answer := ask(handler, "GET", "/api/v1/auth")
	if answer.Code != http.StatusOK || !strings.Contains(answer.Body.String(), `"sign_in":true`) {
		t.Errorf("a signed-in user: %d %s", answer.Code, answer.Body)
	}
	// A viewer can read a repository with the git client and cannot push.
	for _, tc := range []struct{ method, path string }{
		{"GET", "/git/runbooks.git/info/refs?service=git-receive-pack"},
		{"POST", "/git/runbooks.git/git-receive-pack"}} {
		if got := ask(handler, tc.method, tc.path).Code; got != http.StatusForbidden {
			t.Errorf("%s %s by a viewer answered %d, want 403", tc.method, tc.path, got)
		}
	}
	read := ask(handler, "GET", "/git/runbooks.git/info/refs?service=git-upload-pack").Code
	if read == http.StatusForbidden || read == http.StatusUnauthorized {
		t.Errorf("a read with the git client by a viewer answered %d", read)
	}
	// A viewer cannot start what the demo leaves open to everybody.
	for _, path := range []string{"/api/v1/sim/scenes/drift", "/api/v1/sim/advance",
		"/api/v1/notifications/digest", "/api/v1/forge/pulls"} {
		if got := ask(handler, "POST", path).Code; got != http.StatusForbidden {
			t.Errorf("POST %s by a viewer answered %d, want 403", path, got)
		}
	}
}

func TestTheDemoHasNoGate(t *testing.T) {
	server := &Server{Forge: forge.New(storetest.Open(t))}
	handler, err := server.Handler()
	if err != nil {
		t.Fatal(err)
	}
	answer := ask(handler, "GET", "/api/v1/auth")
	if answer.Code != http.StatusOK || !strings.Contains(answer.Body.String(), `"sign_in":false`) {
		t.Errorf("the demo: %d %s", answer.Code, answer.Body)
	}
	if got := ask(handler, "GET", "/healthz").Code; got != http.StatusOK {
		t.Errorf("the health path answered %d", got)
	}
}
