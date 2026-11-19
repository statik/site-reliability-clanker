package orchestrator_test

import (
	"slices"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
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
		got := orchestrator.LayersForPaths(tc.cloud, packtest.Example(t), tc.paths)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: want %q, got %q", tc.name, tc.want, got)
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

func TestAChartOfAnotherPackTouchesItsLayer(t *testing.T) {
	second := packtest.Second(t)
	got := orchestrator.LayersForPaths("local", second, []string{"apps/harbor/chart/values.yaml"})
	if len(got) != 1 || got[0] != second.Layer {
		t.Errorf("layers = %v, want the layer of the pack", got)
	}
	example := packtest.Example(t)
	other := orchestrator.LayersForPaths("local", second,
		[]string{example.ChartMount() + "/values.yaml"})
	if len(other) != 0 {
		t.Errorf("the chart of another application touched %v", other)
	}
}

func TestAPackCannotPublishIntoARepositoryOfTheCore(t *testing.T) {
	for _, repo := range []string{orchestrator.PlatformRepo, orchestrator.RunbooksRepo} {
		if !pack.ReservedRepository(repo) {
			t.Errorf("a pack can name the repository %s of the core", repo)
		}
	}
}
