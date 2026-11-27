package pack

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// The targets of an environment are an output of the layer of the platform. The pack does
// not say where its components run, and the layer that says it is not a layer of the pack.
const (
	TargetsLayer  = "40-platform"
	TargetsOutput = "targets"
)

// Keys of the output.
const (
	targetApplication = "application"
	targetData        = "data"
	targetWorkloads   = "workloads"
)

// targetKeys are the keys that the output can have.
var targetKeys = []string{targetApplication, targetData, targetWorkloads}

// reservedNamespaces belong to the cluster or to the control plane, as each namespace
// with the prefix "kube-" does. No application and no data service runs there, so no
// debug session reads there and no identity gets rights there. The namespace of the
// application has the name of its pack, so it follows the rule of the names of packs,
// which also reserves the prefix "clanker-" for the platform. The data services can be
// in a namespace of the platform, for example clanker-data on the local target.
var reservedNamespaces = []string{"default", "clanker-system"}

const clusterPrefix = "kube-"

// Targets says where the components of the application run on one environment.
type Targets struct {
	// Namespace is the Kubernetes namespace of the components. A debug session reads only
	// there.
	Namespace string
	// DataNamespace is the namespace of the data services of the application. It is empty
	// on a target whose data services are not in the cluster.
	DataNamespace string
	// WorkloadsNamespace is the namespace of the pods that the application starts, or empty
	// when the pack asks for none. Only the applier of the application layer gets rights
	// there: no runbook and no debug session reads or changes it.
	WorkloadsNamespace string
}

// ParseTargets reads the targets from the outputs of the layer of the platform. The
// outputs come from an apply that a person approved, and the code of a layer can change,
// so each value is checked: a namespace must be a name, and it cannot be a namespace of
// the cluster or of the control plane.
func ParseTargets(outputs map[string]any) (Targets, error) {
	raw, ok := outputs[TargetsOutput]
	if !ok {
		return Targets{}, fmt.Errorf("the layer %s has no output %s: the layer is not "+
			"applied, or the environment is from before this output and must start again",
			TargetsLayer, TargetsOutput)
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return Targets{}, fmt.Errorf("the output %s of the layer %s is not a map of names",
			TargetsOutput, TargetsLayer)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	found := map[string]string{}
	for _, key := range keys {
		if !slices.Contains(targetKeys, key) {
			return Targets{}, fmt.Errorf("the output %s has the key %q; it can have %s",
				TargetsOutput, key, strings.Join(targetKeys, ", "))
		}
		name, _ := values[key].(string)
		if key != targetApplication && name == "" {
			continue
		}
		if err := checkNamespace(key, name); err != nil {
			return Targets{}, err
		}
		found[key] = name
	}
	if found[targetApplication] == "" {
		return Targets{}, fmt.Errorf("the output %s has no namespace for %s", TargetsOutput,
			targetApplication)
	}
	if err := checkDistinct(found); err != nil {
		return Targets{}, err
	}
	return Targets{Namespace: found[targetApplication], DataNamespace: found[targetData],
		WorkloadsNamespace: found[targetWorkloads]}, nil
}

// checkDistinct refuses a namespace that the output names for two keys.
func checkDistinct(found map[string]string) error {
	seen := map[string]bool{}
	for _, key := range targetKeys {
		name := found[key]
		if name != "" && seen[name] {
			return fmt.Errorf("the output %s has the namespace %q two times", TargetsOutput,
				name)
		}
		seen[name] = true
	}
	return nil
}

func checkNamespace(key, name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("the namespace %q of %s in the output %s must match %s", name, key,
			TargetsOutput, namePattern)
	}
	// The data services can be in a namespace of the platform. The application and the
	// pods that it starts cannot.
	if key != targetData && reservedName(name) {
		return fmt.Errorf("the namespace %q of %s in the output %s belongs to the cluster, "+
			"to the control plane or to the platform; it must have the name of the pack",
			name, key, TargetsOutput)
	}
	if slices.Contains(reservedNamespaces, name) || strings.HasPrefix(name, clusterPrefix) {
		return fmt.Errorf("the namespace %q of %s in the output %s belongs to the cluster or "+
			"to the control plane", name, key, TargetsOutput)
	}
	return nil
}
