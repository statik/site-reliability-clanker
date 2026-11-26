package main

import (
	"maps"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/statik/site-reliability-clanker/internal/scrub"
)

// layerStacks are two layers of AWS with the variables of the account and a
// terraform.tfvars file. The values are made up.
var layerStacks = fstest.MapFS{
	"platform/stacks/aws/00-bootstrap/variables.tf": {Data: []byte(
		"variable \"broker_principal\" {\n  type = string\n}\n")},
	"platform/stacks/aws/10-network/variables.tf": {Data: []byte(
		"variable \"existing_network_id\" {\n  type = string\n}\n\n" +
			"variable \"existing_private_subnet_ids\" {\n  type = list(string)\n}\n\n" +
			"variable \"zone_count\" {\n  type = number\n}\n")},
	"platform/stacks/aws/10-network/terraform.tfvars": {Data: []byte(
		"zone_count = 2\nlabels = {\n  existing_network_id = \"x\"\n}\n")},
	"platform/stacks/aws/20-cluster/main.tf": {Data: []byte(
		"variable \"instance_tags\" {\n  type = map(string)\n}\n")},
}

const exampleLayerVariables = `{"existing_network_id": "vpc-0a1b2c3d4e5f60718",
	"existing_private_subnet_ids": ["subnet-0a1b2c3d4e5f60718", "subnet-0f1e2d3c4b5a69788"],
	"instance_tags": {"team": "check-team-value", "note": "a \"quoted\" <value> & more"}}`

func TestTheLayerVariablesBecomeTFVarSettingsInTheFormOfOpenTofu(t *testing.T) {
	got, err := layerVariables(layerStacks, "aws",
		environment(map[string]string{layerVariablesEnv: exampleLayerVariables}))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"TF_VAR_existing_network_id": "vpc-0a1b2c3d4e5f60718",
		"TF_VAR_existing_private_subnet_ids": `["subnet-0a1b2c3d4e5f60718",` +
			`"subnet-0f1e2d3c4b5a69788"]`,
		"TF_VAR_instance_tags": `{"note":"a \"quoted\" <value> & more",` +
			`"team":"check-team-value"}`,
	}
	if !maps.Equal(got, want) {
		t.Errorf("variables = %v, want %v", got, want)
	}
}

func TestTheScrubberLearnsEveryValueOfTheLayerVariables(t *testing.T) {
	if _, err := layerVariables(layerStacks, "aws",
		environment(map[string]string{layerVariablesEnv: exampleLayerVariables})); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"vpc-0a1b2c3d4e5f60718", "subnet-0f1e2d3c4b5a69788",
		"check-team-value", `a "quoted" <value> & more`} {
		if got := scrub.Text("plan: " + value + "."); got != "plan: [REDACTED]." {
			t.Errorf("the scrubber left %q as %q", value, got)
		}
	}
	// A map key names a tag. It is not a value, so the scrubber does not learn it.
	if got := scrub.Text("the tag team"); got != "the tag team" {
		t.Errorf("the scrubber changed a map key: %q", got)
	}
}

func TestTheAWSStacksOfThisRepositoryTakeTheVariablesOfTheAccount(t *testing.T) {
	setting := `{"existing_network_id": "vpc-0a1b2c3d4e5f60718",
		"existing_private_subnet_ids": ["subnet-0a1b2c3d4e5f60718", "subnet-0f1e2d3c4b5a69788"],
		"image_registry": "registry.example.com/check",
		"instance_tags": {"team": "check-team-value"},
		"operator_principal_arn": "arn:aws:iam::111122223333:role/check-operator"}`
	got, err := layerVariables(os.DirFS("../.."), "aws",
		environment(map[string]string{layerVariablesEnv: setting}))
	if err != nil || len(got) != 5 {
		t.Fatalf("variables %v, error %v; want the five inputs of the account", got, err)
	}
}

func TestNoSettingGivesNoLayerVariables(t *testing.T) {
	got, err := layerVariables(layerStacks, "aws", environment(nil))
	if err != nil || got != nil {
		t.Fatalf("variables %v, error %v; want none", got, err)
	}
}

func TestALayerVariableSettingThatIsNotValidStopsTheStart(t *testing.T) {
	cases := []struct{ text, want string }{
		{`not json`, "is not a JSON object from the name of a variable to its value"},
		{`["vpc-0a1b2c3d"]`, "is not a JSON object"},
		{`{"existing_network_id": "vpc-0a1b2c3d"} {}`, "has more text after its JSON object"},
		{`{"region": "eu-west-1"}`, "the core sets the variable region itself"},
		{`{"collector_token": "token-value-1"}`, "the core sets the variable collector_token"},
		{`{"1abc": "x"}`, `"1abc" is not the name of an OpenTofu variable`},
		{`{"zone_count": 2}`, "the value of zone_count: it is not a string, a list of " +
			"strings or a map of strings"},
		{`{"zone_count": true}`, "it is not a string, a list of strings or a map of strings"},
		{`{"existing_network_id": null}`, "the value of existing_network_id: it is null"},
		{`{"instance_tags": {"team": {"nested": "x"}}}`, "it is not a string, a list"},
		{`{"existing_private_subnet_ids": ["subnet-0a", 3]}`, "it is not a string, a list"},
		{`{"instance_tags": {"team": "${file(\"/etc/passwd\")}"}}`,
			"has ${ or %{, which OpenTofu reads as a template"},
		{`{"existing_private_subnet_ids": ["%{ if true }x%{ endif }"]}`, "has ${ or %{"},
		{`{"node_size": "t4g.large"}`, "no layer of aws above 00-bootstrap declares the " +
			"variable node_size"},
		{`{"broker_principal": "arn:aws:iam::111122223333:role/x"}`,
			"no layer of aws above 00-bootstrap declares the variable broker_principal"},
		{`{"zone_count": "3"}`, "platform/stacks/aws/10-network/terraform.tfvars sets the " +
			"variable zone_count, and the file wins over TF_VAR_zone_count"},
	}
	for _, c := range cases {
		_, err := layerVariables(layerStacks, "aws",
			environment(map[string]string{layerVariablesEnv: c.text}))
		if err == nil || !strings.Contains(err.Error(), c.want) ||
			!strings.HasPrefix(err.Error(), layerVariablesEnv+": ") {
			t.Errorf("%s: error %v, want %q", c.text, err, c.want)
		}
	}
}

func TestAnErrorOfTheLayerVariablesHoldsNoValue(t *testing.T) {
	_, err := layerVariables(layerStacks, "aws", environment(map[string]string{
		layerVariablesEnv: `{"instance_tags": {"team": "private-value-${x}"}}`}))
	if err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatalf("error %v, want an error without the value", err)
	}
}

func TestAVariableInsideAMapOfATFVarsFileIsNotSetByTheFile(t *testing.T) {
	// The file sets labels, whose map has a key existing_network_id. The file does not set
	// the variable existing_network_id.
	_, err := layerVariables(layerStacks, "aws", environment(map[string]string{
		layerVariablesEnv: `{"existing_network_id": "vpc-0a1b2c3d4e5f60718"}`}))
	if err != nil {
		t.Fatal(err)
	}
}
