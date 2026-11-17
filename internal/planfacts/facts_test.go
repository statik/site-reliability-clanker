package planfacts_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/planfacts"
)

const upgradePlan = `{"resource_changes":[
 {"address":"module.app.helm_release.paperboat","type":"helm_release","name":"paperboat",
  "change":{"actions":["update"],
   "before":{"name":"paperboat","values":["\"image\":\n  \"registry\": \"localhost:5001\"\n  \"tag\": \"1.0.0\"\n\"api\":\n  \"replicas\": 1\n"],"set_sensitive":[{"name":"adminToken","value":"old"}]},
   "after":{"name":"paperboat","values":["\"image\":\n  \"registry\": \"localhost:5001\"\n  \"tag\": \"1.1.0\"\n\"api\":\n  \"replicas\": 1\n"],"set_sensitive":[{"name":"adminToken","value":"new"}]},
   "before_sensitive":{"set_sensitive":true},"after_sensitive":{"set_sensitive":true}}},
 {"address":"data.x.y","type":"x","name":"y","change":{"actions":["read"]}},
 {"address":"a.unchanged","type":"a","name":"unchanged","change":{"actions":["no-op"]}}
],"output_changes":{"paperboat_version":{"actions":["update"]},"app_url":{"actions":["no-op"]}}}`

func TestParseHumanisesHelmValues(t *testing.T) {
	facts, err := planfacts.Parse([]byte(upgradePlan))
	if err != nil {
		t.Fatal(err)
	}
	if facts.Change != 1 || facts.Add+facts.Destroy+facts.Replace != 0 {
		t.Fatalf("want exactly one update, got %+v", facts)
	}
	if len(facts.Resources) != 1 {
		t.Fatalf("read and no-op entries must be left out, got %d resources", len(facts.Resources))
	}
	details := facts.Resources[0].Details
	if !slices.Contains(details, "values.image.tag: 1.0.0 → 1.1.0") {
		t.Errorf("want the image tag change in the details, got %q", details)
	}
	if !slices.Equal(facts.Outputs, []string{"paperboat_version"}) {
		t.Errorf("want only the changed output, got %q", facts.Outputs)
	}
}

func TestParseHidesSensitiveValues(t *testing.T) {
	facts, err := planfacts.Parse([]byte(upgradePlan))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(facts.Resources[0].Details, "\n")
	if strings.Contains(joined, "old") || strings.Contains(joined, "new") {
		t.Fatalf("a sensitive value is in the details: %s", joined)
	}
	if !strings.Contains(joined, "(sensitive value changes)") {
		t.Errorf("want a note that a sensitive value changes, got %s", joined)
	}
}

func TestParseCountsEachAction(t *testing.T) {
	plan := `{"resource_changes":[
	 {"address":"a.create","type":"a","name":"create","change":{"actions":["create"]}},
	 {"address":"a.destroy","type":"a","name":"destroy","change":{"actions":["delete"]}},
	 {"address":"a.replace","type":"a","name":"replace","change":{"actions":["delete","create"]}},
	 {"address":"a.replace2","type":"a","name":"replace2","change":{"actions":["create","delete"]}}
	]}`
	facts, err := planfacts.Parse([]byte(plan))
	if err != nil {
		t.Fatal(err)
	}
	if facts.Add != 1 || facts.Destroy != 1 || facts.Replace != 2 || facts.Change != 0 {
		t.Fatalf("wrong counts: %+v", facts)
	}
	if !facts.HasChanges() {
		t.Fatal("a plan with changes must report HasChanges")
	}
}

func TestParseEmptyPlanHasNoChanges(t *testing.T) {
	facts, err := planfacts.Parse([]byte(`{"resource_changes":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if facts.HasChanges() {
		t.Fatalf("an empty plan must not report changes: %+v", facts)
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	if _, err := planfacts.Parse([]byte(`not json`)); err == nil {
		t.Error("want an error for a plan file that is not JSON")
	}
	if _, err := planfacts.Parse(nil); err == nil {
		t.Error("want an error for an empty plan")
	}
}
