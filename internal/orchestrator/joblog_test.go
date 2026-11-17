package orchestrator

import (
	"fmt"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

func TestJobLogIsReadableWhileTheJobRuns(t *testing.T) {
	db := storetest.Open(t)
	o := &Orchestrator{DB: db}
	var id int64
	if err := db.QueryRow(`INSERT INTO jobs (work_item, kind, status, started_at)
		VALUES ('WI-0001', 'plan', 'running', 'now') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	log := &jobLog{db: db, job: id}
	fmt.Fprint(log, "first line\n")
	if got, err := o.JobLog(id); err != nil || got != "first line\n" {
		t.Fatalf("log while the job runs = %q, %v", got, err)
	}
	// Writes that come quickly after a store wait for the next store or for the end.
	fmt.Fprint(log, "second line\n")
	fmt.Fprint(log, "bad bytes \xff and \x00 zero\n")
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	want := "first line\nsecond line\nbad bytes ? and  zero\n"
	if got, _ := o.JobLog(id); got != want {
		t.Fatalf("log after the end = %q, want %q", got, want)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("a second close: %v", err)
	}
	if _, err := o.JobLog(id + 100); err == nil {
		t.Fatal("no error for a job that does not exist")
	}
}

func TestLayerOutputsAreStoredForEachLayer(t *testing.T) {
	db := storetest.Open(t)
	empty, err := store.LayerOutputs(db, "local", "50-app")
	if err != nil || len(empty) != 0 {
		t.Fatalf("a layer that was never applied = %v, %v", empty, err)
	}
	o := &Orchestrator{DB: db}
	if o.layerApplied("local", "50-app") {
		t.Fatal("a layer with no outputs counts as applied")
	}
	first := map[string]any{"app_url": "http://localhost:8088", "replicas": float64(1)}
	if err := store.SetLayerOutputs(db, "local", "50-app", first, "t1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetLayerOutputs(db, "local", "50-app", map[string]any{"app_url": "http://new"}, "t2"); err != nil {
		t.Fatal(err)
	}
	got, err := store.LayerOutputs(db, "local", "50-app")
	if err != nil || len(got) != 1 || got["app_url"] != "http://new" {
		t.Fatalf("outputs = %v, %v; want only the newest", got, err)
	}
	if !o.layerApplied("local", "50-app") || o.layerApplied("other", "50-app") {
		t.Fatal("layerApplied does not follow the stored outputs")
	}
	if !o.canPlan("local", "00-bootstrap") || o.canPlan("local", "40-platform") {
		t.Fatal("canPlan must need the layer below")
	}
}
