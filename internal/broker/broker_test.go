package broker_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

type testClock struct{ t time.Time }

func (c *testClock) Now() time.Time { return c.t }

// fakeIssuer stands in for the API of a target.
type fakeIssuer struct {
	issueErr    error
	withdrawErr error
	issued      []string
	withdrawn   []string
	remaining   time.Duration
}

func (f *fakeIssuer) Issue(_ context.Context, g broker.Grant, remaining time.Duration) (
	broker.Credential, error) {
	f.issued = append(f.issued, g.ID)
	f.remaining = remaining
	if f.issueErr != nil {
		return broker.Credential{}, f.issueErr
	}
	return broker.Credential{KubeConfig: "kubeconfig of " + g.Identity, Mechanism: "fake"}, nil
}

func (f *fakeIssuer) Withdraw(_ context.Context, g broker.Grant) error {
	f.withdrawn = append(f.withdrawn, g.ID)
	return f.withdrawErr
}

func newBrokerWith(t *testing.T, issuer broker.Issuer) (*broker.Broker, *testClock) {
	t.Helper()
	dir := t.TempDir()
	db := storetest.Open(t)
	clock := &testClock{time.Date(2026, 11, 21, 2, 0, 0, 0, time.UTC)}
	b, err := broker.New(db, clock, dir, issuer)
	if err != nil {
		t.Fatal(err)
	}
	return b, clock
}

func newBroker(t *testing.T) (*broker.Broker, *testClock) {
	t.Helper()
	return newBrokerWith(t, nil)
}

var held = broker.Presentation{Commit: "9f3c1e7", PlanSHA256: "5b1d"}

func status(t *testing.T, b *broker.Broker, id string) string {
	t.Helper()
	stored, err := b.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return stored.Status
}

func TestExchangeGivesTheCredentialOfTheIssuer(t *testing.T) {
	issuer := &fakeIssuer{}
	b, clock := newBrokerWith(t, issuer)
	g, err := b.Issue(applyRequest())
	if err != nil {
		t.Fatal(err)
	}
	clock.t = clock.t.Add(20 * time.Minute)
	cred, err := b.Exchange(context.Background(), g, held)
	if err != nil {
		t.Fatal(err)
	}
	if cred.GrantID != g.ID || cred.KubeConfig != "kubeconfig of applier-30-data" {
		t.Errorf("unexpected credential %+v", cred)
	}
	if issuer.remaining != 40*time.Minute {
		t.Errorf("want 40 minutes until the grant expires, got %s", issuer.remaining)
	}
	if got := status(t, b, g.ID); got != "active" {
		t.Errorf("want status active, got %s", got)
	}
}

func TestExchangeRefusedGrantNeverReachesTheIssuer(t *testing.T) {
	issuer := &fakeIssuer{}
	b, _ := newBrokerWith(t, issuer)
	g, err := b.Issue(applyRequest())
	if err != nil {
		t.Fatal(err)
	}
	stale := broker.Presentation{Commit: "9f3c1e7", PlanSHA256: "other"}
	if _, err := b.Exchange(context.Background(), g, stale); err == nil {
		t.Fatal("want the exchange of a stale plan refused")
	}
	if len(issuer.issued) != 0 {
		t.Errorf("the issuer was called for a refused grant: %v", issuer.issued)
	}
}

func TestExchangeIssuerFailureLeavesTheStatus(t *testing.T) {
	issuer := &fakeIssuer{issueErr: errors.New("the target is down")}
	b, _ := newBrokerWith(t, issuer)
	g, err := b.Issue(applyRequest())
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Exchange(context.Background(), g, held)
	if err == nil || !strings.Contains(err.Error(), "the target is down") {
		t.Fatalf("want the error of the issuer, got %v", err)
	}
	if got := status(t, b, g.ID); got != "issued" {
		t.Errorf("want status issued, got %s", got)
	}
}

func TestExchangeWithoutAnIssuerIsRefused(t *testing.T) {
	b, _ := newBroker(t)
	g, err := b.Issue(applyRequest())
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Exchange(context.Background(), g, held)
	if err == nil || !strings.Contains(err.Error(), "no credential issuer") {
		t.Fatalf("want an error that names the missing issuer, got %v", err)
	}
	if err := b.Revoke(context.Background(), g, "failed"); err != nil {
		t.Fatalf("a revoke without an issuer must end the grant, got %v", err)
	}
	if got := status(t, b, g.ID); got != "failed" {
		t.Errorf("want status failed, got %s", got)
	}
}

func TestRevokeEndsTheGrantWhenWithdrawFails(t *testing.T) {
	issuer := &fakeIssuer{withdrawErr: errors.New("the target is down")}
	b, _ := newBrokerWith(t, issuer)
	g, err := b.Issue(applyRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Exchange(context.Background(), g, held); err != nil {
		t.Fatal(err)
	}
	err = b.Revoke(context.Background(), g, "used")
	if err == nil || !strings.Contains(err.Error(), "the target is down") {
		t.Fatalf("want the error of the issuer, got %v", err)
	}
	if got := status(t, b, g.ID); got != "used" {
		t.Errorf("want status used, got %s", got)
	}
	if len(issuer.withdrawn) != 1 || issuer.withdrawn[0] != g.ID {
		t.Errorf("want one withdraw of %s, got %v", g.ID, issuer.withdrawn)
	}
	if err := b.Check(g, held); err == nil {
		t.Error("an ended grant must stay refused")
	}
}

func applyRequest() broker.Request {
	return broker.Request{WorkItem: "WI-0142", Env: "local", Layer: "30-data",
		Kind: broker.KindApply, Commit: "9f3c1e7", PlanSHA256: "5b1d", Tier: "T3",
		Approvers: []string{"alice", "bob"}, Duration: time.Hour}
}

func TestIssueNeedsTheApproversOfTheTier(t *testing.T) {
	b, _ := newBroker(t)
	cases := []struct {
		tier      string
		approvers []string
		wantErr   bool
	}{
		{"T3", []string{"alice", "bob"}, false},
		{"T3", []string{"alice"}, true},
		{"T3", []string{"alice", "alice"}, true},
		{"T3", []string{"alice", ""}, true},
		{"T2", []string{"alice"}, false},
		{"T2", nil, true},
	}
	for _, tc := range cases {
		req := applyRequest()
		req.Tier, req.Approvers = tc.tier, tc.approvers
		_, err := b.Issue(req)
		if (err != nil) != tc.wantErr {
			t.Errorf("tier %s with approvers %q: error = %v, want error %v", tc.tier,
				tc.approvers, err, tc.wantErr)
		}
	}
}

func TestIssueApplyGrantNeedsCommitAndPlanHash(t *testing.T) {
	b, _ := newBroker(t)
	req := applyRequest()
	req.PlanSHA256 = ""
	if _, err := b.Issue(req); err == nil {
		t.Error("want an error for an apply grant without a plan hash")
	}
	req = applyRequest()
	req.Commit = ""
	if _, err := b.Issue(req); err == nil {
		t.Error("want an error for an apply grant without a commit")
	}
}

func TestCheckAcceptsTheExactCommitAndPlan(t *testing.T) {
	b, _ := newBroker(t)
	g, err := b.Issue(applyRequest())
	if err != nil {
		t.Fatal(err)
	}
	if g.Identity != "applier-30-data" {
		t.Errorf("want identity applier-30-data, got %s", g.Identity)
	}
	if err := b.Check(g, broker.Presentation{Commit: "9f3c1e7", PlanSHA256: "5b1d"}); err != nil {
		t.Fatalf("want the grant accepted, got %v", err)
	}
}

func TestCheckRefusals(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(g *broker.Grant, p *broker.Presentation, clock *testClock)
		wantErr string
	}{
		{"stale plan", func(_ *broker.Grant, p *broker.Presentation, _ *testClock) {
			p.PlanSHA256 = "other"
		}, "runner holds plan"},
		{"other commit", func(_ *broker.Grant, p *broker.Presentation, _ *testClock) {
			p.Commit = "abc"
		}, "runner holds commit"},
		{"expired", func(_ *broker.Grant, _ *broker.Presentation, c *testClock) {
			c.t = c.t.Add(time.Hour)
		}, "expired"},
		{"before the window", func(_ *broker.Grant, _ *broker.Presentation, c *testClock) {
			c.t = c.t.Add(-time.Minute)
		}, "not valid before"},
		{"layer edited after signing", func(g *broker.Grant, _ *broker.Presentation, _ *testClock) {
			g.Layer = "00-bootstrap"
		}, "signature is not valid"},
		{"expiry edited after signing", func(g *broker.Grant, _ *broker.Presentation, _ *testClock) {
			g.Expires = "2099-01-01T00:00:00.000Z"
		}, "signature is not valid"},
		{"forged signature", func(g *broker.Grant, _ *broker.Presentation, _ *testClock) {
			g.Signature = "AAAA"
		}, "signature is not valid"},
	}
	for _, tc := range cases {
		b, clock := newBroker(t)
		g, err := b.Issue(applyRequest())
		if err != nil {
			t.Fatal(err)
		}
		p := broker.Presentation{Commit: "9f3c1e7", PlanSHA256: "5b1d"}
		tc.mutate(&g, &p, clock)
		err = b.Check(g, p)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: want an error with %q, got %v", tc.name, tc.wantErr, err)
		}
	}
}

func TestExpireOverdueMarksGrants(t *testing.T) {
	b, clock := newBroker(t)
	g, err := b.Issue(applyRequest())
	if err != nil {
		t.Fatal(err)
	}
	if overdue, err := b.ExpireOverdue(); err != nil || len(overdue) != 0 {
		t.Fatalf("no grant is overdue yet: %v, %v", overdue, err)
	}
	clock.t = clock.t.Add(2 * time.Hour)
	overdue, err := b.ExpireOverdue()
	if err != nil || len(overdue) != 1 || overdue[0].ID != g.ID {
		t.Fatalf("want grant %s expired, got %v, %v", g.ID, overdue, err)
	}
	stored, err := b.Get(g.ID)
	if err != nil || stored.Status != "expired" {
		t.Fatalf("want status expired, got %s, %v", stored.Status, err)
	}
	clock.t = clock.t.Add(-2 * time.Hour)
	err = b.Check(g, broker.Presentation{Commit: "9f3c1e7", PlanSHA256: "5b1d"})
	if err == nil || !strings.Contains(err.Error(), "status is expired") {
		t.Fatalf("an expired grant must stay refused, got %v", err)
	}
}

func TestIdentityFor(t *testing.T) {
	cases := map[[2]string]string{
		{broker.KindPlan, "50-app"}:    "planner",
		{broker.KindApply, "50-app"}:   "applier-50-app",
		{broker.KindRunbook, "any"}:    "runbook",
		{broker.KindDebug, "whatever"}: "debugger",
	}
	for in, want := range cases {
		if got := broker.IdentityFor(in[0], in[1]); got != want {
			t.Errorf("IdentityFor(%s, %s) = %s, want %s", in[0], in[1], got, want)
		}
	}
}
