package chatops

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// linkedSlack returns a world where alice is linked in Slack and a tier T2 change waits,
// so that a click that gets through would approve.
func linkedSlack(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.workItem("WI-0001", "T2")
	w.announce(w.slack, "WI-0001")
	w.link(w.slack, "alice", "WI-0001")
	return w
}

func TestSlackRefusesARequestThatSlackDidNotSignNow(t *testing.T) {
	w := linkedSlack(t)
	body := w.slack.clickBody("alice", "WI-0001", "approve")
	stamp := func(age time.Duration) string {
		return strconv.FormatInt(time.Now().Add(-age).Unix(), 10)
	}
	now, old, future := stamp(0), stamp(6*time.Minute), stamp(-6*time.Minute)
	cases := []struct {
		name    string
		body    string
		headers map[string]string
	}{
		{"no signature", body, map[string]string{"X-Slack-Request-Timestamp": now}},
		{"no timestamp", body, map[string]string{
			"X-Slack-Signature": slackSignature(slackSecret, now, body)}},
		{"another secret", body, map[string]string{"X-Slack-Request-Timestamp": now,
			"X-Slack-Signature": slackSignature("another-secret", now, body)}},
		{"a timestamp older than five minutes", body, map[string]string{
			"X-Slack-Request-Timestamp": old,
			"X-Slack-Signature":         slackSignature(slackSecret, old, body)}},
		{"a timestamp from the future", body, map[string]string{
			"X-Slack-Request-Timestamp": future,
			"X-Slack-Signature":         slackSignature(slackSecret, future, body)}},
		{"a timestamp that is not a number", body, map[string]string{
			"X-Slack-Request-Timestamp": "now",
			"X-Slack-Signature":         slackSignature(slackSecret, "now", body)}},
		{"a body changed after signing", strings.Replace(body, "UALICE", "UBOB", 1),
			map[string]string{"X-Slack-Request-Timestamp": now,
				"X-Slack-Signature": slackSignature(slackSecret, now, body)}},
		{"a signature of another timestamp", body, map[string]string{
			"X-Slack-Request-Timestamp": now,
			"X-Slack-Signature":         slackSignature(slackSecret, stamp(time.Minute), body)}},
	}
	answers := len(w.slack.answers())
	for _, tc := range cases {
		if status := w.slack.deliver(tc.body, tc.headers); status != http.StatusUnauthorized {
			t.Errorf("%s: answered %d, want 401", tc.name, status)
		}
	}
	// The decision of an accepted click comes after the answer. The valid click below is
	// handled after every refused one, so its decision is the only one.
	answer := w.click(w.slack, "alice", "WI-0001", "approve")
	if !strings.Contains(answer, "is recorded") {
		t.Fatalf("a signed click after the refused ones: %s", answer)
	}
	if got := strings.Join(w.decisions("WI-0001"), " "); got != "alice:approve" {
		t.Errorf("decisions = %q, want only the one of the signed click", got)
	}
	if n := len(w.slack.answers()) - answers; n != 1 {
		t.Errorf("the refused requests got %d private answers", n-1)
	}
}

func TestSlackAnswersOnlyThePersonWhoClicked(t *testing.T) {
	w := newWorld(t)
	w.workItem("WI-0001", "T2")
	w.announce(w.slack, "WI-0001")
	w.click(w.slack, "alice", "WI-0001", "approve")
	answer := w.slack.calls.to("/response")[0].body
	// Without these two fields the answer could take the place of the request for everybody.
	if answer["response_type"] != "ephemeral" || answer["replace_original"] != false {
		t.Errorf("the private answer is not ephemeral or replaces the request: %v", answer)
	}
	if n := len(w.slack.closed()); n != 0 {
		t.Errorf("a click with no link changed the request %d times", n)
	}
}

func TestSlackIgnoresAClickThatIsNoDecision(t *testing.T) {
	w := linkedSlack(t)
	answers := len(w.slack.answers())
	for _, body := range []string{
		// The link button also sends a payload.
		w.slack.payload("alice", "clanker_open", "WI-0001"),
		w.slack.payload("alice", "another_action", "WI-0001 binding"),
		// A decision with no binding is not a click on a posted request.
		w.slack.payload("alice", slackApprove, "WI-0001"),
	} {
		if status := w.slack.deliver(body, signedHeaders(body)); status != http.StatusOK {
			t.Errorf("a signed payload that is no decision answered %d, want 200", status)
		}
	}
	body := "payload=not-json"
	if status := w.slack.deliver(body, signedHeaders(body)); status != http.StatusBadRequest {
		t.Errorf("a signed body that is no payload answered %d, want 400", status)
	}
	answer := w.click(w.slack, "alice", "WI-0001", "reject")
	if !strings.Contains(answer, "is recorded") {
		t.Fatalf("the click after the ignored ones: %s", answer)
	}
	if got := strings.Join(w.decisions("WI-0001"), " "); got != "alice:reject" {
		t.Errorf("decisions = %q, want only the rejection", got)
	}
	if n := len(w.slack.answers()) - answers; n != 1 {
		t.Errorf("the ignored clicks got %d private answers", n-1)
	}
}

func TestSlackSaysWhatFailedWhenSlackRefusesTheToken(t *testing.T) {
	w := newWorld(t)
	w.workItem("WI-0001", "T2")
	adapter, err := NewSlack(w.service, SlackConfig{SigningSecret: slackSecret,
		BotToken: "xoxb-wrong", Channel: "#changes", APIURL: w.slack.url + "/api"})
	if err != nil {
		t.Fatal(err)
	}
	err = adapter.Send(t.Context(), "approvers", announcement("WI-0001"))
	if err == nil || !strings.Contains(err.Error(), "invalid_auth") ||
		strings.Contains(err.Error(), "xoxb-wrong") {
		t.Errorf("error = %v, want the reason of Slack and no token", err)
	}
	if _, err := NewSlack(w.service, SlackConfig{BotToken: "xoxb", Channel: "#c"}); err == nil {
		t.Error("an app with no signing secret was accepted")
	}
}
