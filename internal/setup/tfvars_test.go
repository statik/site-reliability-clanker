package setup

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
)

func TestReadHomeVarsReadsTheExampleOfTheStack(t *testing.T) {
	vars, found, err := ReadHomeVars(filepath.Join("stack", "aws", "terraform.tfvars.example"))
	if err != nil || !found {
		t.Fatalf("found %v, %v", found, err)
	}
	want := HomeVars{Env: "prod", Region: "us-west-2", Network: "vpc-0123456789abcdef0",
		Subnets:     []string{"subnet-0123456789abcdef0", "subnet-0123456789abcdef1"},
		RouterGroup: "sg-0123456789abcdef0", ZoneID: "Z0EXAMPLE",
		Tags: map[string]string{"example:project": "example", "example:owner": "owner@example.com"}}
	if !reflect.DeepEqual(vars, want) {
		t.Errorf("vars = %+v, want %+v", vars, want)
	}
}

func TestReadHomeVarsWithoutAFileGivesNoValues(t *testing.T) {
	vars, found, err := ReadHomeVars(filepath.Join(t.TempDir(), "home.tfvars"))
	if err != nil || found || !reflect.DeepEqual(vars, HomeVars{}) {
		t.Errorf("vars %+v, found %v, err %v", vars, found, err)
	}
}

func TestHomeVarsSurviveAWriteAndARead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "home.tfvars")
	check := func(env, key, value, model string) bool {
		vars := HomeVars{Env: env, Region: "r", Network: "n", Subnets: []string{value, key},
			RouterGroup: "g", ZoneID: "z", Tags: map[string]string{key: value}, ModelKey: model}
		if err := WriteHomeVars(path, vars); err != nil {
			t.Fatal(err)
		}
		read, _, err := ReadHomeVars(path)
		if err != nil {
			t.Logf("read %q: %v", env, err)
			return false
		}
		if model == "" {
			read.ModelKey = ""
		}
		return reflect.DeepEqual(read, vars)
	}
	if err := quick.Check(check, nil); err != nil {
		t.Error(err)
	}
	for _, text := range []string{`a"b`, `c\d`, "${x}", "%{if}", "$${y}", "line\nend", "é"} {
		if !check(text, text, text, text) {
			t.Errorf("the value %q changed in the round trip", text)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, %v; want 0600", info.Mode(), err)
	}
}

func TestReadHomeVarsNamesTheLineAndNotTheValue(t *testing.T) {
	cases := map[string]string{
		"unknown_input = \"secret-value\"\n":        "has no input unknown_input",
		"zone_id = \"a\"\nregion = secret-value\n":  "line 2",
		"required_tags = {\n  k = \"secret-value\"": "line 2",
		"vpc_id = [\"secret-value\"]\n":             "another kind",
		"region = \"a\"\nregion = \"secret-value\"": "set twice",
		"zone_id = \"${secret-value}\"\n":           "template",
	}
	for text, want := range cases {
		path := filepath.Join(t.TempDir(), "home.tfvars")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, err := ReadHomeVars(path)
		if err == nil || !strings.Contains(err.Error(), want) ||
			strings.Contains(err.Error(), "secret-value") {
			t.Errorf("%q: err = %v, want %q and no value", text, err, want)
		}
	}
}

func TestMergeKeepsTheSavedValueOfAnEmptyInput(t *testing.T) {
	saved := HomeVars{Region: "us-west-2", Network: "vpc-1", Subnets: []string{"subnet-1"},
		RouterGroup: "sg-1", ZoneID: "Z1", Tags: map[string]string{"a": "1", "b": "2"},
		ModelKey: "key"}
	merged := merge(InstallInput{Env: "e", ZoneID: "Z2", Tags: map[string]string{"a": "",
		"c": "3"}}, saved)
	if merged.Env != "e" || merged.ZoneID != "Z2" || merged.Network != "vpc-1" ||
		merged.ModelKey != "key" || !maps.Equal(merged.Tags, map[string]string{"a": "1", "c": "3"}) {
		t.Errorf("merged = %+v", merged)
	}
}

func TestLineWriterHidesPrivateValuesInEachLine(t *testing.T) {
	lines := []string{}
	w := &lineWriter{send: func(line string) { lines = append(lines, line) },
		redact: newRedactor("owner@example.com", "", "owner")}
	for _, part := range []string{"tag = \"owner@exa", "mple.com\"\r\nsecond ", "owner"} {
		if _, err := w.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	want := []string{`tag = "(hidden)"`, "second (hidden)"}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %q, want %q", lines, want)
	}
}
