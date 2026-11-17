package orchestrator_test

import (
	"slices"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/policy"
)

func TestLayersForPaths(t *testing.T) {
	cases := []struct {
		name  string
		cloud string
		paths []string
		want  []string
	}{
		{"one layer", "local", []string{"platform/stacks/local/50-app/terraform.tfvars"},
			[]string{"50-app"}},
		{"apply order, not path order", "local",
			[]string{"platform/stacks/local/50-app/main.tf", "platform/stacks/local/30-data/main.tf"},
			[]string{"30-data", "50-app"}},
		{"other cloud is ignored", "local", []string{"platform/stacks/aws/50-app/main.tf"},
			[]string{}},
		{"chart touches the app layer", "aws", []string{"apps/paperboat/chart/values.yaml"},
			[]string{"50-app"}},
		{"shared platform module", "gcp", []string{"platform/modules/shared/platform/main.tf"},
			[]string{"40-platform"}},
		{"unrelated file", "local", []string{"README.md"}, []string{}},
		{"no paths", "local", nil, []string{}},
	}
	for _, tc := range cases {
		got := orchestrator.LayersForPaths(tc.cloud, tc.paths)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: want %q, got %q", tc.name, tc.want, got)
		}
	}
}

func TestApproversRequired(t *testing.T) {
	for tier, want := range map[string]int{"T0": 0, "T1": 0, "T2": 1, "T3": 2, "": 0} {
		if got := policy.ApproversRequired(tier); got != want {
			t.Errorf("tier %q: want %d approvers, got %d", tier, want, got)
		}
	}
}

func TestTemplateExplanationUsesOnlyPlanFacts(t *testing.T) {
	wi := orchestrator.WorkItem{Title: "Upgrade", Layers: []string{"50-app"}, Trigger: "release"}
	plan := orchestrator.Plan{Layer: "50-app"}
	plan.Facts.Change = 1
	explanation := orchestrator.TemplateExplanation(wi, []orchestrator.Plan{plan})
	if explanation.Source != "template" {
		t.Errorf("want source template, got %s", explanation.Source)
	}
	want := "Upgrade: 0 resources to add, 1 to change, 0 to destroy or replace in 50-app."
	if explanation.Sentence != want {
		t.Errorf("want %q, got %q", want, explanation.Sentence)
	}
}
