package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
	"github.com/statik/site-reliability-clanker/internal/packstore"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC) }

func packCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runPack(context.Background(), args, &out)
	return out.String(), err
}

func TestPackValidatePrintsOneLineForEachCheck(t *testing.T) {
	out, err := packCommand(t, "validate", filepath.Join(packtest.Root(t), "apps", "paperboat"))
	if err != nil {
		t.Fatalf("the example pack is not correct: %v\n%s", err, out)
	}
	for _, want := range []string{"ok    manifest: paperboat ", "ok    runbooks: db-backup, ",
		"ok    eval cases: investigator_diagnosis", "ok    hash: sha256 "} {
		if !strings.Contains(out, want) {
			t.Errorf("the output has no line %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "ok    ") {
			t.Errorf("a line of a correct pack does not start with ok: %q", line)
		}
	}
	second, err := packCommand(t, "validate", filepath.Join(packtest.SecondSource(t), "harbor"))
	if err != nil || !strings.Contains(second, "ok    eval cases: none") {
		t.Errorf("the second pack: %v\n%s", err, second)
	}
}

func TestPackValidateFailsForAPackThatIsNotCorrect(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join(packtest.SecondSource(t), "harbor"))); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "jobs", "restart-deployment.sh")); err != nil {
		t.Fatal(err)
	}
	out, err := packCommand(t, "validate", dir)
	if err == nil || !strings.HasPrefix(out, "FAIL  ") || !strings.Contains(out, "no script") {
		t.Errorf("error %v, output %q", err, out)
	}
	if out, err := packCommand(t, "validate", filepath.Join(dir, "nothing")); err == nil ||
		!strings.Contains(out, "FAIL") {
		t.Errorf("a directory that does not exist: %v, %q", err, out)
	}
	_, err = packCommand(t, "sign")
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("a command that does not exist: %v", err)
	}
}

// packRepository returns a git repository with the second pack at its root and the tag.
func packRepository(t *testing.T, tag string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join(packtest.SecondSource(t), "harbor"))); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "."},
		{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c",
			"commit.gpgsign=false", "commit", "--quiet", "-m", "The pack"},
		{"-c", "tag.gpgsign=false", "tag", tag}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func TestAnImportedPackStartsTheControlPlane(t *testing.T) {
	repo, data := packRepository(t, "v0.1.0"), t.TempDir()
	out, err := packCommand(t, "import", repo, "--tag", "v0.1.0", "--data", data)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	dir := filepath.Join(data, "packs", "harbor", "0.1.0")
	for _, want := range []string{"pack     harbor 0.1.0 imported", "source   " + repo + " tag v0.1.0",
		"sha256   ", "start    clanker up --pack " + dir} {
		if !strings.Contains(out, want) {
			t.Errorf("the output has no %q:\n%s", want, out)
		}
	}
	if _, err := packCommand(t, "import", repo, "--data", data); err == nil {
		t.Error("an import with no tag did not fail")
	}
	// The pack of a start can be a directory outside the source files.
	o := options{sourceDir: packtest.Root(t), source: os.DirFS(packtest.Root(t)), packDir: dir}
	if err := locatePack(&o); err != nil {
		t.Fatal(err)
	}
	if o.app.Name != "harbor" || o.packDisk != dir || o.packOrigin.Source != repo ||
		o.packOrigin.Tag != "v0.1.0" {
		t.Fatalf("pack %q on %q from %+v", o.app.Name, o.packDisk, o.packOrigin)
	}
	db := storetest.Open(t)
	f := forge.New(db)
	if err := seedForge(f, o.source, o.app); err != nil {
		t.Fatal(err)
	}
	for repository, path := range map[string]string{
		orchestrator.PlatformRepo: "apps/harbor/chart/Chart.yaml",
		orchestrator.RunbooksRepo: "jobs/restart-deployment.sh",
		"harbor":                  "notes/3.4.0.md",
	} {
		if _, err := f.ReadFile(repository, "main", path); err != nil {
			t.Errorf("%s has no %s: %v", repository, path, err)
		}
	}
	if _, err := f.ReadFile(orchestrator.RunbooksRepo, "main", "policy/tier.rego"); err != nil {
		t.Errorf("the runbooks repository lost the policy of the core: %v", err)
	}
	log := audit.New(db, fixedClock{})
	for range 2 {
		if err := recordPack(db, log, o); err != nil {
			t.Fatal(err)
		}
	}
	events, err := log.Query(audit.Filter{Types: []string{packLoadedKey}})
	if err != nil || len(events) != 1 {
		t.Fatalf("%d events for two starts with the same pack, %v", len(events), err)
	}
	data0 := events[0].Data
	if data0["name"] != "harbor" || data0["tag"] != "v0.1.0" || data0["source"] != repo ||
		data0["sha256"] != o.packOrigin.SHA256 {
		t.Errorf("the event has %v", data0)
	}
}

func TestThePackOfTheSourceFilesIsBuiltIn(t *testing.T) {
	root := packtest.Root(t)
	o := options{sourceDir: root, source: os.DirFS(root)}
	if err := locatePack(&o); err != nil {
		t.Fatal(err)
	}
	if o.packOrigin.Source != packstore.BuiltIn || len(o.packOrigin.SHA256) != 64 ||
		o.packDisk != filepath.Join(root, "apps", o.app.Name) {
		t.Errorf("pack on %q from %+v", o.packDisk, o.packOrigin)
	}
	named := options{sourceDir: root, source: os.DirFS(root), packDir: "apps/" + o.app.Name}
	if err := locatePack(&named); err != nil || named.packOrigin != o.packOrigin {
		t.Errorf("--pack with a directory of the source files: %+v, %v", named.packOrigin, err)
	}
}
