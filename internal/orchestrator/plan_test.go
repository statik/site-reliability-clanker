package orchestrator_test

import (
	"slices"
	"strings"
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
		{"bootstrap on the local target", "local",
			[]string{"platform/stacks/local/00-bootstrap/main.tf"}, []string{"00-bootstrap"}},
	}
	for _, tc := range cases {
		got, err := orchestrator.LayersForPaths(tc.cloud, packtest.Example(t), tc.paths)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%s: want %q, got %q, %v", tc.name, tc.want, got, err)
		}
	}
}

func TestAChangeToTheBootstrapLayerOfACloudIsRefused(t *testing.T) {
	paths := []string{"platform/stacks/aws/10-network/main.tf",
		"platform/stacks/aws/00-bootstrap/variables.tf"}
	got, err := orchestrator.LayersForPaths("aws", packtest.Example(t), paths)
	want := "the change touches layer 00-bootstrap of cloud aws, which the control plane " +
		"does not apply: there is no applier role for it. Apply it with `clanker bootstrap " +
		"--cloud aws`"
	if err == nil || !strings.Contains(err.Error(), want) || got != nil {
		t.Fatalf("layers = %q, error = %v, want %q", got, err, want)
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

func TestTemplateExplanationCountsTheChangedOutputs(t *testing.T) {
	wi := orchestrator.WorkItem{Title: "Deploy", Layers: []string{"10-network"}}
	plan := orchestrator.Plan{Layer: "10-network"}
	plan.Facts.Outputs = []string{"network_id", "private_subnet_ids"}
	want := "Deploy: 0 resources to add, 0 to change, 0 to destroy or replace in " +
		"10-network. 2 outputs change."
	if got := orchestrator.TemplateExplanation(wi, []orchestrator.Plan{plan}).Sentence; got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestAChartOfAnotherPackTouchesItsLayer(t *testing.T) {
	second := packtest.Second(t)
	got, err := orchestrator.LayersForPaths("local", second,
		[]string{"apps/harbor/chart/values.yaml"})
	if err != nil || len(got) != 1 || got[0] != second.Layer {
		t.Errorf("layers = %v, %v, want the layer of the pack", got, err)
	}
	example := packtest.Example(t)
	other, err := orchestrator.LayersForPaths("local", second,
		[]string{example.ChartMount() + "/values.yaml"})
	if err != nil || len(other) != 0 {
		t.Errorf("the chart of another application touched %v, %v", other, err)
	}
}

func TestAPackCannotPublishIntoARepositoryOfTheCore(t *testing.T) {
	for _, repo := range []string{orchestrator.PlatformRepo, orchestrator.RunbooksRepo} {
		if !pack.ReservedRepository(repo) {
			t.Errorf("a pack can name the repository %s of the core", repo)
		}
	}
}
