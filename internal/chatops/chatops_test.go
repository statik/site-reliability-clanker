package chatops

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/notify"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
)

// firstClick clicks as a person whose account has no link. Nothing must be decided and no
// audit event must be left. It returns the link address of the private answer.
func firstClick(t *testing.T, w *world, c chat) string {
	t.Helper()
	answer := w.click(c, "alice", "WI-0001", "approve")
	address := linkAddress.FindString(answer)
	if address == "" || !strings.Contains(answer, "Nothing was decided") {
		t.Fatalf("the answer to a click with no link: %s", answer)
	}
	check(t, len(w.decisions("WI-0001")) == 0, "a click with no link decided")
	events := len(w.events("chat.linked")) + len(w.events("approval.refused")) +
		len(w.events("chat.refused"))
	check(t, events == 0, "a click with no link left %d audit events", events)
	return address
}

// decisionEvent checks that the audit event of the decision has the user and the chat
// account.
func decisionEvent(t *testing.T, w *world, c chat) {
	t.Helper()
	events := w.events("approval.recorded")
	if len(events) != 1 {
		t.Fatalf("approval events: %+v", events)
	}
	origin, _ := events[0].Data["origin"].(map[string]any)
	account, _ := origin["account"].(string)
	workspace, _ := origin["workspace"].(string)
	check(t, events[0].Actor == "alice", "the actor is %s, want alice", events[0].Actor)
	check(t, origin["platform"] == c.name(), "the event does not name the platform: %v", origin)
	check(t, account != "", "the event does not name the chat account: %v", origin)
	check(t, workspace != "", "the event does not name the workspace or tenant: %v", origin)
}

func TestAClickDecidesAsTheLinkedPersonAndTheRequestCloses(t *testing.T) {
	each(t, func(t *testing.T, w *world, c chat) {
		w.workItem("WI-0001", "T2")
		w.announce(c, "WI-0001")
		check(t, c.offers("WI-0001"), "the request for a tier T2 change has no decision action")
		address := firstClick(t, w, c)

		// The person signs in, sees the chat account and the email, and confirms.
		browser := w.signedIn("alice@example.com")
		_, page := browser.get(address)
		check(t, has(page, "alice@example.com", "Link this account", "alice"),
			"the link page does not show the account and the email: %s", page)
		_, page = browser.confirm(address)
		check(t, has(page, "The account is linked"), "the confirmation answered: %s", page)

		answer := w.click(c, "alice", "WI-0001", "approve")
		check(t, has(answer, "is recorded", "Alice Ng"), "the answer to a linked approver: %s", answer)
		decisions := strings.Join(w.decisions("WI-0001"), " ")
		check(t, decisions == "alice:approve", "decisions = %q, want one by alice", decisions)
		decisionEvent(t, w, c)

		// The posted request changes: it shows who decided and has no action.
		w.eventually("the request closes", func() bool { return len(c.closed()) == 1 })
		closed := c.closed()[0]
		check(t, has(closed, "Approved by Alice Ng at 2026-11-21 02:00 UTC"), "closed: %s", closed)
		check(t, !has(closed, c.mark()), "the closed request has an action: %s", closed)
		w.eventually("the change is applied", func() bool {
			return w.state("WI-0001") == orchestrator.StateDone
		})

		// A click on the decided change decides nothing.
		answer = w.click(c, "alice", "WI-0001", "approve")
		check(t, has(answer, "does not wait for approval"), "a click on a decided change: %s", answer)
		check(t, len(w.decisions("WI-0001")) == 1, "a click on a decided change decided again")
	})
}

func TestARejectionInChatRejectsTheChange(t *testing.T) {
	each(t, func(t *testing.T, w *world, c chat) {
		w.workItem("WI-0001", "T2")
		w.announce(c, "WI-0001")
		w.link(c, "alice", "WI-0001")
		if answer := w.click(c, "alice", "WI-0001", "reject"); !strings.Contains(answer, "is recorded") {
			t.Fatalf("the answer to a rejection: %s", answer)
		}
		if got := w.state("WI-0001"); got != orchestrator.StateRejected {
			t.Fatalf("the state is %s, want Rejected", got)
		}
		w.eventually("the request closes", func() bool { return len(c.closed()) == 1 })
		if closed := c.closed()[0]; !strings.Contains(closed, "Rejected by Alice Ng") {
			t.Errorf("the closed request: %s", closed)
		}
	})
}

func TestChatRefusesWhatOnlyTheDashboardDecides(t *testing.T) {
	cases := []struct {
		name, person, tier string
		// before runs after the request is posted and the person is linked.
		before func(w *world)
		want   string
	}{
		{name: "a viewer", person: "dave", tier: "T2", want: "has role viewer"},
		{name: "a tier T3 change", person: "alice", tier: "T3", want: "tier T3"},
		{name: "a tier that is not known", person: "alice", tier: "T9", want: "tier T9"},
		{name: "a request of an older plan", person: "alice", tier: "T2",
			before: func(w *world) { w.plan("WI-0001", "second-plan") },
			want:   "plan of WI-0001 changed"},
		{name: "a change that does not wait", person: "alice", tier: "T2",
			before: func(w *world) {
				if _, err := w.orch.Decide(context.Background(), "WI-0001", orchestrator.Decision{
					User: "bob", Decision: "request_changes", Comment: "not now"}); err != nil {
					w.t.Fatal(err)
				}
			}, want: "does not wait for approval"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			each(t, func(t *testing.T, w *world, c chat) {
				w.workItem("WI-0001", tc.tier)
				w.announce(c, "WI-0001")
				check(t, c.offers("WI-0001") == (tc.tier == "T2"),
					"the request of a tier %s change offers a decision: %v", tc.tier, c.offers("WI-0001"))
				w.link(c, tc.person, "WI-0001")
				if tc.before != nil {
					tc.before(w)
				}
				before := len(w.decisions("WI-0001"))
				answer := w.click(c, tc.person, "WI-0001", "approve")
				check(t, has(answer, tc.want, "/work/WI-0001"),
					"the answer lacks %q or the link to the page: %s", tc.want, answer)
				check(t, len(w.decisions("WI-0001")) == before, "the refused click decided")
				refusals := len(w.events("approval.refused")) + len(w.events("chat.refused"))
				check(t, refusals == 1, "want one audit event for the refusal, got %d", refusals)
			})
		})
	}
}

func TestTwoClicksAtTheSameMomentDecideOnce(t *testing.T) {
	each(t, func(t *testing.T, w *world, c chat) {
		w.workItem("WI-0001", "T2")
		w.announce(c, "WI-0001")
		w.link(c, "alice", "WI-0001")
		before := len(c.answers())
		var clicks sync.WaitGroup
		for range 2 {
			clicks.Add(1)
			go func() {
				defer clicks.Done()
				c.fire("alice", "WI-0001", "approve")
			}()
		}
		clicks.Wait()
		w.eventually("both clicks are answered", func() bool { return len(c.answers()) == before+2 })
		if got := strings.Join(w.decisions("WI-0001"), " "); got != "alice:approve" {
			t.Fatalf("decisions = %q, want one approval", got)
		}
		answers := strings.Join(c.answers()[before:], "\n")
		if strings.Count(answers, "is recorded") != 1 ||
			!strings.Contains(answers, "does not wait for approval") {
			t.Errorf("want one recorded decision and one refusal, got: %s", answers)
		}
		if n := len(w.events("apply.error")); n != 0 {
			t.Errorf("the second click started a second apply: %d errors", n)
		}
	})
}

func TestTheRequestClosesWhenTheChangeIsDecidedElsewhere(t *testing.T) {
	each(t, func(t *testing.T, w *world, c chat) {
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go w.service.Run(ctx)
		w.workItem("WI-0001", "T2")
		w.workItem("WI-0002", "T2")
		w.announce(c, "WI-0001")
		w.announce(c, "WI-0002")
		// A decision on the dashboard goes to the orchestrator with no chat.
		if _, err := w.orch.Decide(ctx, "WI-0001", orchestrator.Decision{User: "bob",
			Decision: "reject", Comment: "wrong week"}); err != nil {
			t.Fatal(err)
		}
		w.eventually("the decided request closes", func() bool { return len(c.closed()) == 1 })
		if closed := c.closed()[0]; !strings.Contains(closed, "Rejected by Bob Okafor") ||
			strings.Contains(closed, c.mark()) || !strings.Contains(closed, "WI-0001") {
			t.Errorf("the closed request: %s", closed)
		}
		// A new plan replaces the request of the other change.
		w.plan("WI-0002", "second-plan")
		w.service.Sweep(ctx)
		if closed := c.closed(); len(closed) != 2 || !strings.Contains(closed[1], "The plan changed") {
			t.Errorf("the request of the older plan: %v", closed)
		}
		// A closed request is not changed again.
		w.service.Sweep(ctx)
		if n := len(c.closed()); n != 2 {
			t.Errorf("a second sweep changed %d requests more", n-2)
		}
	})
}

func TestOtherNotificationsArePostedByTheAppWithNoDecision(t *testing.T) {
	each(t, func(t *testing.T, w *world, c chat) {
		before := len(c.closed())
		if err := c.send(notify.Message{Event: "apply.failed", WorkItem: "WI-0001",
			Title: "Apply of 50-app failed", Link: w.url + "/work/WI-0001",
			Facts: [][2]string{{"Work item", "WI-0001"}}}); err != nil {
			t.Fatal(err)
		}
		if c.offers("WI-0001") || len(c.closed()) != before {
			t.Error("a notification that asks for no approval got a decision action")
		}
		var rows int
		if err := w.db.QueryRow(`SELECT COUNT(*) FROM chat_messages`).Scan(&rows); err != nil ||
			rows != 0 {
			t.Errorf("the notification was remembered as a request: %d rows, %v", rows, err)
		}
	})
}

func TestTheNotifierGivesARequestForApprovalToTheApp(t *testing.T) {
	each(t, func(t *testing.T, w *world, c chat) {
		adapter, _ := c.(notify.Adapter)
		rules := notify.Rules{Routes: []notify.Route{{Name: "Approval needed",
			Types: []string{"approval.requested"}, Channels: []string{c.name()},
			Audience: "approvers", Urgency: "immediate"}}}
		notifier := notify.New(w.db, w.log, fixedClock{}, w.url, []notify.Adapter{adapter},
			func() (notify.Rules, error) { return rules, nil })
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go notifier.Run(ctx)
		w.workItem("WI-0001", "T2")
		// The notifier subscribes in its own goroutine. An event before that is lost, so the
		// event is sent until the request is there.
		w.eventually("the request is posted", func() bool {
			if _, err := w.log.Append(audit.Event{Type: "approval.requested", Actor: "orchestrator",
				WorkItem: "WI-0001", Summary: "Approval needed: Upgrade Paperboat",
				Data: map[string]any{"tier": "T2", "sentence": "Upgrade Paperboat to 1.1.0."}}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(50 * time.Millisecond)
			return c.offers("WI-0001")
		})
		stop()
		w.link(c, "alice", "WI-0001")
		if answer := w.click(c, "alice", "WI-0001", "approve"); !strings.Contains(answer, "is recorded") {
			t.Fatalf("the click on the request of the notifier: %s", answer)
		}
		var content string
		if err := w.db.QueryRow(`SELECT content FROM chat_messages ORDER BY id LIMIT 1`).Scan(
			&content); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(content, "Upgrade Paperboat to 1.1.0.") ||
			strings.Contains(content, "cannot approve from this message") {
			t.Errorf("the request of the notifier: %s", content)
		}
	})
}
