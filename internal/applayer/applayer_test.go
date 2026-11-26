package applayer_test

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/statik/site-reliability-clanker/internal/applayer"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
)

var update = flag.Bool("update", false, "write the generated files of the example pack into "+
	"platform/ of the checkout")

// platformTree returns the platform directory of the checkout.
func platformTree(t *testing.T) string {
	t.Helper()
	return filepath.Join(packtest.Root(t), "platform")
}

// The files of the example pack are committed, so that CI can validate the stacks. They
// must be what the seed writes. Run the test with -update to write them.
func TestTheCommittedFilesOfTheExamplePackAreTheGeneratedFiles(t *testing.T) {
	dir := platformTree(t)
	files, err := applayer.Files(packtest.Example(t), os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3*len(pack.Clouds) {
		t.Errorf("%d files for %d targets, want 3 for each", len(files), len(pack.Clouds))
	}
	for name, want := range files {
		file := filepath.Join(dir, filepath.FromSlash(name))
		if *update {
			if err := os.WriteFile(file, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(file)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("platform/%s is not the generated file (%v); run go test "+
				"./internal/applayer -run Committed -update\n got:\n%s\nwant:\n%s", name, err,
				got, want)
		}
	}
	checkFormat(t, files)
}

// checkFormat runs `tofu fmt -check` on each generated .tf and .tfvars file when tofu is
// installed. The CI job of the Go tests has no tofu; the job of the platform checks the
// committed files of the example pack with `tofu fmt -check -recursive platform`.
func checkFormat(t *testing.T, files map[string][]byte) {
	t.Helper()
	tofu, err := exec.LookPath("tofu")
	if err != nil {
		t.Log("tofu is not installed: the format of the generated files is not checked")
		return
	}
	dir := t.TempDir()
	for name, data := range files {
		file := filepath.Join(dir, strings.ReplaceAll(name, "/", "_"))
		if err := os.WriteFile(file, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(tofu, "fmt", "-check", "-diff", dir).CombinedOutput()
	if err != nil {
		t.Errorf("tofu fmt -check: %v\n%s", err, out)
	}
}

// platformWith returns a platform tree with the tfvars files of the stacks of the given
// targets.
func platformWith(clouds ...string) fstest.MapFS {
	tree := fstest.MapFS{}
	for _, cloud := range clouds {
		tree["stacks/"+cloud+"/50-app/terraform.tfvars"] = &fstest.MapFile{
			Data: []byte("env_name = \"" + cloud + "\"\nold_version = \"1.0.0\"\n")}
		tree["stacks/"+cloud+"/40-platform/terraform.tfvars"] = &fstest.MapFile{
			Data: []byte("env_name    = \"" + cloud + "\"\napp_name    = \"paperboat\"\n" +
				"public_port = 8088\n")}
	}
	return tree
}

func TestTheLayerOfASecondPackHasItsNames(t *testing.T) {
	harbor := packtest.Second(t)
	files, err := applayer.Files(harbor, os.DirFS(platformTree(t)))
	if err != nil {
		t.Fatal(err)
	}
	app := string(files["stacks/aws/50-app/app.tf"])
	for _, want := range []string{
		`variable "harbor_release" {`,
		`condition     = can(regex("^[0-9]+\\.[0-9]+\\.[0-9]+(-[a-z][a-z0-9]*)?$", ` +
			`var.harbor_release))`,
		`variable "gateway_replicas" {`,
		`condition     = var.gateway_replicas >= 1 && var.gateway_replicas <= 4 && ` +
			`floor(var.gateway_replicas) == var.gateway_replicas`,
		`error_message = "gateway_replicas must be a whole number from 1 to 4."`,
		"variable \"jobs_enabled\" {\n  type        = bool\n",
		`name            = "harbor"`,
		`version         = var.harbor_release`,
		`values          = true`, `extras          = true`, `records         = false`,
		`timeout_seconds = 600`,
		`secrets         = ["admin-password"]`,
		`"gateway.replicaCount" = tostring(var.gateway_replicas)`,
		`"jobservice.enabled"   = tostring(var.jobs_enabled)`,
		`service         = "harbor-ui"`, `paths           = ["/api", "/v2"]`,
		`timeout_seconds = 0`,
		`output "harbor_url" {`, `output "harbor_release" {`,
	} {
		if !strings.Contains(app, want) {
			t.Errorf("app.tf of harbor has no %q:\n%s", want, app)
		}
	}
	if strings.Contains(app, "paperboat") {
		t.Errorf("app.tf of harbor names the example pack:\n%s", app)
	}
	vars := string(files["stacks/local/50-app/terraform.tfvars"])
	want := "env_name         = \"local\"\nharbor_release   = \"3.4.0\"\n" +
		"gateway_replicas = 2\njobs_enabled     = false\n"
	if vars != want {
		t.Errorf("terraform.tfvars of local 50-app\n got %q\nwant %q", vars, want)
	}
	platform := string(files["stacks/local/40-platform/terraform.tfvars"])
	if !strings.Contains(platform, "app_name    = \"harbor\"\n") ||
		!strings.Contains(platform, "public_port = 8088\n") {
		t.Errorf("terraform.tfvars of local 40-platform:\n%s", platform)
	}
	checkFormat(t, files)
}

func TestAPackWithRecordsSaysSo(t *testing.T) {
	files, err := applayer.Files(packtest.SecondWithRecords(t), platformWith("local"))
	if err != nil {
		t.Fatal(err)
	}
	if app := string(files["stacks/local/50-app/app.tf"]); !strings.Contains(app,
		"records         = true") {
		t.Errorf("app.tf of a pack with records:\n%s", app)
	}
}

func TestTheFilesAreOnlyThoseOfTheTargetsInThePlatform(t *testing.T) {
	files, err := applayer.Files(packtest.Second(t), platformWith("local", "gcp"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	want := []string{
		"stacks/gcp/40-platform/terraform.tfvars", "stacks/gcp/50-app/app.tf",
		"stacks/gcp/50-app/terraform.tfvars", "stacks/local/40-platform/terraform.tfvars",
		"stacks/local/50-app/app.tf", "stacks/local/50-app/terraform.tfvars",
	}
	if !slices.Equal(names, want) {
		t.Errorf("files\n got %v\nwant %v", names, want)
	}
	// The tfvars keep the core line env_name and lose the line of another pack.
	if vars := string(files["stacks/gcp/50-app/terraform.tfvars"]); strings.Contains(vars,
		"old_version") || !strings.Contains(vars, `env_name         = "gcp"`) {
		t.Errorf("terraform.tfvars of gcp 50-app:\n%s", vars)
	}
}

func TestThePlatformWithoutTheLayerOrTheAppNameLineIsAnError(t *testing.T) {
	harbor := packtest.Second(t)
	if _, err := applayer.Files(harbor, fstest.MapFS{"CONTRACT.md": {}}); err == nil ||
		!strings.Contains(err.Error(), "no stack of the layer 50-app") {
		t.Errorf("no stack: %v", err)
	}
	for name, text := range map[string]string{
		"no line":   "public_port = 80\n",
		"two lines": "app_name = \"a\"\napp_name = \"b\"\n",
	} {
		tree := platformWith("aws")
		tree["stacks/aws/40-platform/terraform.tfvars"] = &fstest.MapFile{Data: []byte(text)}
		if _, err := applayer.Files(harbor, tree); err == nil ||
			!strings.Contains(err.Error(), "app_name") {
			t.Errorf("%s: %v", name, err)
		}
	}
	tree := platformWith("azure")
	delete(tree, "stacks/azure/50-app/terraform.tfvars")
	tree["stacks/azure/50-app/main.tf"] = &fstest.MapFile{Data: []byte("# main\n")}
	if _, err := applayer.Files(harbor, tree); err == nil ||
		!strings.Contains(err.Error(), "stacks/azure/50-app/terraform.tfvars") {
		t.Errorf("no tfvars: %v", err)
	}
}

func TestThePlatformTreeHasTheGeneratedFilesInPlace(t *testing.T) {
	source := platformWith("local")
	source["modules/shared/app/run.sh"] = &fstest.MapFile{Data: []byte("#!/bin/sh\n"),
		Mode: 0o755}
	source["stacks/local/50-app/app.tf"] = &fstest.MapFile{Data: []byte("# old\n")}
	harbor := packtest.Second(t)
	tree, err := applayer.Platform(harbor, source)
	if err != nil {
		t.Fatal(err)
	}
	files, err := applayer.Files(harbor, source)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		if got, err := fs.ReadFile(tree, name); err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is not the generated file: %v", name, err)
		}
	}
	if info, err := fs.Stat(tree, "modules/shared/app/run.sh"); err != nil ||
		info.Mode() != 0o755 {
		t.Errorf("the other files of the tree keep their mode: %v, %v", info, err)
	}
	listed := []string{}
	if err := fs.WalkDir(tree, ".", func(name string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			listed = append(listed, name)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"modules/shared/app/run.sh", "stacks/local/40-platform/terraform.tfvars",
		"stacks/local/50-app/app.tf", "stacks/local/50-app/terraform.tfvars"}
	if !slices.Equal(listed, want) {
		t.Errorf("the tree lists\n%v\nwant\n%v", listed, want)
	}
	if err := fstest.TestFS(tree, want...); err != nil {
		t.Error(err)
	}
}

func TestThePlatformTreeAddsAGeneratedFileThatIsNew(t *testing.T) {
	source := platformWith("aws")
	tree, err := applayer.Platform(packtest.Second(t), source)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(tree, "stacks/aws/50-app")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !slices.Equal(names, []string{applayer.AppFile, applayer.VarsFile}) {
		t.Errorf("stacks/aws/50-app lists %v", names)
	}
}
