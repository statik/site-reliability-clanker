package policy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The real rules of the repository.
const rulesDir = "../../runbooks/policy"

func planFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func change(resourceType string, actions ...string) string {
	return `{"address":"` + resourceType + `.x","type":"` + resourceType +
		`","change":{"actions":["` + strings.Join(actions, `","`) + `"]}}`
}

func TestEvaluateGivesTheTierOfTheRealRules(t *testing.T) {
	cases := []struct {
		name, kind, layer, changes string
		tier, rule                 string
		createOnly                 bool
	}{
		{"nothing changes", "drift", "50-app", ``, "T0", "no-changes", true},
		{"an application release", "app-upgrade", "50-app", change("helm_release", "update"),
			"T2", "normal-change", false},
		{"a stateful resource is replaced", "change", "30-data",
			change("kubernetes_stateful_set_v1", "delete", "create"), "T3", "stateful-destroy", false},
		{"a cloud database is destroyed", "change", "30-data", change("aws_db_instance", "delete"),
			"T3", "stateful-destroy", false},
		{"only new resources", "change", "50-app", change("helm_release", "create"),
			"T2", "normal-change", true},
	}
	for _, c := range cases {
		plan := planFile(t, `{"resource_changes":[`+c.changes+`]}`)
		got, err := Evaluate(context.Background(), Input{Kind: c.kind, Layer: c.layer,
			PlanJSONPath: plan, PolicyDir: rulesDir})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got.Tier != c.tier || got.Rule != c.rule || got.CreateOnly != c.createOnly {
			t.Errorf("%s: got tier %s rule %s create_only %v, want %s %s %v", c.name, got.Tier,
				got.Rule, got.CreateOnly, c.tier, c.rule, c.createOnly)
		}
		if len(got.Reasons) == 0 {
			t.Errorf("%s: the decision has no reason", c.name)
		}
	}
}

func TestEvaluateFailsClearlyOnBadInput(t *testing.T) {
	good := planFile(t, `{"resource_changes":[]}`)
	empty := t.TempDir()
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, "tier.rego"), []byte("package clanker.tier\nthis is not rego"), 0o644); err != nil {
		t.Fatal(err)
	}
	testsOnly := t.TempDir()
	if err := os.WriteFile(filepath.Join(testsOnly, "tier_test.rego"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "other.rego"), []byte("package other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		in   Input
		want string
	}{
		"plan file missing":     {Input{PlanJSONPath: filepath.Join(empty, "none.json"), PolicyDir: rulesDir}, "read plan JSON"},
		"plan is not JSON":      {Input{PlanJSONPath: planFile(t, "{"), PolicyDir: rulesDir}, "decode plan JSON"},
		"no policy file":        {Input{PlanJSONPath: good, PolicyDir: empty}, "has no Rego file"},
		"only a test file":      {Input{PlanJSONPath: good, PolicyDir: testsOnly}, "has no Rego file"},
		"policy does not parse": {Input{PlanJSONPath: good, PolicyDir: broken}, "evaluate the policies"},
		"policy has no tier":    {Input{PlanJSONPath: good, PolicyDir: other, Layer: "50-app"}, "gave no decision"},
	}
	for name, c := range cases {
		_, err := Evaluate(context.Background(), c.in)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error = %v, want one that contains %q", name, err, c.want)
		}
	}
}
