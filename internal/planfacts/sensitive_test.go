package planfacts_test

import (
	"testing"

	"github.com/statik/site-reliability-clanker/internal/planfacts"
	"github.com/statik/site-reliability-clanker/internal/scrub"
)

func TestParseRegistersSensitiveValuesWithTheScrubber(t *testing.T) {
	plan := `{"resource_changes":[{"address":"a.b","type":"a","name":"b","change":{
	 "actions":["create"],"before":null,
	 "after":{"host":"relay.example.com",
	  "login":[{"user":"mailer-user","pass":"plan-marked-4471"}]},
	 "after_sensitive":{"login":[{"pass":true}]}}}]}`
	if _, err := planfacts.Parse([]byte(plan)); err != nil {
		t.Fatal(err)
	}
	line := "| relay.example.com | mailer-user | plan-marked-4471 |"
	if got := scrub.Text(line); got != "| relay.example.com | mailer-user | [REDACTED] |" {
		t.Errorf("want only the value with the sensitive mark replaced, got %q", got)
	}
}
