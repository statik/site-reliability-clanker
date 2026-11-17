package chatops

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/notify"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/signin"
	"github.com/statik/site-reliability-clanker/internal/signin/fakeidp"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// These tests use a fake Slack, a fake Bot Connector, a fake identity provider and test
// keys. Nothing here talks to the real Slack or the real Microsoft Teams.

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 11, 21, 2, 0, 0, 0, time.UTC) }

// world is a control plane with the sign-in, both chat apps and the fakes around them.
type world struct {
	t        *testing.T
	db       *sql.DB
	orch     *orchestrator.Orchestrator
	log      *audit.Log
	service  *Service
	provider *fakeidp.Provider
	// url is the address of the control plane.
	url   string
	slack *fakeSlack
	teams *fakeTeams
}

// recorded is one request that a fake got.
type recorded struct {
	method string
	path   string
	body   map[string]any
	text   string
}

// calls is the list of requests that a fake got.
type calls struct {
	mu   sync.Mutex
	list []recorded
}

func (c *calls) add(r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body) // a body that is not JSON stays in text
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = append(c.list, recorded{method: r.Method, path: r.URL.Path, body: body,
		text: string(raw)})
}

// to returns the requests to paths that end with suffix.
func (c *calls) to(suffix string) []recorded {
	c.mu.Lock()
	defer c.mu.Unlock()
	found := []recorded{}
	for _, call := range c.list {
		if strings.HasSuffix(call.path, suffix) {
			found = append(found, call)
		}
	}
	return found
}

func newWorld(t *testing.T) *world {
	t.Helper()
	db := storetest.Open(t)
	clock := fixedClock{}
	w := &world{t: t, db: db, log: audit.New(db, clock)}
	// The orchestrator reads the approval settings from the runbooks repository.
	f := forge.New(db)
	if err := f.Seed(orchestrator.RunbooksRepo, map[string]fs.FS{".": fstest.MapFS{
		"README.md": {Data: []byte("Runbooks\n")}}}, "Seed runbooks"); err != nil {
		t.Fatal(err)
	}
	w.orch = &orchestrator.Orchestrator{DB: db, Audit: w.log, Forge: f, Clock: clock}
	// An approval starts the apply in the background. The apply must end before the
	// database of the test goes away.
	t.Cleanup(w.settle)

	idpMux := http.NewServeMux()
	idp := httptest.NewServer(idpMux)
	t.Cleanup(idp.Close)
	provider, err := fakeidp.New(idp.URL)
	if err != nil {
		t.Fatal(err)
	}
	idpMux.Handle("/", provider.Handler())
	w.provider = provider

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	w.url = server.URL
	sessions, err := signin.New(context.Background(), db, signin.Config{Issuer: idp.URL,
		ClientID: "clanker", ClientSecret: "secret", PublicURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	w.service = New(db, w.orch, w.log, server.URL)
	w.service.Sessions = sessions
	mux.Handle("/auth/chat-link", w.service.LinkHandler())
	mux.Handle("/auth/", sessions.Handler())
	w.slack = newFakeSlack(t, w, mux)
	w.teams = newFakeTeams(t, w, mux)

	for _, u := range [][4]string{{"alice", "Alice Ng", "approver", "alice@example.com"},
		{"bob", "Bob Okafor", "approver", "bob@example.com"},
		{"dave", "Dave Lindqvist", "viewer", "dave@example.com"}} {
		if _, err := db.Exec(`INSERT INTO users (name, display, role, email) VALUES ($1,$2,$3,$4)`,
			u[0], u[1], u[2], u[3]); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// settle waits until no work item is between the approval and the end of its apply. The
// apply writes the last state before the audit event of that state, so an apply has ended
// only when that event is in the log.
func (w *world) settle() {
	moving := []string{orchestrator.StateApproved, orchestrator.StateApplying,
		orchestrator.StateVerifying}
	ended := []string{orchestrator.StateDone, orchestrator.StateFailed,
		orchestrator.StateRolledBack}
	w.eventually("every apply ended", func() bool {
		items, err := w.orch.List()
		if err != nil {
			return false
		}
		changes := w.events("workitem.state_changed")
		return !slices.ContainsFunc(items, func(wi orchestrator.WorkItem) bool {
			recorded := slices.ContainsFunc(changes, func(e audit.Event) bool {
				return e.WorkItem == wi.ID && e.Data["to"] == wi.State
			})
			return slices.Contains(moving, wi.State) ||
				(slices.Contains(ended, wi.State) && !recorded)
		})
	})
}

func (w *world) eventually(what string, condition func() bool) {
	w.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	w.t.Fatalf("did not happen in 10 seconds: %s", what)
}

// workItem makes a work item that waits for approval, with one saved plan that changes
// nothing, so that an apply after the approval ends at once.
func (w *world) workItem(id, tier string) {
	w.t.Helper()
	if _, err := w.db.Exec(`INSERT INTO work_items (id, title, kind, env, layers, state, tier,
		commit_sha, created_at, updated_at) VALUES ($1, 'Upgrade Paperboat', 'app-upgrade', 'local',
		'["50-app"]', $2, $3, 'c0ffee', 'then', 'then')`, id, orchestrator.StateAwaitingApproval,
		tier); err != nil {
		w.t.Fatal(err)
	}
	w.plan(id, "first-plan")
}

// plan saves a new plan for a work item. The plans before it are superseded.
func (w *world) plan(id, sha string) {
	w.t.Helper()
	if _, err := w.db.Exec(`UPDATE plans SET superseded = 1 WHERE work_item = $1`, id); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.db.Exec(`INSERT INTO plans (work_item, layer, commit_sha, plan_sha256, plan,
		plan_json, created_at) VALUES ($1, '50-app', 'c0ffee', $2, '', '{}', 'then')`, id,
		sha); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) state(id string) string {
	w.t.Helper()
	wi, err := w.orch.Get(id)
	if err != nil {
		w.t.Fatal(err)
	}
	return wi.State
}

// decisions returns who decided what on a work item, for example "alice:approve".
func (w *world) decisions(id string) []string {
	w.t.Helper()
	approvals, err := w.orch.Approvals(id)
	if err != nil {
		w.t.Fatal(err)
	}
	decisions := []string{}
	for _, a := range approvals {
		decisions = append(decisions, a.Approver+":"+a.Decision)
	}
	return decisions
}

// events returns the audit events of one type.
func (w *world) events(eventType string) []audit.Event {
	w.t.Helper()
	events, err := w.log.Query(audit.Filter{Types: []string{eventType}})
	if err != nil {
		w.t.Fatal(err)
	}
	return events
}

// announcement is the notification of a request for approval, as the notifier makes it.
func announcement(id string) notify.Message {
	return notify.Message{Event: "approval.requested", WorkItem: id,
		Title: "Approval needed: Upgrade Paperboat", Sentence: "Upgrade Paperboat to 1.1.0.",
		Facts: [][2]string{{"Work item", id}, {"Environment", "local"}}}
}

// browser is a person in a browser with cookies.
type browser struct {
	t      *testing.T
	w      *world
	client *http.Client
}

// signedIn returns a browser where the person with this email is signed in.
func (w *world) signedIn(email string) *browser {
	w.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		w.t.Fatal(err)
	}
	b := &browser{t: w.t, w: w, client: &http.Client{Jar: jar}}
	w.provider.SignInAs(fakeidp.Person{Email: email, Verified: true})
	if status, _ := b.get(w.url + "/auth/login?return=/auth/chat-link"); status != http.StatusOK {
		w.t.Fatalf("the sign-in of %s answered %d", email, status)
	}
	return b
}

func read(t *testing.T, response *http.Response, err error) (int, string) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}

func (b *browser) get(address string) (int, string) {
	b.t.Helper()
	response, err := b.client.Get(address)
	return read(b.t, response, err)
}

// post sends a form to the link page as a page of origin does.
func (b *browser) post(form url.Values, origin string) (int, string) {
	b.t.Helper()
	request, err := http.NewRequest(http.MethodPost, b.w.url+"/auth/chat-link",
		strings.NewReader(form.Encode()))
	if err != nil {
		b.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", origin)
	response, err := b.client.Do(request)
	return read(b.t, response, err)
}

// confirm opens the link page of a link address and confirms the link.
func (b *browser) confirm(link string) (int, string) {
	b.t.Helper()
	if status, page := b.get(link); status != http.StatusOK {
		b.t.Fatalf("the link page answered %d: %s", status, page)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		b.t.Fatal(err)
	}
	return b.post(url.Values{"code": {parsed.Query().Get("code")}}, b.w.url)
}

var linkAddress = regexp.MustCompile(`https?://[^"|>\\ ]+/auth/chat-link\?code=[A-Za-z0-9_-]+`)

// chat is one chat platform as the tests drive it. The same cases run on Slack and on
// Teams through it.
type chat interface {
	name() string
	// mark is a text that a message has only while it carries the decision actions.
	mark() string
	// send gives a notification to the adapter of the platform.
	send(m notify.Message) error
	// fire clicks a decision on the newest request of the work item, as a person.
	fire(person, id, decision string)
	// answers returns each answer that only the person who clicked sees.
	answers() []string
	// offers reports whether the newest posted request carries the decision actions.
	offers(id string) bool
	// closed returns the content of each change of a posted request.
	closed() []string
}

func (w *world) chats() []chat { return []chat{w.slack, w.teams} }

// Channel and the two methods below make a fake the notify.Adapter of its platform.
func (f *fakeSlack) Channel() string           { return f.adapter.Channel() }
func (f *fakeSlack) Targets(a string) []string { return f.adapter.Targets(a) }
func (f *fakeSlack) Send(ctx context.Context, a string, m notify.Message) error {
	return f.adapter.Send(ctx, a, m)
}
func (f *fakeTeams) Channel() string           { return f.adapter.Channel() }
func (f *fakeTeams) Targets(a string) []string { return f.adapter.Targets(a) }
func (f *fakeTeams) Send(ctx context.Context, a string, m notify.Message) error {
	return f.adapter.Send(ctx, a, m)
}

// each runs a case once for Slack and once for Teams, each in a new world.
func each(t *testing.T, run func(t *testing.T, w *world, c chat)) {
	for _, platform := range []string{"slack", "teams"} {
		t.Run(platform, func(t *testing.T) {
			w := newWorld(t)
			for _, c := range w.chats() {
				if c.name() == platform {
					run(t, w, c)
				}
			}
		})
	}
}

// announce posts the request for approval of a work item.
func (w *world) announce(c chat, id string) {
	w.t.Helper()
	if err := c.send(announcement(id)); err != nil {
		w.t.Fatalf("%s: post the request for approval: %v", c.name(), err)
	}
}

// click clicks a decision and returns the answer that only the person sees.
func (w *world) click(c chat, person, id, decision string) string {
	w.t.Helper()
	before := len(c.answers())
	c.fire(person, id, decision)
	w.eventually(c.name()+" answers the click", func() bool { return len(c.answers()) > before })
	return c.answers()[before]
}

// link links the chat account of a person on a platform to the email of the person. It
// clicks once to get the link address, then confirms in a browser.
func (w *world) link(c chat, person, id string) {
	w.t.Helper()
	answer := w.click(c, person, id, "approve")
	address := linkAddress.FindString(answer)
	if address == "" {
		w.t.Fatalf("%s: the answer to a click with no link has no link address: %s", c.name(),
			answer)
	}
	_, page := w.signedIn(person + "@example.com").confirm(address)
	if !strings.Contains(page, "The account is linked") {
		w.t.Fatalf("%s: the confirmation answered: %s", c.name(), page)
	}
}

// check reports a failed expectation and lets the test go on.
func check(t *testing.T, ok bool, format string, args ...any) {
	t.Helper()
	if !ok {
		t.Errorf(format, args...)
	}
}

// has reports whether text contains every part.
func has(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
