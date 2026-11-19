package detect

import (
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// The release check reads the repository, the directory, the layer and the variable from
// the pack. This test uses a pack with none of the names of the example application.
func TestTheReleaseCheckUsesTheNamesOfThePack(t *testing.T) {
	db := storetest.Open(t)
	app := packtest.Second(t)
	clock := fixedClock{time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)}
	f := forge.New(db)
	if err := f.Seed(app.Releases.Repository, notesOf(t, app), "Releases"); err != nil {
		t.Fatal(err)
	}
	now := store.FormatTime(clock.Now())
	if err := store.SetLayerOutputs(db, "local", app.Layer,
		map[string]any{app.VersionVariable: "3.4.0"}, now); err != nil {
		t.Fatal(err)
	}
	raised := make(chan Finding, 2)
	d := &Detector{DB: db, Audit: audit.New(db, clock), Forge: f, Clock: clock, App: app,
		OnFinding: func(_ context.Context, found Finding) { raised <- found }}
	if err := d.CheckReleases(context.Background(), "local"); err != nil {
		t.Fatal(err)
	}
	findings, err := d.Findings()
	if err != nil || len(findings) != 1 {
		t.Fatalf("%d findings, %v", len(findings), err)
	}
	got := findings[0]
	if got.Title != "Harbor 3.5.0 is published; local runs 3.4.0" {
		t.Errorf("title = %q", got.Title)
	}
	want := map[string]any{"kind": "app-upgrade", "layer": "50-app", "variable": "harbor_release",
		"value": "3.5.0", "running": "3.4.0", "release_note": "notes/3.5.0.md"}
	for key, value := range want {
		if got.Detail[key] != value {
			t.Errorf("detail %s = %v, want %v", key, got.Detail[key], value)
		}
	}
	// A second check opens no second finding for the same release.
	if err := d.CheckReleases(context.Background(), "local"); err != nil {
		t.Fatal(err)
	}
	if findings, _ := d.Findings(); len(findings) != 1 {
		t.Errorf("%d findings after the second check", len(findings))
	}
}

// notesOf returns the release notes of the pack, as the content of its releases repository.
func notesOf(t *testing.T, app pack.Pack) map[string]fs.FS {
	t.Helper()
	notes, err := fs.Sub(app.Files(), app.Releases.Directory)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]fs.FS{app.Releases.Directory: notes}
}
