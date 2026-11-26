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
		"runbooks/README.md": "# Runbooks\n",
		"platform/stacks/local/50-app/terraform.tfvars": "env_name = \"local\"\n" +
			"paperboat_version = \"1.0.0\"\n",
		"platform/stacks/local/40-platform/terraform.tfvars": "app_name = \"paperboat\"\n"} {
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
	dir := checkoutWith(t, "harbor")
	packtest.AddRecords(t, filepath.Join(dir, "apps", "harbor"))
	source := os.DirFS(dir)
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
	// Nothing of the pack is outside apps/harbor/: the rest is platform/ of the source.
	inPack, outside := []string{}, []string{}
	for _, file := range repositoryFiles(t, f, orchestrator.PlatformRepo, "") {
		if rest, ok := strings.CutPrefix(file, "apps/harbor/"); ok {
			inPack = append(inPack, rest)
		} else {
			outside = append(outside, file)
		}
	}
	wantOutside := []string{"platform/CONTRACT.md",
		"platform/stacks/local/40-platform/terraform.tfvars",
		"platform/stacks/local/50-app/app.tf", "platform/stacks/local/50-app/terraform.tfvars"}
	if !slices.Equal(outside, wantOutside) {
		t.Errorf("the platform repository has %v outside the pack, want %v", outside,
			wantOutside)
	}
	// The application layer has the names of the pack, not those of the source files.
	platformVars := "app_name            = \"harbor\"\nworkloads_namespace = true\n"
	for file, want := range map[string]string{
		"platform/stacks/local/50-app/app.tf":                `variable "harbor_release" {`,
		"platform/stacks/local/50-app/terraform.tfvars":      "harbor_release   = \"3.4.0\"\n",
		"platform/stacks/local/40-platform/terraform.tfvars": platformVars,
	} {
		text, err := f.ReadFile(orchestrator.PlatformRepo, "main", file)
		if err != nil || !strings.Contains(string(text), want) ||
			strings.Contains(string(text), "paperboat") {
			t.Errorf("%s of the platform repository has no %q, or names the source pack: "+
				"%v\n%s", file, want, err, text)
		}
	}
	wantPack := []string{"chart/Chart.yaml", "charts/harbor-3.5.0.tgz", "extras/Chart.yaml",
		"extras/templates/job.yaml", "releases/3.4.0.json", "releases/3.5.0.json",
		"values/values-aws.yaml", "values/values.yaml"}
	if !slices.Equal(inPack, wantPack) {
		t.Errorf("apps/harbor/ of the platform repository\n got %v\nwant %v", inPack, wantPack)
	}
	// The simulator publishes the later releases; the seed has only the first note, and
	// the chart.
	got := repositoryFiles(t, f, "harbor", "")
	if want := []string{"chart/Chart.yaml", "notes/3.4.0.md"}; !slices.Equal(got, want) {
		t.Errorf("the releases repository has %v, want %v", got, want)
	}
}

// repositoryFiles returns the paths of the files below dir on main of a repository, in
// the order of the paths.
func repositoryFiles(t *testing.T, f *forge.Forge, repo, dir string) []string {
	t.Helper()
	entries, err := f.Tree(repo, "main", dir)
	if err != nil {
		t.Fatal(err)
	}
	files := []string{}
	for _, entry := range entries {
		if entry.Type == "tree" {
			files = append(files, repositoryFiles(t, f, repo, entry.Path)...)
		} else {
			files = append(files, entry.Path)
		}
	}
	slices.Sort(files)
	return files
}
