package dispatch

import (
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/jobspec"
)

var launch = time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)

func launched() *pending {
	return &pending{job: jobspec.Job{Spec: jobspec.Spec{Kind: jobspec.KindPlan}}, seen: launch}
}

// check returns the error of overdue at the time after the launch, or "" when the runner
// still has time.
func check(t *testing.T, d *Dispatcher, p *pending, after time.Duration) string {
	t.Helper()
	if err := d.overdue(p, launch.Add(after)); err != nil {
		return err.Error()
	}
	return ""
}

func TestARunnerWithNoRequestFailsAfterTheStartGrace(t *testing.T) {
	d := &Dispatcher{Silence: 90 * time.Second, StartGrace: 5 * time.Minute}
	p := launched()
	if got := check(t, d, p, 5*time.Minute); got != "" {
		t.Fatalf("at the end of the grace: %s", got)
	}
	got := check(t, d, p, 5*time.Minute+time.Second)
	if !strings.Contains(got, "plan job made no request in the 5m1s after its launch") {
		t.Fatalf("after the grace: %q, want the failure to start", got)
	}
}

func TestAfterTheFirstRequestTheSilenceLimitApplies(t *testing.T) {
	d := &Dispatcher{Silence: 90 * time.Second, StartGrace: 5 * time.Minute}
	p := launched()
	first := 5*time.Minute - time.Second
	p.touch(launch.Add(first))
	// The grace ends, but the runner has started.
	if got := check(t, d, p, 5*time.Minute+time.Second); got != "" {
		t.Fatalf("after the grace: %s", got)
	}
	if got := check(t, d, p, first+90*time.Second); got != "" {
		t.Fatalf("at the end of the silence: %s", got)
	}
	got := check(t, d, p, first+91*time.Second)
	if !strings.Contains(got, "plan job said nothing for 1m31s") {
		t.Fatalf("after the silence: %q, want the silence", got)
	}
}

func TestAStartGraceShorterThanTheSilenceIsTheLimitBeforeTheFirstRequest(t *testing.T) {
	d := &Dispatcher{Silence: 90 * time.Second, StartGrace: 30 * time.Second}
	got := check(t, d, launched(), 31*time.Second)
	if !strings.Contains(got, "made no request") {
		t.Fatalf("got %q, want the failure to start", got)
	}
}

func TestWithoutAStartGraceTheSilenceCountsFromTheLaunch(t *testing.T) {
	d := &Dispatcher{Silence: 90 * time.Second}
	p := launched()
	if got := check(t, d, p, 90*time.Second); got != "" {
		t.Fatalf("at the end of the silence: %s", got)
	}
	got := check(t, d, p, 91*time.Second)
	if !strings.Contains(got, "said nothing for 1m31s") {
		t.Fatalf("got %q, want the silence", got)
	}
}
