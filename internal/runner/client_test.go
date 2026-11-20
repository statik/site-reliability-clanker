package runner

import (
	"errors"
	"reflect"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/jobspec"
)

// A diagnose job carries reads. The runner makes the command of each read and starts no
// program for a read that is not valid.
func TestADiagnoseJobRunsOnlyTheCommandsOfItsReads(t *testing.T) {
	started := [][]string{}
	kubectl := func(args ...string) ([]byte, error) {
		started = append(started, args)
		if args[0] == "logs" {
			return []byte("partial"), errors.New("exit status 1")
		}
		return []byte("ok"), nil
	}
	spec := jobspec.Spec{Kind: jobspec.KindDiagnose, Namespace: "shop", Reads: []jobspec.Read{
		{Read: jobspec.ReadPods},
		{Read: "delete"},
		{Read: jobspec.ReadLogs, Workload: "--all-namespaces", Lines: 5},
		{Read: jobspec.ReadLogs, Workload: "shop-api", Lines: 5},
	}}
	got := diagnose(spec, kubectl)
	wantStarted := [][]string{{"get", "-n", "shop", "pods", "-o", "wide"},
		{"logs", "-n", "shop", "deployment/shop-api", "--tail=5"}}
	if !reflect.DeepEqual(started, wantStarted) {
		t.Errorf("started %v, want %v", started, wantStarted)
	}
	if len(got) != 4 || got[0].Output != "ok" || got[0].Failure != "" {
		t.Fatalf("results %+v", got)
	}
	if got[1].Failure == "" || got[2].Failure == "" || got[1].Output != "" {
		t.Errorf("a read that is not valid must be a failure with no output: %+v", got[1:3])
	}
	if got[3].Output != "partial" || got[3].Failure != "exit status 1" {
		t.Errorf("a command that fails keeps its output: %+v", got[3])
	}
}

func TestADiagnoseJobWithANamespaceThatIsAFlagStartsNoProgram(t *testing.T) {
	spec := jobspec.Spec{Namespace: "-A", Reads: []jobspec.Read{{Read: jobspec.ReadPods}}}
	got := diagnose(spec, func(...string) ([]byte, error) {
		t.Error("a program started")
		return nil, nil
	})
	if len(got) != 1 || got[0].Failure == "" {
		t.Errorf("results %+v", got)
	}
}
