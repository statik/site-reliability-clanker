package pack_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/statik/site-reliability-clanker/internal/pack"
)

const evalCases = `{"schema": 1, "task": "investigator_diagnosis", "samples": [{"id": "one"}]}`

// withEvals returns the files of a pack that names a directory of eval cases, with one
// case file that has the given content.
func withEvals(t *testing.T, file, content string) fstest.MapFS {
	t.Helper()
	files := packFiles(manifest(t, set("evals", "cases")))
	files["p/cases/"+file] = &fstest.MapFile{Data: []byte(content)}
	return files
}

func TestLoadChecksTheRunbooksOfThePack(t *testing.T) {
	raw := manifest(t, func(map[string]any) {})
	definition := `{"description": "A job.", "tier": "T1"}`
	cases := map[string]struct {
		change func(fstest.MapFS)
		want   string
	}{
		"a definition with no script": {func(f fstest.MapFS) {
			f["p/jobs/prune.json"] = &fstest.MapFile{Data: []byte(definition)}
		}, "no script prune.sh"},
		"a script with no definition": {func(f fstest.MapFS) {
			f["p/jobs/prune.sh"] = &fstest.MapFile{Data: []byte("#!/bin/sh\n")}
		}, "not a definition"},
		"a definition that is not correct": {func(f fstest.MapFS) {
			f["p/jobs/prune.json"] = &fstest.MapFile{Data: []byte(`{"tier": "T9"}`)}
			f["p/jobs/prune.sh"] = &fstest.MapFile{Data: []byte("#!/bin/sh\n")}
		}, "prune"},
		"a policy in a directory below": {func(f fstest.MapFS) {
			f["p/jobs/policy/tier.rego"] = &fstest.MapFile{Data: []byte("package tier\n")}
		}, "files only"},
		"a restart of a deployment of another application": {func(f fstest.MapFS) {
			f["p/jobs/restart-deployment.json"] = &fstest.MapFile{Data: []byte(
				`{"description": "Restart.", "tier": "T1", "incident_mitigation": true, ` +
					`"inputs": {"deployment": {"type": "string", "description": "The one.", ` +
					`"values": ["coredns"]}}}`)}
		}, "not a deployment of the pack"},
		"no runbook": {func(f fstest.MapFS) {
			delete(f, "p/jobs/restart-deployment.json")
			delete(f, "p/jobs/restart-deployment.sh")
			f["p/jobs/.keep"] = &fstest.MapFile{}
		}, "not a definition"},
	}
	for name, tc := range cases {
		files := packFiles(raw)
		tc.change(files)
		if _, err := pack.Load(files, "p"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want one with %q", name, err, tc.want)
		}
	}
	if _, err := pack.Load(packFiles(manifest(t, set("runbooks", "../runbooks"))), "p"); err == nil {
		t.Error("a runbooks directory outside the pack loaded")
	}
}

func TestLoadChecksTheEvalCasesOfThePack(t *testing.T) {
	app, err := pack.Load(withEvals(t, "investigator_diagnosis.json", evalCases), "p")
	if err != nil {
		t.Fatalf("a pack with correct eval cases does not load: %v", err)
	}
	parts := app.CheckParts()
	if len(parts) != 2 || parts[1].Name != "eval cases" ||
		parts[1].Detail != "investigator_diagnosis" {
		t.Errorf("the checks of the parts are %+v", parts)
	}
	cases := map[string][2]string{
		"a name that is not a task": {"Not A Task.json", evalCases},
		"another task in the file":  {"author_correctness.json", evalCases},
		"another schema": {"investigator_diagnosis.json",
			`{"schema": 2, "task": "investigator_diagnosis", "samples": [{"id": "one"}]}`},
		"no sample": {"investigator_diagnosis.json",
			`{"schema": 1, "task": "investigator_diagnosis", "samples": []}`},
		"an id two times": {"investigator_diagnosis.json",
			`{"schema": 1, "task": "investigator_diagnosis", "samples": [{"id": "a"}, {"id": "a"}]}`},
		"a sample that is not an object": {"investigator_diagnosis.json",
			`{"schema": 1, "task": "investigator_diagnosis", "samples": ["a"]}`},
		"text that is not JSON": {"investigator_diagnosis.json", `not json`},
	}
	for name, tc := range cases {
		if _, err := pack.Load(withEvals(t, tc[0], tc[1]), "p"); err == nil ||
			!strings.Contains(err.Error(), "eval cases") {
			t.Errorf("%s: error %v", name, err)
		}
	}
}

func TestHashChangesWithAFileItsNameAndItsMode(t *testing.T) {
	base := func() fstest.MapFS {
		return fstest.MapFS{"a/one": {Data: []byte("1")}, "b": {Data: []byte("2"), Mode: 0o644}}
	}
	first, err := pack.Hash(base())
	second, _ := pack.Hash(base())
	if err != nil || len(first) != 64 || first != second {
		t.Fatalf("hash %q and %q, %v", first, second, err)
	}
	changes := map[string]func(fstest.MapFS){
		"other content": func(f fstest.MapFS) { f["b"] = &fstest.MapFile{Data: []byte("3")} },
		"other name": func(f fstest.MapFS) {
			f["c"] = f["b"]
			delete(f, "b")
		},
		"a new file": func(f fstest.MapFS) { f["d"] = &fstest.MapFile{} },
		"can be run": func(f fstest.MapFS) { f["b"] = &fstest.MapFile{Data: []byte("2"), Mode: 0o755} },
		"moved bytes": func(f fstest.MapFS) {
			f["a/one"], f["b"] = &fstest.MapFile{Data: []byte("12")}, &fstest.MapFile{}
		},
	}
	for name, change := range changes {
		files := base()
		change(files)
		if got, _ := pack.Hash(files); got == first {
			t.Errorf("%s: the hash did not change", name)
		}
	}
}
