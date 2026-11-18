package agent

import (
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/statik/site-reliability-clanker/internal/detect"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

const dataTfvars = "platform/stacks/local/30-data/terraform.tfvars"

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }

// agentWithDataLayer returns an agent whose platform repository holds the tfvars of the
// local data layer of this checkout.
func agentWithDataLayer(t *testing.T) *Agent {
	t.Helper()
	tfvars, err := os.ReadFile("../../" + dataTfvars)
	if err != nil {
		t.Fatal(err)
	}
	db := storetest.Open(t)
	f := forge.New(db)
	if err := f.Seed(orchestrator.PlatformRepo, map[string]fs.FS{
		"platform/stacks/local/30-data": fstest.MapFS{"terraform.tfvars": {Data: tfvars}}},
		"Seed platform"); err != nil {
		t.Fatal(err)
	}
	return &Agent{DB: db, Forge: f, Clock: wallClock{}, Orch: &orchestrator.Orchestrator{
		DB: db, Forge: f, Envs: map[string]orchestrator.EnvConfig{"local": {Cloud: "local"}}}}
}

func storageFinding(running, value string) detect.Finding {
	return detect.Finding{Detector: "storage", Env: "local",
		Key: "storage:local:postgres-data:" + running + "Gi",
		Title: "Database volume of local is full on 2026-11-21: 1.47 GiB of 2.00 GiB used, " +
			"growth 40 MiB each day",
		Detail: map[string]any{"kind": detect.StorageKind, "running": running, "value": value}}
}

func TestStoragePullSetsTheSizeThatTheFindingNames(t *testing.T) {
	a := agentWithDataLayer(t)
	d, err := a.storagePull(storageFinding("2", "5"))
	if err != nil {
		t.Fatal(err)
	}
	if d.pull.Title != "Grow the database volume from 2 GiB to 5 GiB" {
		t.Errorf("title = %q", d.pull.Title)
	}
	for _, part := range []string{"postgres_storage_gb in layer 30-data goes from 2 to 5",
		"full on 2026-11-21", "90 days of growth", "Rollback:", "local-path"} {
		if !strings.Contains(d.pull.Body, part) {
			t.Errorf("the body does not say %q:\n%s", part, d.pull.Body)
		}
	}
	branch, err := a.Forge.ReadFile(orchestrator.PlatformRepo, d.branch, dataTfvars)
	if err != nil {
		t.Fatal(err)
	}
	main, err := a.Forge.ReadFile(orchestrator.PlatformRepo, "main", dataTfvars)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(main, "postgres_storage_gb   = 2", "postgres_storage_gb   = 5", 1)
	if want == main || branch != want {
		t.Errorf("tfvars on the branch:\n%s\nwant only the size changed:\n%s", branch, want)
	}
}

func TestStoragePullRefusesAFindingThatItCannotApply(t *testing.T) {
	a := agentWithDataLayer(t)
	refused := map[string]detect.Finding{
		"no new size":             storageFinding("2", ""),
		"the size that is set":    storageFinding("2", "2"),
		"text that is not a size": storageFinding("2", "5\nenv_name = \"x\""),
	}
	for label, f := range refused {
		if d, err := a.storagePull(f); err == nil {
			t.Errorf("%s: pull request #%d was opened", label, d.pull.ID)
		}
	}
}
