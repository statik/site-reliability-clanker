package orchestrator

import (
	"reflect"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/jobspec"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

func TestLastPartKeepsTheNewestLines(t *testing.T) {
	short := "one\ntwo"
	if got := lastPart(short); got != short {
		t.Errorf("short output must stay unchanged, got %q", got)
	}
	var long strings.Builder
	for range 400 {
		long.WriteString("old line of log output\n")
	}
	long.WriteString("NEWEST ERROR status 500")
	got := lastPart(long.String())
	if len(got) > maxCommandOutput+40 {
		t.Errorf("output has %d characters, over the limit", len(got))
	}
	if !strings.HasSuffix(got, "NEWEST ERROR status 500") {
		t.Error("the newest line must be kept")
	}
	if !strings.HasPrefix(got, "(earlier output left out)\nold line") {
		t.Errorf("want a note and then a whole line, got %q", got[:60])
	}
}

// The pack gives two reads: the logs of one component and the events. The core puts the
// state of the pods and of the deployments before them. The record of the session has the
// command of each read, in the namespace of the pack.
func TestADebugSessionReadsTheStateAndThenWhatThePackNames(t *testing.T) {
	reads := debugReads(packtest.Second(t))
	want := []jobspec.Read{{Read: jobspec.ReadPods}, {Read: jobspec.ReadWorkloads},
		{Read: jobspec.ReadLogs, Workload: "harbor-gateway", Lines: 30},
		{Read: jobspec.ReadEvents}}
	if !reflect.DeepEqual(reads, want) {
		t.Fatalf("debugReads = %+v\nwant %+v", reads, want)
	}
	results := []jobspec.CommandOutput{{Output: "pod-1 Running\n"}, {Output: "gateway 1/1"},
		{Output: "error: token=abcdef0123456789abcdef0123456789", Failure: "exit status 1"},
		{Output: ""}}
	var record strings.Builder
	if err := writeTranscript(&record, "harbor", reads, results); err != nil {
		t.Fatal(err)
	}
	got := record.String()
	for _, line := range []string{
		"$ kubectl get -n harbor pods -o wide\npod-1 Running\n",
		"$ kubectl get -n harbor deployments\ngateway 1/1\n",
		"$ kubectl logs -n harbor deployment/harbor-gateway --tail=30\n",
		"(command failed: exit status 1)\n",
		"$ kubectl get -n harbor events --sort-by=.lastTimestamp\n",
	} {
		if !strings.Contains(got, line) {
			t.Errorf("the record does not have %q:\n%s", line, got)
		}
	}
	if strings.Contains(got, "abcdef0123456789") {
		t.Errorf("the record has a secret:\n%s", got)
	}
}

func TestTheRecordNeedsOneAnswerForEachRead(t *testing.T) {
	reads := debugReads(packtest.Second(t))
	var record strings.Builder
	err := writeTranscript(&record, "harbor", reads, []jobspec.CommandOutput{{Output: "x"}})
	if err == nil || !strings.Contains(err.Error(), "1 of 4") || record.Len() != 0 {
		t.Errorf("error %v, record %q", err, record.String())
	}
}

// The namespace of a debug session comes from the output of the layer of the platform, as
// an approved apply wrote it. With no such output there is no session.
func TestTheTargetsOfAnEnvironmentComeFromThePlatformLayer(t *testing.T) {
	o := &Orchestrator{DB: storetest.Open(t)}
	if _, err := o.Targets("local"); err == nil || !strings.Contains(err.Error(),
		"environment local") {
		t.Errorf("an environment with no platform layer: %v", err)
	}
	set := func(targets any) {
		t.Helper()
		err := store.SetLayerOutputs(o.DB, "local", "40-platform",
			map[string]any{"targets": targets}, "2026-11-20T12:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
	}
	set(map[string]any{"application": "harbor", "data": "harbor-store"})
	got, err := o.Targets("local")
	if err != nil || got.Namespace != "harbor" || got.DataNamespace != "harbor-store" {
		t.Errorf("Targets = %+v, %v", got, err)
	}
	set(map[string]any{"application": "kube-system"})
	if _, err := o.Targets("local"); err == nil {
		t.Error("the namespace of the cluster is a target")
	}
}
