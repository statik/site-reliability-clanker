package sim

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/detect"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// simulatorWithDataLayer returns a simulator whose platform repository declares a database
// volume of 2 GiB. No application runs, as on a target that is not deployed.
func simulatorWithDataLayer(t *testing.T, raised chan detect.Finding) *Simulator {
	t.Helper()
	db := storetest.Open(t)
	clock, err := NewClock(db)
	if err != nil {
		t.Fatal(err)
	}
	f := forge.New(db)
	tfvars := "env_name              = \"local\"\npostgres_storage_gb   = 2\n"
	if err := f.Seed(orchestrator.PlatformRepo, map[string]fs.FS{
		"platform/stacks/local/30-data": fstest.MapFS{
			"terraform.tfvars": {Data: []byte(tfvars)}}}, "Seed platform"); err != nil {
		t.Fatal(err)
	}
	log := audit.New(db, clock)
	m := monitor.New(db, clock)
	return &Simulator{DB: db, Clock: clock, Audit: log, Forge: f, Monitor: m, Env: "local",
		Orch: &orchestrator.Orchestrator{DB: db, Forge: f,
			Envs: map[string]orchestrator.EnvConfig{"local": {Cloud: "local"}}},
		Detector: &detect.Detector{DB: db, Audit: log, Forge: f, Monitor: m, Clock: clock,
			OnFinding: func(_ context.Context, f detect.Finding) { raised <- f }}}
}

func TestStorageGrowthSceneMakesTheForecastFire(t *testing.T) {
	raised := make(chan detect.Finding, 2)
	s := simulatorWithDataLayer(t, raised)
	if err := s.RunScene(context.Background(), "storage-growth"); err != nil {
		t.Fatal(err)
	}
	samples, err := s.Monitor.VolumeHistory("local", monitor.DatabaseVolume, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 8 {
		t.Fatalf("samples = %d, want 8", len(samples))
	}
	finding := <-raised
	// 75 percent of 2 GiB is used and 2 percent of 2 GiB is added each day:
	// 1.50 GiB + 90 x 0.04 GiB = 5.10 GiB, which needs 6 GiB.
	if finding.Detail["running"] != "2" || finding.Detail["value"] != "6" {
		t.Errorf("the finding grows %v GiB to %v GiB, want 2 to 6", finding.Detail["running"],
			finding.Detail["value"])
	}
	fullAt := store.ParseTime(finding.Detail["full_at"].(string))
	days := fullAt.Sub(s.Clock.Now()).Hours() / 24
	if days < 12 || days > 13 {
		t.Errorf("the volume is full in %.1f days, want 12.5", days)
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if last := state.Scenes[len(state.Scenes)-1]; last.Name != "storage-growth" || !last.Done {
		t.Errorf("last scene = %+v, want storage-growth and done", last)
	}

	if err := s.RunScene(context.Background(), "storage-growth"); err != nil {
		t.Fatal(err)
	}
	findings, err := s.Detector.Findings()
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Errorf("after a second run: %d findings, want 1", len(findings))
	}
}

func TestStorageGrowthSceneNeedsTheDeclaredSize(t *testing.T) {
	s := simulatorWithDataLayer(t, make(chan detect.Finding, 1))
	_, err := s.Forge.Commit(forge.CommitRequest{Repo: orchestrator.PlatformRepo,
		Branch: "main", Base: "main", Author: "test", Message: "Remove the size",
		Files: map[string]string{
			"platform/stacks/local/30-data/terraform.tfvars": "env_name = \"local\"\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RunScene(context.Background(), "storage-growth"); err == nil {
		t.Error("the scene ran with no declared size")
	}
}
