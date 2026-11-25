package observation

// KubernetesWorkload is what a collector reports for one workload on Kubernetes.
type KubernetesWorkload struct {
	// Wanted is the number of instances that the specification asks for.
	Wanted int
	// Ready is the number of instances that are ready.
	Ready int
	// Updated is the number of instances that have the new specification.
	Updated int
	// Known is false when no data arrived in time, or when the cluster did not yet read
	// the last change of the workload.
	Known bool
	// RestartsSinceLast is the number of container starts since the last observation.
	RestartsSinceLast int
}

// KubernetesState maps the report of one workload to a state, as the column `kubernetes`
// of the table "The states" in section B.4 of docs/PACKS.md says.
//
// The rules apply in this order:
//
//   - StateUnknown when the workload is not known.
//   - StateUnknown when no instance is wanted. The table has no row for a workload that
//     is scaled to zero. The core cannot tell that such a workload works, so it does not
//     report StateOK, and no instance failed, so it does not report StateDown.
//   - StateDown when instances are wanted and none is ready.
//   - StateDegraded when not all wanted instances are ready, or not all have the new
//     specification, or a container started again since the last observation.
//   - StateOK when each wanted instance is ready and has the new specification.
func KubernetesState(w KubernetesWorkload) State {
	if !w.Known {
		return StateUnknown
	}
	if w.Wanted <= 0 {
		return StateUnknown
	}
	if w.Ready <= 0 {
		return StateDown
	}
	if w.Ready < w.Wanted || w.Updated < w.Wanted || w.RestartsSinceLast > 0 {
		return StateDegraded
	}
	return StateOK
}

// ComponentState derives the state of a component from the states of its instances, for
// a runtime on hosts where each host is one instance.
//
// The rules apply in this order:
//
//   - StateUnknown when there is no instance.
//   - StateUnknown when the state of one instance or more is StateUnknown, or is not one
//     of the four states. A host without data can hide a failure, so the known hosts do
//     not decide for it.
//   - StateOK when each instance is StateOK.
//   - StateDown when each instance is StateDown.
//   - StateDegraded in each other case.
func ComponentState(instances []Instance) State {
	if len(instances) == 0 {
		return StateUnknown
	}
	ok, down := 0, 0
	for _, instance := range instances {
		switch instance.State {
		case StateOK:
			ok++
		case StateDown:
			down++
		case StateDegraded:
		default:
			return StateUnknown
		}
	}
	if ok == len(instances) {
		return StateOK
	}
	if down == len(instances) {
		return StateDown
	}
	return StateDegraded
}
