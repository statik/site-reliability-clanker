package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// checkoutWith returns a directory with the smallest source files and the packs with the
// given names, each a copy of the second pack of the tests.
func checkoutWith(t *testing.T, packs ...string) string {
	t.Helper()
	dir := t.TempDir()
	second := filepath.Join(packtest.SecondSource(t), packtest.Second(t).Dir())
	for _, name := range packs {
		if err := os.CopyFS(filepath.Join(dir, "apps", name), os.DirFS(second)); err != nil {
			t.Fatal(err)
		}
	}
	for file, text := range map[string]string{"platform/CONTRACT.md": "# Contract\n",
		"runbooks/README.md": "# Runbooks\n"} {
		path := filepath.Join(dir, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestTheCommandFindsTheOnePackOfTheSourceFiles(t *testing.T) {
	source := os.DirFS(checkoutWith(t, "harbor"))
	app, err := loadPack(source, "")
	if err != nil || app.Name != "harbor" || app.Dir() != "apps/harbor" {
		t.Fatalf("pack %q in %q, %v", app.Name, app.Dir(), err)
	}
	two := os.DirFS(checkoutWith(t, "harbor", "quay"))
	if _, err := loadPack(two, ""); err == nil || !strings.Contains(err.Error(), "--pack") {
		t.Errorf("two packs and no flag: %v", err)
	}
	if app, err := loadPack(two, "apps/quay"); err != nil || app.Dir() != "apps/quay" {
		t.Errorf("two packs and --pack: %q, %v", app.Dir(), err)
	}
	example, err := loadPack(os.DirFS(packtest.Root(t)), "")
	if err != nil || example.Name != packtest.Example(t).Name {
		t.Errorf("the example pack of the repository: %q, %v", example.Name, err)
	}
}

func TestTheForgeIsSeededFromThePack(t *testing.T) {
	source := os.DirFS(checkoutWith(t, "harbor"))
	app, err := loadPack(source, "")
	if err != nil {
		t.Fatal(err)
	}
	f := forge.New(storetest.Open(t))
	if err := seedForge(f, source, app); err != nil {
		t.Fatal(err)
	}
	repos, err := f.Repos()
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(repos)
	want := []string{"harbor", orchestrator.PlatformRepo, orchestrator.RunbooksRepo}
	slices.Sort(want)
	if !slices.Equal(repos, want) {
		t.Fatalf("repositories = %v, want %v", repos, want)
	}
	for repo, path := range map[string]string{
		orchestrator.PlatformRepo: "apps/harbor/chart/Chart.yaml",
		"harbor":                  "notes/3.4.0.md",
	} {
		if _, err := f.ReadFile(repo, "main", path); err != nil {
			t.Errorf("%s has no %s: %v", repo, path, err)
		}
	}
	// The simulator publishes the later releases; the seed has only the first one.
	if _, err := f.ReadFile("harbor", "main", "notes/3.5.0.md"); err == nil {
		t.Error("the seed has a release after the first one")
	}
	if _, err := f.ReadFile("harbor", "main", "chart/Chart.yaml"); err != nil {
		t.Errorf("the releases repository has no chart: %v", err)
	}
}
