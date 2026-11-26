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
)

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
		if key != targetApplication && key != targetData {
			return Targets{}, fmt.Errorf("the output %s has the key %q; it can have %s and %s",
				TargetsOutput, key, targetApplication, targetData)
		}
		name, _ := values[key].(string)
		if key == targetData && name == "" {
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
	if found[targetApplication] == found[targetData] {
		return Targets{}, fmt.Errorf("the output %s has the namespace %q two times",
			TargetsOutput, found[targetData])
	}
	return Targets{Namespace: found[targetApplication], DataNamespace: found[targetData]}, nil
}

func checkNamespace(key, name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("the namespace %q of %s in the output %s must match %s", name, key,
			TargetsOutput, namePattern)
	}
	if key == targetApplication && reservedName(name) {
		return fmt.Errorf("the namespace %q of %s in the output %s belongs to the cluster, "+
			"to the control plane or to the platform; it must be the name of the pack",
			name, key, TargetsOutput)
	}
	if slices.Contains(reservedNamespaces, name) || strings.HasPrefix(name, clusterPrefix) {
		return fmt.Errorf("the namespace %q of %s in the output %s belongs to the cluster or "+
			"to the control plane", name, key, TargetsOutput)
	}
	return nil
}
