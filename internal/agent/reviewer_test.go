package agent

import (
	"strings"
	"testing"
)

func TestParseReviewAcceptsOnlyTheAgreedForm(t *testing.T) {
	good := map[string]string{
		`{"tier":"T3","concerns":["aws_db_instance.main: backup_retention_period goes to 0"]}`: "T3",
		"```json\n{\"tier\":\"T2\",\"concerns\":[]}\n```":                                      "T2",
		`  {"tier":"T0","concerns":null}  `:                                                    "T0",
	}
	for content, want := range good {
		review, err := parseReview(content)
		if err != nil || review.Tier != want {
			t.Errorf("parseReview(%q) = %+v, %v; want tier %s", content, review, err, want)
		}
	}
	bad := map[string]string{
		`The change looks safe.`:                         "did not return the JSON object",
		`{"tier":"T4","concerns":[]}`:                    "is not one of",
		`{"tier":"low","concerns":[]}`:                   "is not one of",
		`{"concerns":["no tier"]}`:                       "is not one of",
		`{"tier":"T2","concerns":[],"approved":true}`:    "did not return the JSON object",
		`{"tier":"T2","concerns":"one string, no list"}`: "did not return the JSON object",
	}
	for content, want := range bad {
		_, err := parseReview(content)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("parseReview(%q): error %v, want one with %q", content, err, want)
		}
	}
}
