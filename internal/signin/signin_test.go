package signin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/signin/fakeidp"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// site is a control plane that serves only the sign-in and one page that says who is
// signed in.
type site struct {
	service  *Service
	provider *fakeidp.Provider
	url      string
	browser  *http.Client
}

func newSite(t *testing.T) *site {
	t.Helper()
	idpMux := http.NewServeMux()
	idp := httptest.NewServer(idpMux)
	t.Cleanup(idp.Close)
	provider, err := fakeidp.New(idp.URL)
	if err != nil {
		t.Fatal(err)
	}
	idpMux.Handle("/", provider.Handler())

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	service, err := New(context.Background(), storetest.Open(t), Config{Issuer: idp.URL,
		ClientID: "clanker", ClientSecret: "secret", PublicURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	mux.Handle("/auth/", service.Handler())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		email, err := service.Email(r)
		if errors.Is(err, ErrNoSession) {
			http.Error(w, "nobody", http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		io.WriteString(w, email+" at "+r.URL.Path) //nolint:errcheck // a test page
	})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &site{service: service, provider: provider, url: server.URL,
		browser: &http.Client{Jar: jar}}
}

func (s *site) get(t *testing.T, path string) (int, string) {
	t.Helper()
	response, err := s.browser.Get(s.url + path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, strings.TrimSpace(string(body))
}

func TestSignInGivesASessionAndGoesBackToThePage(t *testing.T) {
	s := newSite(t)
	if status, _ := s.get(t, "/approvals"); status != http.StatusUnauthorized {
		t.Fatalf("before the sign-in the page answered %d", status)
	}
	s.provider.SignInAs(fakeidp.Person{Email: "Alice@Example.com", Verified: true})
	// The return path has a dot, as the version of a release has.
	status, body := s.get(t, "/auth/login?return=/work/WI-0001.2")
	if status != http.StatusOK || body != "alice@example.com at /work/WI-0001.2" {
		t.Fatalf("after the sign-in: %d %q", status, body)
	}
	if status, body := s.get(t, "/other"); status != http.StatusOK || body != "alice@example.com at /other" {
		t.Fatalf("the session did not last: %d %q", status, body)
	}
}

func TestSignInRefusesAnEmailThatTheProviderDidNotConfirm(t *testing.T) {
	s := newSite(t)
	s.provider.SignInAs(fakeidp.Person{Email: "mallory@example.com", Verified: false})
	status, body := s.get(t, "/auth/login")
	if status != http.StatusForbidden || !strings.Contains(body, "did not confirm an email") {
		t.Fatalf("an email that is not confirmed: %d %q", status, body)
	}
	if status, _ := s.get(t, "/"); status != http.StatusUnauthorized {
		t.Fatalf("a refused sign-in gave a session: %d", status)
	}
}

func TestCallbackRefusesAnAnswerThatThisBrowserDidNotAskFor(t *testing.T) {
	s := newSite(t)
	s.provider.SignInAs(fakeidp.Person{Email: "alice@example.com", Verified: true})
	status, body := s.get(t, "/auth/callback?code=stolen&state=guess")
	if status != http.StatusForbidden || !strings.Contains(body, "did not start a sign-in") {
		t.Fatalf("a callback with no sign-in: %d %q", status, body)
	}
	// A browser that started a sign-in refuses an answer with another state.
	s.browser.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if status, _ := s.get(t, "/auth/login"); status != http.StatusFound {
		t.Fatalf("login answered %d, want a redirect", status)
	}
	status, body = s.get(t, "/auth/callback?code=stolen&state=guess")
	if status != http.StatusForbidden || !strings.Contains(body, "does not belong to this sign-in") {
		t.Fatalf("a callback with a wrong state: %d %q", status, body)
	}
}

func TestSignInGoesOnlyToAPathOfThisSite(t *testing.T) {
	for target, want := range map[string]string{"/approvals?x=1": "/approvals?x=1", "": "/",
		"https://evil.example/": "/", "//evil.example/": "/", "/\\evil.example": "/", "/\t/evil.example": "/",
		"/\n/evil.example": "/"} {
		if got := localPath(target); got != want {
			t.Errorf("localPath(%q) = %q, want %q", target, got, want)
		}
	}
}

func TestASessionEndsAtLogoutAndAfterItsLife(t *testing.T) {
	s := newSite(t)
	s.provider.SignInAs(fakeidp.Person{Email: "alice@example.com", Verified: true})
	if status, _ := s.get(t, "/auth/login"); status != http.StatusOK {
		t.Fatalf("sign-in answered %d", status)
	}
	s.service.now = func() time.Time { return time.Now().Add(sessionLife + time.Minute) }
	if status, _ := s.get(t, "/"); status != http.StatusUnauthorized {
		t.Fatalf("a session after its life answered %d", status)
	}
	s.service.now = time.Now
	if status, _ := s.get(t, "/"); status != http.StatusOK {
		t.Fatalf("the session inside its life answered %d", status)
	}
	response, err := s.browser.Post(s.url+"/auth/logout", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout answered %d", response.StatusCode)
	}
	if status, _ := s.get(t, "/"); status != http.StatusUnauthorized {
		t.Fatalf("after the logout the page answered %d", status)
	}
}

func TestNewFailsWhenTheProviderDoesNotAnswer(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	_, err := New(context.Background(), storetest.Open(t), Config{Issuer: closed.URL,
		ClientID: "clanker", ClientSecret: "secret", PublicURL: "https://clanker.example"})
	if err == nil || !strings.Contains(err.Error(), "discovery document") {
		t.Fatalf("error = %v, want one about the discovery document", err)
	}
}
