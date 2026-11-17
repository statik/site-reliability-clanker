package runbook_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/runbook"
)

const restart = `{
  "description": "Restart one deployment.",
  "tier": "T1",
  "incident_mitigation": true,
  "inputs": {
    "deployment": {"type": "string", "description": "The deployment.",
      "values": ["paperboat-api", "paperboat-web"]},
    "replicas": {"type": "integer", "description": "The number of pods."}
  },
  "outputs": {
    "file": {"type": "string", "description": "A path."},
    "objects": {"type": "integer", "description": "A count."}
  }
}`

func parse(t *testing.T) runbook.Definition {
	t.Helper()
	d, err := runbook.Parse("restart-deployment", []byte(restart))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestParseReadsADefinition(t *testing.T) {
	d := parse(t)
	if d.Name != "restart-deployment" || d.Tier != "T1" || !d.IncidentMitigation {
		t.Errorf("definition = %+v", d)
	}
	if got := d.Inputs["deployment"].Values; len(got) != 2 {
		t.Errorf("values = %q", got)
	}
}

func TestParseRefusesABadDefinition(t *testing.T) {
	cases := map[string]struct{ name, raw, want string }{
		"bad name":            {"Restart", `{"description":"x","tier":"T1"}`, "name"},
		"name that steps out": {"../x", `{"description":"x","tier":"T1"}`, "name"},
		"not JSON":            {"a-job", `description: x`, "decode"},
		"empty file":          {"a-job", ``, "decode"},
		"unknown field":       {"a-job", `{"description":"x","tier":"T1","steps":[]}`, "unknown field"},
		"name in the file": {"a-job", `{"name":"other","description":"x","tier":"T1"}`,
			"name of the file"},
		"no description": {"a-job", `{"description":" ","tier":"T1"}`, "no description"},
		"no tier":        {"a-job", `{"description":"x"}`, "tier"},
		"unknown tier":   {"a-job", `{"description":"x","tier":"T9"}`, "tier"},
		"input name in upper case": {"a-job", `{"description":"x","tier":"T1","inputs":
			{"Deployment":{"type":"integer","description":"d"}}}`, "not valid"},
		"input without description": {"a-job", `{"description":"x","tier":"T1","inputs":
			{"count":{"type":"integer"}}}`, "no description"},
		"input of unknown type": {"a-job", `{"description":"x","tier":"T1","inputs":
			{"on":{"type":"boolean","description":"d"}}}`, "type"},
		"free text input": {"a-job", `{"description":"x","tier":"T1","inputs":
			{"target":{"type":"string","description":"d"}}}`, "must list its values"},
		"empty value": {"a-job", `{"description":"x","tier":"T1","inputs":
			{"target":{"type":"string","description":"d","values":[""]}}}`, "empty value"},
		"integer input with values": {"a-job", `{"description":"x","tier":"T1","inputs":
			{"count":{"type":"integer","description":"d","values":["1"]}}}`, "no list of values"},
		"output with values": {"a-job", `{"description":"x","tier":"T1","outputs":
			{"file":{"type":"string","description":"d","values":["a"]}}}`, "no list of values"},
		"output of unknown type": {"a-job", `{"description":"x","tier":"T1","outputs":
			{"file":{"type":"path","description":"d"}}}`, "type"},
	}
	for label, tc := range cases {
		_, err := runbook.Parse(tc.name, []byte(tc.raw))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want one with %q", label, err, tc.want)
		}
	}
}

func TestCheckInputs(t *testing.T) {
	d := parse(t)
	good := map[string]string{"deployment": "paperboat-web", "replicas": "3"}
	if err := d.CheckInputs(good); err != nil {
		t.Fatalf("good inputs: %v", err)
	}
	cases := map[string]struct {
		params map[string]string
		want   string
	}{
		"no parameters": {nil, `needs the input "deployment"`},
		"a missing input": {map[string]string{"deployment": "paperboat-api"},
			`needs the input "replicas"`},
		"an unknown input": {map[string]string{"deployment": "paperboat-api", "replicas": "1",
			"namespace": "kube-system"}, `has no input "namespace"`},
		"a value outside the set": {map[string]string{"deployment": "postgres", "replicas": "1"},
			"is not one of paperboat-api, paperboat-web"},
		"an empty value": {map[string]string{"deployment": "", "replicas": "1"}, "is not one of"},
		"shell text as a value": {map[string]string{"deployment": "paperboat-api; rm -r /",
			"replicas": "1"}, "is not one of"},
		"text for an integer": {map[string]string{"deployment": "paperboat-api",
			"replicas": "many"}, "is not an integer"},
		"a fraction for an integer": {map[string]string{"deployment": "paperboat-api",
			"replicas": "1.5"}, "is not an integer"},
		"an integer with a space": {map[string]string{"deployment": "paperboat-api",
			"replicas": " 1"}, "is not an integer"},
	}
	for label, tc := range cases {
		err := d.CheckInputs(tc.params)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want one with %q", label, err, tc.want)
		}
	}
}

func TestCheckInputsOfARunbookWithoutInputs(t *testing.T) {
	d, err := runbook.Parse("db-maintenance", []byte(`{"description":"x","tier":"T1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CheckInputs(nil); err != nil {
		t.Errorf("no parameters: %v", err)
	}
	if err := d.CheckInputs(map[string]string{}); err != nil {
		t.Errorf("an empty map: %v", err)
	}
	err = d.CheckInputs(map[string]string{"table": "fish"})
	if err == nil || !strings.Contains(err.Error(), "its inputs are (none)") {
		t.Errorf("an undeclared parameter: error = %v", err)
	}
}

func TestCheckOutputs(t *testing.T) {
	d := parse(t)
	got, err := d.CheckOutputs(map[string]any{"file": "/backups/a.dump", "objects": "42"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"file": "/backups/a.dump", "objects": int64(42)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("outputs = %#v, want %#v", got, want)
	}
	cases := map[string]struct {
		reported map[string]any
		want     string
	}{
		"nothing reported": {nil, `did not report its output "file"`},
		"one is missing":   {map[string]any{"file": "a"}, `did not report its output "objects"`},
		"one is not declared": {map[string]any{"file": "a", "objects": "1", "token": "s3cr3t"},
			`reported the output "token"`},
		"text for an integer":      {map[string]any{"file": "a", "objects": "few"}, "is not an integer"},
		"a number from the runner": {map[string]any{"file": "a", "objects": float64(3)}, "not text"},
		"a value that is too long": {map[string]any{"file": strings.Repeat("a", 501),
			"objects": "1"}, "more than 500"},
	}
	for label, tc := range cases {
		_, err := d.CheckOutputs(tc.reported)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want one with %q", label, err, tc.want)
		}
	}
}

// TestEveryRunbookOfTheRepositoryHasADefinition reads the real files. A script without a
// definition cannot run, and a definition without a script fails when it runs.
func TestEveryRunbookOfTheRepositoryHasADefinition(t *testing.T) {
	dir := filepath.Join("..", "..", "runbooks", "jobs")
	scripts, err := filepath.Glob(filepath.Join(dir, "*.sh"))
	if err != nil || len(scripts) == 0 {
		t.Fatalf("no scripts in %s: %v", dir, err)
	}
	definitions, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != len(scripts) {
		t.Errorf("%d definitions for %d scripts", len(definitions), len(scripts))
	}
	for _, script := range scripts {
		checkScript(t, script)
	}
}

// checkScript reads the definition beside a script and looks in the script for each input
// and each output of the definition.
func checkScript(t *testing.T, script string) {
	t.Helper()
	name := strings.TrimSuffix(filepath.Base(script), ".sh")
	raw, err := os.ReadFile(strings.TrimSuffix(script, ".sh") + ".json")
	if err != nil {
		t.Errorf("script %s has no definition: %v", name, err)
		return
	}
	d, err := runbook.Parse(name, raw)
	if err != nil {
		t.Error(err)
		return
	}
	text, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	for input := range d.Inputs {
		if !strings.Contains(string(text), "PARAM_"+strings.ToUpper(input)) {
			t.Errorf("script %s does not read its input %s", name, input)
		}
	}
	for output := range d.Outputs {
		if !strings.Contains(string(text), `echo "`+output+`=`) {
			t.Errorf("script %s does not write its output %s", name, output)
		}
	}
}
