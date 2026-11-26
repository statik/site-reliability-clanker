package pack_test

import (
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/pack"
)

func targets(values map[string]any) map[string]any {
	return map[string]any{pack.TargetsOutput: values}
}

func TestTheTargetsComeFromTheOutputOfThePlatformLayer(t *testing.T) {
	got, err := pack.ParseTargets(targets(map[string]any{"application": "shop",
		"data": "shop-data"}))
	if err != nil || got != (pack.Targets{Namespace: "shop", DataNamespace: "shop-data"}) {
		t.Errorf("ParseTargets = %+v, %v", got, err)
	}
	got, err = pack.ParseTargets(targets(map[string]any{"application": "paperboat",
		"data": "clanker-data"}))
	if err != nil || got != (pack.Targets{Namespace: "paperboat", DataNamespace: "clanker-data"}) {
		t.Errorf("the data namespace of the local target: ParseTargets = %+v, %v", got, err)
	}
	got, err = pack.ParseTargets(targets(map[string]any{"application": "coder",
		"data": "clanker-data", "workloads": "coder-workloads"}))
	if err != nil || got != (pack.Targets{Namespace: "coder", DataNamespace: "clanker-data",
		WorkloadsNamespace: "coder-workloads"}) {
		t.Errorf("the workloads namespace: ParseTargets = %+v, %v", got, err)
	}
	for name, values := range map[string]map[string]any{
		"no key for the data":          {"application": "shop"},
		"an empty data namespace":      {"application": "shop", "data": ""},
		"an empty workloads namespace": {"application": "shop", "workloads": ""},
	} {
		got, err := pack.ParseTargets(targets(values))
		if err != nil || got != (pack.Targets{Namespace: "shop"}) {
			t.Errorf("%s: ParseTargets = %+v, %v", name, got, err)
		}
	}
}

func TestTheTargetsAreChecked(t *testing.T) {
	cases := map[string]struct {
		outputs map[string]any
		want    string
	}{
		"a layer that is not applied": {map[string]any{}, "has no output targets"},
		"an output that is text":      {map[string]any{"targets": "shop"}, "not a map"},
		"no application":              {targets(map[string]any{"data": "shop-data"}), "no namespace for application"},
		"an empty application":        {targets(map[string]any{"application": ""}), `namespace ""`},
		"a namespace that is a number": {targets(map[string]any{"application": 7}),
			`namespace ""`},
		"a namespace that is a flag": {targets(map[string]any{"application": "-A"}),
			`namespace "-A"`},
		"the namespace of the cluster": {targets(map[string]any{"application": "kube-system"}),
			`"kube-system" of application in the output targets belongs`},
		"the default namespace": {targets(map[string]any{"application": "default"}),
			"belongs to the cluster"},
		"another namespace of the cluster": {targets(map[string]any{
			"application": "kube-flannel"}), `"kube-flannel" of application`},
		"the namespace of the control plane as data": {targets(map[string]any{
			"application": "shop", "data": "clanker-system"}), `"clanker-system" of data`},
		"a namespace of the platform as application": {targets(map[string]any{
			"application": "clanker-data"}), `"clanker-data" of application`},
		"the namespace of the control plane as application": {targets(map[string]any{
			"application": "clanker-system"}), "the name of the pack"},
		"the administrator of PostgreSQL as application": {targets(map[string]any{
			"application": "postgres"}), `"postgres" of application`},
		"a data namespace that is a flag": {targets(map[string]any{"application": "shop",
			"data": "--all"}), `namespace "--all" of data`},
		"one namespace for both": {targets(map[string]any{"application": "shop",
			"data": "shop"}), "two times"},
		"a key that does not exist": {targets(map[string]any{"application": "shop",
			"cluster": "x"}), `the key "cluster"`},
		"a workloads namespace of the platform": {targets(map[string]any{
			"application": "shop", "workloads": "clanker-workloads"}),
			`"clanker-workloads" of workloads`},
		"the namespace of the control plane as workloads": {targets(map[string]any{
			"application": "shop", "workloads": "clanker-system"}), `"clanker-system" of workloads`},
		"a namespace of the cluster as workloads": {targets(map[string]any{
			"application": "shop", "workloads": "kube-public"}), `"kube-public" of workloads`},
		"a workloads namespace that is a flag": {targets(map[string]any{
			"application": "shop", "workloads": "-A"}), `namespace "-A" of workloads`},
		"the application namespace as workloads": {targets(map[string]any{
			"application": "shop", "workloads": "shop"}), "two times"},
		"the data namespace as workloads": {targets(map[string]any{"application": "shop",
			"data": "shop-data", "workloads": "shop-data"}), "two times"},
	}
	for name, tc := range cases {
		got, err := pack.ParseTargets(tc.outputs)
		if err == nil || !strings.Contains(err.Error(), tc.want) || got != (pack.Targets{}) {
			t.Errorf("%s: %+v, error %v; want an error with %q", name, got, err, tc.want)
		}
	}
}
