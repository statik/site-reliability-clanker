package policy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realRules reads the rules of the repository, test files too, as the forge gives them.
func realRules(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob("../../runbooks/policy/*.rego")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no rules found: %v", err)
	}
	modules := map[string]string{}
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		modules["policy/"+filepath.Base(path)] = string(source)
	}
	return modules
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
	rules := realRules(t)
	for _, c := range cases {
		got, err := Evaluate(context.Background(), Input{Kind: c.kind, Layer: c.layer,
			PlanJSON: []byte(`{"resource_changes":[` + c.changes + `]}`), Modules: rules})
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
	good := []byte(`{"resource_changes":[]}`)
	rules := realRules(t)
	cases := map[string]struct {
		in   Input
		want string
	}{
		"plan is not JSON": {Input{PlanJSON: []byte("{"), Modules: rules}, "decode plan JSON"},
		"no policy file":   {Input{PlanJSON: good, Modules: map[string]string{}}, "has no Rego file"},
		"only a test file": {Input{PlanJSON: good,
			Modules: map[string]string{"policy/tier_test.rego": "package x\n"}}, "has no Rego file"},
		"a file that is not Rego": {Input{PlanJSON: good,
			Modules: map[string]string{"policy/README.md": "notes"}}, "has no Rego file"},
		"policy does not parse": {Input{PlanJSON: good,
			Modules: map[string]string{"policy/tier.rego": "package clanker.tier\nthis is not rego"}},
			"evaluate the policies"},
		"policy has no tier": {Input{PlanJSON: good, Layer: "50-app",
			Modules: map[string]string{"policy/other.rego": "package other\n"}}, "gave no decision"},
	}
	for name, c := range cases {
		_, err := Evaluate(context.Background(), c.in)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error = %v, want one that contains %q", name, err, c.want)
		}
	}
}
