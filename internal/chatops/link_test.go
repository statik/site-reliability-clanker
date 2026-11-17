package chatops

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// pendingLink returns a world with a request for approval, and the link address that the
// first click of alice in Slack got.
func pendingLink(t *testing.T) (*world, string) {
	t.Helper()
	w := newWorld(t)
	w.workItem("WI-0001", "T2")
	w.announce(w.slack, "WI-0001")
	address := linkAddress.FindString(w.click(w.slack, "alice", "WI-0001", "approve"))
	if address == "" {
		t.Fatal("the first click got no link address")
	}
	return w, address
}

func (w *world) links() int {
	w.t.Helper()
	var n int
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM chat_links`).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func code(address string) url.Values {
	parsed, _ := url.Parse(address)
	return url.Values{"code": {parsed.Query().Get("code")}}
}

// programPost confirms a link code as a program does: with a token of the device flow and
// no browser session. It returns the status.
func programPost(t *testing.T, w *world, address string) int {
	t.Helper()
	sum := sha256.Sum256([]byte("program-token"))
	if _, err := w.db.Exec(`INSERT INTO tokens (id, email, expires, created_at) VALUES ($1,
		'alice@example.com', $2, $2)`, hex.EncodeToString(sum[:]),
		store.FormatTime(time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, w.url+"/auth/chat-link",
		strings.NewReader(code(address).Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Authorization", "Bearer program-token")
	response, err := http.DefaultClient.Do(request)
	status, _ := read(t, response, err)
	return status
}

func TestOnlyAPersonInABrowserOfThisSiteLinks(t *testing.T) {
	w, address := pendingLink(t)
	status := programPost(t, w, address)
	check(t, status == http.StatusUnauthorized, "a program token answered %d, want 401", status)

	// A person who is not signed in goes to the sign-in first.
	plain := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := plain.Get(address)
	status, _ = read(t, response, err)
	check(t, status == http.StatusFound, "the link page with no sign-in answered %d", status)
	check(t, strings.HasPrefix(response.Header.Get("Location"), "/auth/login?return="),
		"the link page with no sign-in sends to %s", response.Header.Get("Location"))

	// A page of another site cannot confirm with the session of the person.
	browser := w.signedIn("alice@example.com")
	status, _ = browser.post(code(address), "https://evil.example")
	check(t, status == http.StatusForbidden, "another site answered %d, want 403", status)
	// To open the page changes nothing.
	status, _ = browser.get(address)
	check(t, status == http.StatusOK, "the link page answered %d", status)
	if n := w.links(); n != 0 {
		t.Fatalf("%d links exist and nobody confirmed in a browser of this site", n)
	}
	// The same code still works for the person in the browser.
	status, _ = browser.post(code(address), w.url)
	check(t, status == http.StatusOK, "the confirmation answered %d", status)
	check(t, w.links() == 1, "%d links exist, want 1", w.links())
}

func TestALinkCodeWorksOnce(t *testing.T) {
	w, address := pendingLink(t)
	browser := w.signedIn("alice@example.com")
	status, _ := browser.confirm(address)
	check(t, status == http.StatusOK, "the confirmation answered %d", status)
	status, page := browser.post(code(address), w.url)
	check(t, status == http.StatusNotFound, "a second use of the code answered %d", status)
	check(t, has(page, "not known, was used"), "a second use of the code: %s", page)
	events := w.events("chat.linked")
	if len(events) != 1 {
		t.Fatalf("link events: %+v", events)
	}
	check(t, events[0].Actor == "alice@example.com", "the actor is %s", events[0].Actor)
	// The audit log never holds the code.
	all, err := w.log.Query(audit.Filter{Search: code(address).Get("code")})
	check(t, err == nil, "search the audit log: %v", err)
	check(t, len(all) == 0, "the audit log holds the link code")
}

func TestALinkCodeEndsAfterTenMinutes(t *testing.T) {
	w, address := pendingLink(t)
	w.service.now = func() time.Time { return time.Now().Add(linkLife + time.Minute) }
	browser := w.signedIn("alice@example.com")
	status, _ := browser.get(address)
	check(t, status == http.StatusNotFound, "the page of an old code answered %d", status)
	status, _ = browser.post(code(address), w.url)
	check(t, status == http.StatusNotFound, "the confirmation of an old code answered %d", status)
	check(t, w.links() == 0, "an old code linked")
	// A short time before the end the code still works.
	w.service.now = func() time.Time { return time.Now().Add(linkLife - 5*time.Second) }
	status, _ = browser.post(code(address), w.url)
	check(t, status == http.StatusOK, "the confirmation inside ten minutes answered %d", status)
}

func TestAnAccountHasOneLinkCodeAtATime(t *testing.T) {
	w, first := pendingLink(t)
	second := linkAddress.FindString(w.click(w.slack, "alice", "WI-0001", "approve"))
	browser := w.signedIn("alice@example.com")
	if status, _ := browser.post(code(first), w.url); status != http.StatusNotFound {
		t.Errorf("the code before the newest one answered %d, want 404", status)
	}
	if status, _ := browser.post(code(second), w.url); status != http.StatusOK {
		t.Errorf("the newest code answered %d", status)
	}
}

func TestAPersonWithNoUserLinksAndCannotApprove(t *testing.T) {
	w := newWorld(t)
	w.workItem("WI-0001", "T2")
	w.announce(w.slack, "WI-0001")
	address := linkAddress.FindString(w.click(w.slack, "mallory", "WI-0001", "approve"))
	browser := w.signedIn("mallory@example.com")
	_, page := browser.get(address)
	check(t, has(page, "has no role in this control plane"), "the link page: %s", page)
	status, _ := browser.post(code(address), w.url)
	check(t, status == http.StatusOK, "the confirmation answered %d", status)
	check(t, w.links() == 1, "%d links exist, want 1", w.links())
	answer := w.click(w.slack, "mallory", "WI-0001", "approve")
	check(t, has(answer, "mallory@example.com has no role"), "the answer: %s", answer)
	check(t, len(w.decisions("WI-0001")) == 0, "a person with no user decided")
	check(t, len(w.events("chat.refused")) == 1, "want one audit event for the refusal")
}

func TestAPersonRemovesOnlyTheOwnLink(t *testing.T) {
	w := newWorld(t)
	w.workItem("WI-0001", "T2")
	w.announce(w.slack, "WI-0001")
	w.link(w.slack, "alice", "WI-0001")
	unlink := url.Values{"action": {"unlink"}, "platform": {"slack"}, "workspace": {"T0ACME"},
		"account": {"UALICE"}}
	status, _ := w.signedIn("bob@example.com").post(unlink, w.url)
	check(t, status == http.StatusNotFound, "the removal by another person answered %d", status)
	check(t, w.links() == 1, "another person removed the link")

	alice := w.signedIn("alice@example.com")
	_, page := alice.get(w.url + "/auth/chat-link")
	check(t, has(page, "UALICE"), "the page does not list the linked account: %s", page)
	status, _ = alice.post(unlink, w.url)
	check(t, status == http.StatusOK, "the removal answered %d", status)
	check(t, w.links() == 0, "the link is still there")
	check(t, len(w.events("chat.unlinked")) == 1, "want one audit event for the removal")

	// After the removal a click asks for a link again and decides nothing.
	answer := w.click(w.slack, "alice", "WI-0001", "approve")
	check(t, linkAddress.FindString(answer) != "", "after the removal: %s", answer)
	check(t, len(w.decisions("WI-0001")) == 0, "a click after the removal decided")
}
