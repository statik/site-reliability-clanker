package policy_test

import (
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/policy"
)

func TestApprovalsWithoutAFileNeedTwoApproversForT3(t *testing.T) {
	var absent policy.Approvals
	for tier, want := range map[string]int{"T0": 0, "T1": 0, "T2": 1, "T3": 2, "": 0} {
		if got := absent.Required(tier); got != want {
			t.Errorf("tier %q: want %d approvers, got %d", tier, want, got)
		}
	}
}

func TestParseApprovalsAcceptsOneAndTwo(t *testing.T) {
	for source, want := range map[string]int{
		`{"t3_approvers": 1}`:       1,
		`{"t3_approvers": 2}`:       2,
		"{\"t3_approvers\": 1}\n\n": 1,
	} {
		approvals, err := policy.ParseApprovals([]byte(source))
		if err != nil {
			t.Errorf("%s: %v", source, err)
			continue
		}
		if got := approvals.Required("T3"); got != want {
			t.Errorf("%s: tier T3 needs %d approvers, want %d", source, got, want)
		}
		if got := approvals.Required("T2"); got != 1 {
			t.Errorf("%s: tier T2 needs %d approvers, want 1", source, got)
		}
		if got := approvals.Required("T1"); got != 0 {
			t.Errorf("%s: tier T1 needs %d approvers, want 0", source, got)
		}
	}
}

func TestParseApprovalsRefusesEverythingElse(t *testing.T) {
	cases := map[string]string{
		`{"t3_approvers": 0}`:                    "t3_approvers = 0",
		`{"t3_approvers": 3}`:                    "t3_approvers = 3",
		`{"t3_approvers": -1}`:                   "t3_approvers = -1",
		`{"t3_approvers": "1"}`:                  "is not valid",
		`{"t3_approvers": 1.5}`:                  "is not valid",
		`{"t3_approvers": null}`:                 "no value for t3_approvers",
		`{}`:                                     "no value for t3_approvers",
		`null`:                                   "no value for t3_approvers",
		`{"t3_approvers": 1, "t2_approvers": 0}`: "is not valid",
		`{"t2_approvers": 0}`:                    "is not valid",
		`not JSON`:                               "is not valid",
		``:                                       "is not valid",
		`{"t3_approvers": 1`:                     "is not valid",
		`{"t3_approvers": 1} {"t3_approvers": 2}`: "data after the JSON object",
		`{"t3_approvers": 1} x`:                   "data after the JSON object",
		`[1]`:                                     "is not valid",
	}
	for source, want := range cases {
		approvals, err := policy.ParseApprovals([]byte(source))
		if err == nil {
			t.Errorf("%q: want an error, got %d approvers for T3", source,
				approvals.Required("T3"))
			continue
		}
		for _, part := range []string{want, "policy/approvals.json", "1 and 2"} {
			if !strings.Contains(err.Error(), part) {
				t.Errorf("%q: the error %q does not contain %q", source, err, part)
			}
		}
		if got := approvals.Required("T3"); got != 2 {
			t.Errorf("%q: the value beside the error gives %d approvers for T3, want 2",
				source, got)
		}
	}
}
