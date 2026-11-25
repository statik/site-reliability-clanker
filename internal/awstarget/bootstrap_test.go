package awstarget

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// exampleOutputs returns the outputs of layer 00-bootstrap as `tofu output -json` prints
// them, for an example account.
func exampleOutputs() map[string]map[string]any {
	text := func(value string) map[string]any {
		return map[string]any{"sensitive": false, "type": "string", "value": value}
	}
	return map[string]map[string]any{
		"clanker_ttl":     text("168"),
		"evidence_bucket": text("clanker-example-evidence-111122223333"),
		"identities": {"sensitive": false, "type": []any{"map", "string"},
			"value": map[string]any{"planner": plannerRole, "applier-30-data": applierRole}},
		"permission_guard": text("arn:aws:iam::111122223333:policy/clanker-example-boundary"),
		"state_backend": {"sensitive": false, "type": []any{"object", map[string]any{
			"bucket": "string", "encrypt": "string", "region": "string",
			"use_lockfile": "string"}},
			"value": map[string]any{"bucket": "clanker-example-state-111122223333",
				"encrypt": "true", "region": "eu-west-1", "use_lockfile": "true"}},
		"workload_role_path": text("/clanker-workload/"),
	}
}

func encode(t *testing.T, outputs any) string {
	t.Helper()
	raw, err := json.Marshal(outputs)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestParseBootstrapGivesTheOutputsTheBackendAndTheRoles(t *testing.T) {
	b, err := ParseBootstrap(encode(t, exampleOutputs()))
	if err != nil {
		t.Fatal(err)
	}
	wantBackend := map[string]string{"bucket": "clanker-example-state-111122223333",
		"encrypt": "true", "region": "eu-west-1", "use_lockfile": "true"}
	if !reflect.DeepEqual(b.Backend, wantBackend) || b.Region != "eu-west-1" {
		t.Errorf("backend = %v in %q, want %v in eu-west-1", b.Backend, b.Region, wantBackend)
	}
	wantRoles := map[string]string{"planner": plannerRole, "applier-30-data": applierRole}
	if !reflect.DeepEqual(b.Roles, wantRoles) {
		t.Errorf("roles = %v, want %v", b.Roles, wantRoles)
	}
	// The outputs have the form of the outputs of an apply job: the value of each, by name.
	if len(b.Outputs) != len(bootstrapOutputs) || b.Outputs["clanker_ttl"] != "168" {
		t.Errorf("outputs = %v", b.Outputs)
	}
	identities, isMap := b.Outputs["identities"].(map[string]any)
	if !isMap || identities["planner"] != plannerRole {
		t.Errorf("output identities = %#v", b.Outputs["identities"])
	}
}

func TestParseBootstrapRefusesOutputsThatAreNotThoseOfTheLayer(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]map[string]any)
		want   string
	}{
		{"unknown output", func(o map[string]map[string]any) {
			o["cluster_name"] = o["clanker_ttl"]
		}, `layer 00-bootstrap has no output "cluster_name"`},
		{"missing output", func(o map[string]map[string]any) {
			delete(o, "workload_role_path")
		}, "the output workload_role_path of layer 00-bootstrap is missing"},
		{"sensitive output", func(o map[string]map[string]any) {
			o["evidence_bucket"]["sensitive"] = true
		}, "the output evidence_bucket of layer 00-bootstrap is sensitive"},
		{"no value", func(o map[string]map[string]any) {
			delete(o["clanker_ttl"], "value")
		}, "the output clanker_ttl of layer 00-bootstrap has no `sensitive` or no `value`"},
		{"no sensitive", func(o map[string]map[string]any) {
			delete(o["clanker_ttl"], "sensitive")
		}, "the output clanker_ttl of layer 00-bootstrap has no `sensitive` or no `value`"},
		{"unknown field of an output", func(o map[string]map[string]any) {
			o["clanker_ttl"]["description"] = "lifetime"
		}, `unknown field "description"`},
		{"key in the backend", func(o map[string]map[string]any) {
			backend, _ := o["state_backend"]["value"].(map[string]any)
			backend["key"] = "other/10-network.tfstate"
		}, `state_backend of layer 00-bootstrap has the setting "key"`},
		{"backend with no region", func(o map[string]map[string]any) {
			backend, _ := o["state_backend"]["value"].(map[string]any)
			delete(backend, "region")
		}, "state_backend of layer 00-bootstrap has no value for region"},
		{"backend that is not text", func(o map[string]map[string]any) {
			backend, _ := o["state_backend"]["value"].(map[string]any)
			backend["encrypt"] = true
		}, "state_backend of layer 00-bootstrap is not a map of text values"},
		{"identities that are a list", func(o map[string]map[string]any) {
			o["identities"]["value"] = []string{plannerRole}
		}, "the output identities of layer 00-bootstrap is not a map of text values"},
		{"no identity", func(o map[string]map[string]any) {
			o["identities"]["value"] = map[string]string{}
		}, "the roles of the identities name no identity"},
		{"identity that is not a role", func(o map[string]map[string]any) {
			o["identities"]["value"] = map[string]string{
				"planner": "arn:aws:iam::111122223333:user/example"}
		}, "not the ARN of an IAM role"},
	}
	for _, c := range cases {
		outputs := exampleOutputs()
		c.change(outputs)
		if _, err := ParseBootstrap(encode(t, outputs)); err == nil ||
			!strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error = %v, want %q", c.name, err, c.want)
		}
	}
	for _, text := range []string{"", "not JSON", `["state_backend"]`} {
		if _, err := ParseBootstrap(text); err == nil ||
			!strings.Contains(err.Error(), "not what `tofu output -json` prints") {
			t.Errorf("%q: error = %v", text, err)
		}
	}
}

func TestBootstrapOutputsAreTheOutputsOfTheStack(t *testing.T) {
	source, err := os.ReadFile("../../platform/stacks/aws/00-bootstrap/outputs.tf")
	if err != nil {
		t.Fatal(err)
	}
	declared := []string{}
	for _, match := range regexp.MustCompile(`(?m)^output "([^"]+)"`).FindAllStringSubmatch(
		string(source), -1) {
		declared = append(declared, match[1])
	}
	slices.Sort(declared)
	if !slices.Equal(declared, bootstrapOutputs) {
		t.Fatalf("outputs.tf declares %v, the control plane reads %v: change both together",
			declared, bootstrapOutputs)
	}
}
