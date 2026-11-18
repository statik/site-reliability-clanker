package api

import (
	"context"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/detect"
	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestStorageForecastIsOnTheFutureList(t *testing.T) {
	db := storetest.Open(t)
	now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	clock := fixedClock{now}
	m := monitor.New(db, clock)
	detector := &detect.Detector{DB: db, Audit: audit.New(db, clock), Monitor: m, Clock: clock}
	server := &Server{Detector: detector, Orch: &orchestrator.Orchestrator{DB: db,
		Envs: map[string]orchestrator.EnvConfig{"local": {Cloud: "local"}}}}

	entries, err := server.storageForecasts(store.FormatTime(now))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("with no samples: %v, want no entry", entries)
	}

	const mib = int64(1) << 20
	for day := range int64(8) {
		err := m.RecordVolume(monitor.VolumeSample{
			TS:  store.FormatTime(now.Add(time.Duration(day-7) * 24 * time.Hour)),
			Env: "local", Volume: monitor.DatabaseVolume, UsedBytes: (1228 + 40*day) * mib,
			TotalBytes: 2048 * mib})
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err = server.storageForecasts(store.FormatTime(now))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %v, want 1", entries)
	}
	want := map[string]any{"ts": "2026-11-21T12:00:00.000Z", "env": "local", "kind": "forecast",
		"source": "Volume samples", "title": "Database volume of local is full on " +
			"2026-11-21: 1.47 GiB of 2.00 GiB used, growth 40 MiB each day"}
	for name, value := range want {
		if entries[0][name] != value {
			t.Errorf("%s = %v, want %v", name, entries[0][name], value)
		}
	}
	if _, linked := entries[0]["work_item"]; linked {
		t.Errorf("work item = %v before a finding has one", entries[0]["work_item"])
	}

	if err := detector.CheckStorage(context.Background(), "local"); err != nil {
		t.Fatal(err)
	}
	if err := detector.SetWorkItem("storage:local:postgres-data:2Gi:2026-11-08", "wi-7", "drafted"); err != nil {
		t.Fatal(err)
	}
	entries, err = server.storageForecasts(store.FormatTime(now))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0]["work_item"] != "wi-7" {
		t.Errorf("entries = %v, want the link to wi-7", entries)
	}
}
