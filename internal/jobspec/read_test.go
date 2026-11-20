package jobspec

import (
	"reflect"
	"strings"
	"testing"
)

func TestEachReadIsOneCommandThatReads(t *testing.T) {
	cases := []struct {
		read Read
		want []string
	}{
		{Read{Read: ReadPods}, []string{"get", "-n", "shop", "pods", "-o", "wide"}},
		{Read{Read: ReadWorkloads}, []string{"get", "-n", "shop", "deployments"}},
		{Read{Read: ReadLogs, Workload: "shop-api", Lines: 30},
			[]string{"logs", "-n", "shop", "deployment/shop-api", "--tail=30"}},
		{Read{Read: ReadEvents}, []string{"get", "-n", "shop", "events",
			"--sort-by=.lastTimestamp"}},
	}
	for _, tc := range cases {
		got, err := tc.read.KubectlArgs("shop")
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%+v: %v, %v; want %v", tc.read, got, err, tc.want)
		}
	}
}

func TestAReadCannotBecomeAnotherCommand(t *testing.T) {
	cases := map[string]struct {
		namespace string
		read      Read
		want      string
	}{
		"a read that does not exist": {"shop", Read{Read: "delete"}, "does not exist"},
		"no read":                    {"shop", Read{}, "does not exist"},
		"a namespace that is a flag": {"-A", Read{Read: ReadPods}, "namespace"},
		"no namespace":               {"", Read{Read: ReadPods}, "namespace"},
		"a workload that is a flag": {"shop", Read{Read: ReadLogs, Workload: "--all-namespaces",
			Lines: 1}, "workload"},
		"a workload with a path": {"shop", Read{Read: ReadLogs, Workload: "x/../secret",
			Lines: 1}, "workload"},
		"logs with no workload": {"shop", Read{Read: ReadLogs, Lines: 1}, "workload"},
		"logs with no lines":    {"shop", Read{Read: ReadLogs, Workload: "shop-api"}, "0 lines"},
		"logs with too many lines": {"shop", Read{Read: ReadLogs, Workload: "shop-api",
			Lines: MaxReadLines + 1}, "201 lines"},
		"pods of one workload": {"shop", Read{Read: ReadPods, Workload: "secrets"},
			"takes no workload"},
		"events with lines": {"shop", Read{Read: ReadEvents, Lines: 5}, "takes no workload"},
	}
	for name, tc := range cases {
		args, err := tc.read.KubectlArgs(tc.namespace)
		if err == nil || !strings.Contains(err.Error(), tc.want) || args != nil {
			t.Errorf("%s: %v, error %v; want an error with %q", name, args, err, tc.want)
		}
	}
}
