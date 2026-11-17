package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

func TestTheReviewCanOnlyMakeTheTierHigher(t *testing.T) {
	cases := []struct {
		name               string
		policyTier         string
		review             Review
		reviewErr          error
		wantTier, wantRule string
		wantEvent          string
	}{
		{"the reviewer agrees", "T2", Review{Tier: "T2"}, nil, "T2", "normal-change", "review.completed"},
		{"the reviewer finds a risk", "T2", Review{Tier: "T3", Concerns: []string{"backups go off"}},
			nil, "T3", ReviewRule, "review.completed"},
		{"the reviewer calls a high risk change safe", "T3", Review{Tier: "T0"}, nil, "T3",
			"normal-change", "review.completed"},
		{"the reviewer fails", "T2", Review{}, errors.New("the model did not answer"), "T2",
			"normal-change", "agent.failed"},
		{"there was no review", "T2", Review{}, nil, "T2", "normal-change", ""},
	}
	for _, tc := range cases {
		db := storetest.Open(t)
		o := &Orchestrator{DB: db, Audit: audit.New(db, fixedClock{}), Clock: fixedClock{}}
		var sawTier string
		o.Hooks.Review = func(_ context.Context, _ WorkItem, _ []Plan, policyTier string) (Review, error) {
			sawTier = policyTier
			return tc.review, tc.reviewErr
		}
		wi := WorkItem{ID: "WI-0001", Env: "local", Title: "A change"}
		tier, rule := o.review(context.Background(), wi, tc.policyTier, "normal-change")
		if tier != tc.wantTier || rule != tc.wantRule {
			t.Errorf("%s: tier %s by rule %s, want %s by %s", tc.name, tier, rule, tc.wantTier,
				tc.wantRule)
		}
		if sawTier != tc.policyTier {
			t.Errorf("%s: the reviewer got the tier %q, want %q", tc.name, sawTier, tc.policyTier)
		}
		events, err := o.Audit.Query(audit.Filter{WorkItem: "WI-0001"})
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		if len(events) > 0 {
			got = events[0].Type
		}
		if got != tc.wantEvent || len(events) > 1 {
			t.Errorf("%s: events %v, want one of type %q", tc.name, events, tc.wantEvent)
		}
	}
}

func TestWithoutAReviewerTheTierOfThePolicyEngineStands(t *testing.T) {
	db := storetest.Open(t)
	o := &Orchestrator{DB: db, Audit: audit.New(db, fixedClock{}), Clock: fixedClock{}}
	if tier, rule := o.review(context.Background(), WorkItem{ID: "WI-0001"}, "T2", "normal-change"); tier != "T2" || rule != "normal-change" {
		t.Fatalf("tier %s by rule %s", tier, rule)
	}
}
