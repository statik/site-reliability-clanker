package chatops

import (
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/store"
)

// linkedTeams returns a world where alice is linked in Teams and a tier T2 change waits,
// so that a click that gets through would approve.
func linkedTeams(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.workItem("WI-0001", "T2")
	w.announce(w.teams, "WI-0001")
	w.link(w.teams, "alice", "WI-0001")
	return w
}

func TestTeamsRefusesAnActivityWithNoValidTokenOfTheConnector(t *testing.T) {
	w := linkedTeams(t)
	f := w.teams
	unknown := testKey(t)
	now := time.Now()
	// Each token differs from a valid one in one thing.
	set := func(claim string, value any) func(map[string]any) {
		return func(claims map[string]any) { claims[claim] = value }
	}
	cases := []struct {
		name   string
		key    *rsa.PrivateKey
		keyID  string
		change func(map[string]any)
	}{
		{"another audience", f.key, "teams-key", set("aud", "another-app-id")},
		{"another issuer", f.key, "teams-key", set("iss", "https://sts.windows.net/tenant/")},
		{"an expired token", f.key, "teams-key", set("exp", now.Add(-6*time.Minute).Unix())},
		{"a token with no expiry", f.key, "teams-key",
			func(claims map[string]any) { delete(claims, "exp") }},
		{"a token that is not valid yet", f.key, "teams-key", set("nbf", now.Add(6*time.Minute).Unix())},
		{"another serviceUrl", f.key, "teams-key", set("serviceurl", "https://evil.example")},
		{"no serviceUrl", f.key, "teams-key",
			func(claims map[string]any) { delete(claims, "serviceurl") }},
		{"a key that the connector does not publish", unknown, "teams-key", nil},
		{"a key ID that the connector does not publish", unknown, "unknown-key", nil},
		{"a key that is endorsed for another channel", f.otherChannelKey, "other-key", nil},
	}
	click := f.clickActivity("alice", "WI-0001", "approve")
	for _, tc := range cases {
		status, body := f.deliver(click, f.token(tc.key, tc.keyID, tc.change))
		if status != http.StatusUnauthorized {
			t.Errorf("%s: answered %d, want 401: %s", tc.name, status, body)
		}
	}
	if status, _ := f.deliver(click, ""); status != http.StatusUnauthorized {
		t.Errorf("an activity with no token answered %d, want 401", status)
	}
	if got := w.decisions("WI-0001"); len(got) != 0 {
		t.Fatalf("a refused activity decided: %v", got)
	}
	// The same activity with a valid token decides. A token with the claim name of the
	// documentation, serviceUrl, is valid too.
	valid := f.token(f.key, "teams-key", func(claims map[string]any) {
		claims["serviceUrl"] = claims["serviceurl"]
		delete(claims, "serviceurl")
	})
	if status, body := f.deliver(click, valid); status != http.StatusOK ||
		!strings.Contains(body, "is recorded") {
		t.Fatalf("the activity with a valid token: %d %s", status, body)
	}
	if got := strings.Join(w.decisions("WI-0001"), " "); got != "alice:approve" {
		t.Errorf("decisions = %q", got)
	}
}

func TestTeamsRefusesAnActivityOfAnotherTenantOrChannel(t *testing.T) {
	w := linkedTeams(t)
	f := w.teams
	valid := f.token(f.key, "teams-key", nil)
	for name, change := range map[string]func(map[string]any){
		"another tenant": func(a map[string]any) {
			a["channelData"] = map[string]any{"tenant": map[string]any{"id": "another-tenant"}}
			a["conversation"] = map[string]any{"id": "19:x", "tenantId": "another-tenant"}
		},
		"two different tenants": func(a map[string]any) {
			a["channelData"] = map[string]any{"tenant": map[string]any{"id": "another-tenant"}}
		},
		"no tenant": func(a map[string]any) {
			delete(a, "channelData")
			a["conversation"] = map[string]any{"id": "19:x"}
		},
	} {
		click := f.clickActivity("alice", "WI-0001", "approve")
		change(click)
		if status, _ := f.deliver(click, valid); status != http.StatusForbidden {
			t.Errorf("%s: answered %d, want 403", name, status)
		}
	}
	// A key for another channel does not make an activity of that channel a Teams click.
	click := f.clickActivity("alice", "WI-0001", "approve")
	click["channelId"] = "webchat"
	if status, _ := f.deliver(click, f.token(f.otherChannelKey, "other-key", nil)); status !=
		http.StatusForbidden {
		t.Errorf("an activity of another channel answered %d, want 403", status)
	}
	// A click that names no person decides nothing.
	click = f.clickActivity("alice", "WI-0001", "approve")
	click["from"] = map[string]any{"id": "29:alice", "name": "alice"}
	if status, body := f.deliver(click, valid); status != http.StatusOK ||
		!strings.Contains(body, "names no person") {
		t.Errorf("a click with no Entra object ID: %d %s", status, body)
	}
	if got := w.decisions("WI-0001"); len(got) != 0 {
		t.Errorf("a refused activity decided: %v", got)
	}
}

func TestTeamsAnswersAClickWithACardForThatPerson(t *testing.T) {
	w := newWorld(t)
	w.workItem("WI-0001", "T2")
	w.announce(w.teams, "WI-0001")
	var answer struct {
		StatusCode int    `json:"statusCode"`
		Type       string `json:"type"`
		Value      struct {
			Actions []map[string]any `json:"actions"`
		} `json:"value"`
	}
	raw := w.click(w.teams, "alice", "WI-0001", "approve")
	if err := json.Unmarshal([]byte(raw), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.StatusCode != 200 || answer.Type != "application/vnd.microsoft.card.adaptive" {
		t.Errorf("the answer to the invoke: %s", raw)
	}
	// The card takes the place of the request for this person. It keeps the decisions,
	// so that the person can click again after the link.
	titles := []string{}
	for _, action := range answer.Value.Actions {
		title, _ := action["title"].(string)
		titles = append(titles, title)
	}
	want := "Approve, Reject, Open in S.R.C., Link the account"
	if got := strings.Join(titles, ", "); got != want {
		t.Errorf("the actions of the private card: %s", got)
	}
	if n := len(w.teams.closed()); n != 0 {
		t.Errorf("a click with no link changed the request for everybody %d times", n)
	}
}

func TestTeamsPostsOnlyAfterTheBotWasAddedToATeam(t *testing.T) {
	w := newWorld(t)
	f := w.teams
	w.workItem("WI-0001", "T2")
	if err := store.SetKV(w.db, conversationKey, ""); err != nil {
		t.Fatal(err)
	}
	err := f.send(announcement("WI-0001"))
	if err == nil || !strings.Contains(err.Error(), "add the app to a team") {
		t.Fatalf("error = %v, want one that says to add the app to a team", err)
	}
	// An activity that is not valid does not teach the bot a channel.
	added := f.activity("alice", "installationUpdate")
	added["action"] = "add"
	added["serviceUrl"] = "https://evil.example"
	status, _ := f.deliver(added, f.token(f.key, "teams-key", nil))
	check(t, status == http.StatusUnauthorized, "another serviceUrl answered %d", status)
	check(t, f.send(announcement("WI-0001")) != nil, "the bot learned a channel from an "+
		"activity that is not valid")

	added["serviceUrl"] = f.url
	status, _ = f.deliver(added, f.token(f.key, "teams-key", nil))
	check(t, status == http.StatusOK, "the activity that adds the bot answered %d", status)
	if err := f.send(announcement("WI-0001")); err != nil {
		t.Fatalf("post after the bot was added: %v", err)
	}
	post := f.calls.to("/v3/conversations")[0].body
	channelData, _ := post["channelData"].(map[string]any)
	channel, _ := channelData["channel"].(map[string]any)
	check(t, channel["id"] == "19:changes@thread.tacv2", "the channel of the new thread: %v", post)
	check(t, post["tenantId"] == teamsTenant, "the tenant of the new thread: %v", post)
}
