package audit_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func newLog(t *testing.T) (*audit.Log, *sql.DB) {
	t.Helper()
	db := storetest.Open(t)
	return audit.New(db, fixedClock{time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}), db
}

func appendN(t *testing.T, log *audit.Log, n int) {
	t.Helper()
	for i := range n {
		if _, err := log.Append(audit.Event{Type: "test.event", Actor: "tester",
			Summary: "event", Data: map[string]any{"i": i}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestChainVerifiesAndLinks(t *testing.T) {
	log, _ := newLog(t)
	appendN(t, log, 5)
	result, err := log.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Count != 5 {
		t.Fatalf("want an intact chain of 5, got %+v", result)
	}
	events, err := log.Query(audit.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(events); i++ {
		if events[i].PrevHash != events[i-1].Hash {
			t.Fatalf("event %d does not link to event %d", events[i].Seq, events[i-1].Seq)
		}
	}
	if result.HeadHash != events[4].Hash {
		t.Fatalf("head hash %s is not the hash of the last event", result.HeadHash)
	}
}

func TestVerifyDetectsEditedEvent(t *testing.T) {
	log, db := newLog(t)
	appendN(t, log, 4)
	if _, err := db.Exec(`UPDATE events SET summary = 'tampered' WHERE seq = 2`); err != nil {
		t.Fatal(err)
	}
	result, err := log.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.BrokenSeq != 2 {
		t.Fatalf("want the chain broken at event 2, got %+v", result)
	}
}

func TestVerifyDetectsDeletedEvent(t *testing.T) {
	log, db := newLog(t)
	appendN(t, log, 4)
	if _, err := db.Exec(`DELETE FROM events WHERE seq = 3`); err != nil {
		t.Fatal(err)
	}
	result, err := log.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.BrokenSeq != 4 {
		t.Fatalf("want the chain broken at event 4, got %+v", result)
	}
}

func TestAppendRejectsEventWithoutActor(t *testing.T) {
	log, _ := newLog(t)
	if _, err := log.Append(audit.Event{Type: "test.event"}); err == nil {
		t.Fatal("want an error for an event without an actor")
	}
}

func TestEmptyChainVerifies(t *testing.T) {
	log, _ := newLog(t)
	result, err := log.Verify()
	if err != nil || !result.OK || result.Count != 0 {
		t.Fatalf("want an intact empty chain, got %+v, %v", result, err)
	}
}

func TestQueryFilters(t *testing.T) {
	log, _ := newLog(t)
	for _, e := range []audit.Event{
		{Type: "apply.succeeded", Actor: "runner", Env: "local", WorkItem: "WI-0001", Summary: "a"},
		{Type: "apply.failed", Actor: "runner", Env: "aws", WorkItem: "WI-0002", Summary: "b"},
		{Type: "notify.sent", Actor: "notifier", Summary: "needle here"},
	} {
		if _, err := log.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name   string
		filter audit.Filter
		want   int
	}{
		{"by environment", audit.Filter{Env: "aws"}, 1},
		{"by work item", audit.Filter{WorkItem: "WI-0001"}, 1},
		{"by type list", audit.Filter{Types: []string{"apply.succeeded", "apply.failed"}}, 2},
		{"by prefix", audit.Filter{TypePrefix: "notify."}, 1},
		{"by search", audit.Filter{Search: "needle"}, 1},
		{"after sequence", audit.Filter{AfterSeq: 2}, 1},
		{"no match", audit.Filter{Env: "gcp"}, 0},
	}
	for _, tc := range cases {
		events, err := log.Query(tc.filter)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(events) != tc.want {
			t.Errorf("%s: want %d events, got %d", tc.name, tc.want, len(events))
		}
	}
}
