package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/api"
	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/chatops"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/signin"
	"github.com/statik/site-reliability-clanker/internal/signin/fakeidp"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// clearChat removes every setting that buildChat reads, so that the environment of the
// person who runs the tests changes nothing.
func clearChat(t *testing.T) {
	t.Helper()
	for _, name := range []string{slackSigningSecretEnv, slackBotTokenEnv, slackChannelEnv,
		slackAPIURLEnv, teamsAppIDEnv, teamsAppSecretEnv, teamsTenantIDEnv, issuerEnv, clientIDEnv,
		clientSecretEnv, publicURLEnv, adminEmailEnv} {
		t.Setenv(name, "")
	}
}

func TestWithNoChatSettingsNoChatAppIsBuilt(t *testing.T) {
	clearChat(t)
	apps, err := buildChat(&components{}, options{cloud: "local"})
	if apps != nil || err != nil {
		t.Fatalf("buildChat = %v, %v; want no app and no error", apps, err)
	}
}

func TestAChatAppWithSomeSettingsStopsTheStart(t *testing.T) {
	clearChat(t)
	t.Setenv(slackBotTokenEnv, "xoxb-test")
	_, err := buildChat(&components{}, options{cloud: "local"})
	if err == nil || !strings.Contains(err.Error(), slackSigningSecretEnv) ||
		!strings.Contains(err.Error(), slackChannelEnv) || strings.Contains(err.Error(), "xoxb") {
		t.Fatalf("error = %v, want the names of the missing settings and no secret", err)
	}
	clearChat(t)
	t.Setenv(teamsAppIDEnv, "app")
	t.Setenv(teamsTenantIDEnv, "tenant")
	if _, err := buildChat(&components{}, options{cloud: "local"}); err == nil ||
		!strings.Contains(err.Error(), teamsAppSecretEnv) {
		t.Fatalf("error = %v, want the name of the missing setting", err)
	}
}

func TestAChatAppWithNoSignInStopsTheStart(t *testing.T) {
	clearChat(t)
	t.Setenv(slackSigningSecretEnv, "secret")
	t.Setenv(slackBotTokenEnv, "xoxb-test")
	t.Setenv(slackChannelEnv, "#changes")
	_, err := buildChat(&components{}, options{cloud: "local"})
	if err == nil || !strings.Contains(err.Error(), "the sign-in is not") ||
		!strings.Contains(err.Error(), issuerEnv) {
		t.Fatalf("error = %v, want one that asks for the sign-in", err)
	}
}

// served returns the handler of a control plane with the sign-in and a Slack app, wired as
// `up` wires them.
func served(t *testing.T) http.Handler {
	t.Helper()
	idpMux := http.NewServeMux()
	idp := httptest.NewServer(idpMux)
	t.Cleanup(idp.Close)
	provider, err := fakeidp.New(idp.URL)
	if err != nil {
		t.Fatal(err)
	}
	idpMux.Handle("/", provider.Handler())
	db := storetest.Open(t)
	orch := &orchestrator.Orchestrator{DB: db}
	sessions, err := signin.New(context.Background(), db, signin.Config{Issuer: idp.URL,
		ClientID: "clanker", ClientSecret: "secret", PublicURL: "https://clanker.example"})
	if err != nil {
		t.Fatal(err)
	}
	service := chatops.New(db, orch, audit.New(db, nil), "https://clanker.example")
	apps, err := newChatApps(service, map[string]string{slackSigningSecretEnv: "secret",
		slackBotTokenEnv: "xoxb-test", slackChannelEnv: "#changes"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := &api.Server{Orch: orch, Forge: forge.New(db), Identify: identify(sessions, orch),
		SignIn: sessions.Handler()}
	apps.serve(server, sessions)
	handler, err := server.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestTheWiringServesTheLinkPageTheSignInAndTheConfiguredApp(t *testing.T) {
	handler := served(t)
	cases := []struct {
		method, path string
		status       int
		// want is a part of the Location header or of the body.
		want string
	}{
		// The link page asks for a browser session itself.
		{"GET", "/auth/chat-link?code=abc", http.StatusFound,
			"/auth/login?return=%2Fauth%2Fchat-link"},
		{"POST", "/auth/chat-link", http.StatusUnauthorized, "Sign in to the dashboard first"},
		// The sign-in still answers below /auth/.
		{"GET", "/auth/login", http.StatusFound, "/authorize"},
		{"POST", chatops.SlackPath, http.StatusUnauthorized, "no valid signature of Slack"},
		// The Teams bot is not configured, so nothing answers on its path.
		{"POST", chatops.TeamsPath, http.StatusNotFound, "not found"},
	}
	for _, tc := range cases {
		answer := httptest.NewRecorder()
		handler.ServeHTTP(answer, httptest.NewRequest(tc.method, tc.path, nil))
		text := answer.Header().Get("Location") + answer.Body.String()
		if answer.Code != tc.status || !strings.Contains(text, tc.want) {
			t.Errorf("%s %s: %d %s; want %d with %q", tc.method, tc.path, answer.Code, text,
				tc.status, tc.want)
		}
	}
}
