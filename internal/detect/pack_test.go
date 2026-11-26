package detect

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
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

// A release record beside a note is not a release: only a note is.
func TestTheReleaseCheckReadsOnlyTheNotes(t *testing.T) {
	app := packtest.Second(t)
	d, raised := releaseDetector(t, app, fstest.MapFS{"3.5.0.md": {Data: []byte("# 3.5.0\n")},
		"9.9.9.json": {Data: []byte("{}")}, "README.txt": {Data: []byte("x")}})
	if err := d.CheckReleases(context.Background(), "local"); err != nil {
		t.Fatal(err)
	}
	findings, err := d.Findings()
	if err != nil || len(findings) != 1 || findings[0].Detail["value"] != "3.5.0" {
		t.Fatalf("findings %+v, %v", findings, err)
	}
	<-raised
}

// For a pack with records, a release whose record is not in the platform repository goes
// to a person, who adds the record in a pull request. When the record is there, the
// release becomes an upgrade for the agent.
func TestAReleaseWithNoRecordGoesToAPerson(t *testing.T) {
	app := packtest.SecondWithRecords(t)
	d, raised := releaseDetector(t, app, fstest.MapFS{"3.5.0.md": {Data: []byte("# 3.5.0\n")}})
	if err := d.Forge.Seed(orchestrator.PlatformRepo, map[string]fs.FS{
		"apps/harbor/releases": fstest.MapFS{"3.4.0.json": {Data: []byte("{}")}}},
		"Seed"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := d.CheckReleases(context.Background(), "local"); err != nil {
			t.Fatal(err)
		}
	}
	findings, err := d.Findings()
	if err != nil || len(findings) != 1 {
		t.Fatalf("%d findings, %v", len(findings), err)
	}
	got := findings[0]
	want := "Harbor 3.5.0 is published; add its record apps/harbor/releases/3.5.0.json in a " +
		"pull request"
	if got.Title != want || got.Detail["kind"] != ReleaseRecordKind ||
		got.Detail["variable"] != nil || got.Key != "release-record:local:3.5.0" {
		t.Errorf("finding %+v", got)
	}
	select {
	case f := <-raised:
		t.Fatalf("the agent got the finding %s", f.Key)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := d.Forge.Commit(forge.CommitRequest{Repo: orchestrator.PlatformRepo,
		Branch: "main", Base: "main", Author: "person", Message: "Add the record of 3.5.0",
		Files: map[string]string{"apps/harbor/releases/3.5.0.json": "{}"}}); err != nil {
		t.Fatal(err)
	}
	if err := d.CheckReleases(context.Background(), "local"); err != nil {
		t.Fatal(err)
	}
	upgrade := <-raised
	if upgrade.Detail["kind"] != "app-upgrade" || upgrade.Detail["value"] != "3.5.0" {
		t.Errorf("the agent got %+v", upgrade)
	}
}

// releaseDetector returns a detector of the pack on the environment local, which runs the
// first release, and a releases repository with these notes. The channel gets each finding
// that goes to the agent.
func releaseDetector(t *testing.T, app pack.Pack, notes fstest.MapFS) (*Detector, chan Finding) {
	t.Helper()
	db := storetest.Open(t)
	clock := fixedClock{time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)}
	f := forge.New(db)
	if err := f.Seed(app.Releases.Repository, map[string]fs.FS{app.Releases.Directory: notes},
		"Releases"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetLayerOutputs(db, "local", app.Layer,
		map[string]any{app.VersionVariable: app.Releases.First},
		store.FormatTime(clock.Now())); err != nil {
		t.Fatal(err)
	}
	raised := make(chan Finding, 2)
	return &Detector{DB: db, Audit: audit.New(db, clock), Forge: f, Clock: clock, App: app,
		OnFinding: func(_ context.Context, found Finding) { raised <- found }}, raised
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
