package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/jobspec"
)

type entry struct {
	name, body string
	mode       int64
}

func archive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipped := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipped)
	for _, e := range entries {
		if err := writer.WriteHeader(&tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.body)),
			Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func newRunner(t *testing.T) *Runner {
	t.Helper()
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCheckoutIsUnpackedOnceForOneID(t *testing.T) {
	r := newRunner(t)
	checkout := Checkout{ID: "WI-0001/abc", Archive: archive(t,
		entry{"platform/stacks/local/50-app/main.tf", "module {}\n", 0o644},
		entry{"jobs/run.sh", "#!/bin/sh\n", 0o755})}
	dir, err := r.checkoutDir(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dir, r.WorkRoot) {
		t.Fatalf("the checkout %s is outside the directory of the runner", dir)
	}
	info, err := os.Stat(filepath.Join(dir, "jobs", "run.sh"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("run.sh is not executable: %v", err)
	}
	// A plan and its apply use the same directory: the second use does not unpack again.
	marker := filepath.Join(dir, "written-by-the-plan")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := r.checkoutDir(Checkout{ID: "WI-0001/abc", Archive: []byte("not read")})
	if err != nil || again != dir {
		t.Fatalf("second use = %s, %v", again, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the second use unpacked the checkout again")
	}
}

func TestCheckoutRefusesBadInput(t *testing.T) {
	r := newRunner(t)
	good := archive(t, entry{"a.txt", "a", 0o644})
	cases := map[string]Checkout{
		"no ID":                {Archive: good},
		"ID that steps out":    {ID: "../outside", Archive: good},
		"absolute ID":          {ID: "/tmp/x", Archive: good},
		"not an archive":       {ID: "bad/archive", Archive: []byte("plain text")},
		"entry that steps out": {ID: "bad/entry", Archive: archive(t, entry{"../escape.txt", "x", 0o644})},
	}
	for name, checkout := range cases {
		if _, err := r.checkoutDir(checkout); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := os.Stat(filepath.Join(r.WorkRoot, "checkouts", "escape.txt")); err == nil {
		t.Fatal("an entry was written outside its checkout")
	}
	// A checkout that failed is not marked complete, so a later good archive works.
	if _, err := r.checkoutDir(Checkout{ID: "bad/archive", Archive: good}); err != nil {
		t.Fatalf("a good archive after a bad one: %v", err)
	}
}

func TestCheckoutUsesAGivenDirectory(t *testing.T) {
	r := newRunner(t)
	dir := t.TempDir()
	got, err := r.checkoutDir(Checkout{Dir: dir})
	if err != nil || got != dir {
		t.Fatalf("checkout = %s, %v", got, err)
	}
}

func TestCredentialFileIsPrivateAndRemoved(t *testing.T) {
	r := newRunner(t)
	path, remove, err := r.credentialFile("apiVersion: v1\n")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v; want 600", info.Mode().Perm(), err)
	}
	remove()
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the credential file is still there")
	}
	path, remove, err = r.credentialFile("")
	if err != nil || path != "" {
		t.Fatalf("an empty credential gave the file %q, %v", path, err)
	}
	remove()
}

func TestApplyRefusesAPlanWithAnotherHash(t *testing.T) {
	r := newRunner(t)
	_, err := r.Apply(context.Background(), Job{Env: "local", Cloud: "local", Layer: "50-app"},
		[]byte("the plan"), jobspec.SHA256([]byte("another plan")))
	if err == nil || !strings.Contains(err.Error(), "not the approved hash") {
		t.Fatalf("error = %v, want a refusal before anything runs", err)
	}
}

func TestRunbookNeedsTheScriptInTheCheckout(t *testing.T) {
	r := newRunner(t)
	checkout := Checkout{ID: "runbooks/abc", Archive: archive(t,
		entry{"jobs/hello.sh", "#!/bin/bash\necho \"hello $PARAM_NAME from $STATE_DIR\"\n", 0o755})}
	var log bytes.Buffer
	if err := r.Runbook(context.Background(), RunbookJob{Env: "local", Checkout: checkout,
		Name: "hello", Params: map[string]string{"name": "carol"}, Log: &log}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "hello carol from "+r.StateDir("local")) {
		t.Fatalf("log = %q", log.String())
	}
	err := r.Runbook(context.Background(), RunbookJob{Env: "local", Checkout: checkout,
		Name: "missing", Log: &log})
	if err == nil || !strings.Contains(err.Error(), "is not in the checkout") {
		t.Fatalf("error = %v", err)
	}
}
