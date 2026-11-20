package observation

import (
	"regexp"
	"slices"
	"time"

	"github.com/statik/site-reliability-clanker/internal/otlp"
	"github.com/statik/site-reliability-clanker/internal/pack"
)

// SourceImage is the version source of a record whose versions are the tags of the
// container images.
const SourceImage = "image"

// The metrics of the cluster receiver of the collector that the reducer reads, and the
// attributes of their resources.
const (
	metricDesired   = "k8s.deployment.desired"
	metricAvailable = "k8s.deployment.available"
	metricPhase     = "k8s.pod.phase"
	metricReady     = "k8s.container.ready"
	metricRestarts  = "k8s.container.restarts"

	attributeNamespace  = "k8s.namespace.name"
	attributeDeployment = "k8s.deployment.name"
	attributePod        = "k8s.pod.name"
	attributeContainer  = "k8s.container.name"
	attributeImageTag   = "container.image.tag"

	phaseRunning = 2
)

var workloadPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// KubernetesData is what the reducer needs to make the record of one environment.
type KubernetesData struct {
	// App is the pack of the application.
	App pack.Pack
	// Targets says in which namespace the components run. The reducer drops each point of
	// another namespace.
	Targets pack.Targets
	Env     string
	// At is the time of the record on the clock of the core.
	At time.Time
	// WantedVersion is the version that the layer of the application declares, or "".
	WantedVersion string
	// Points are the points of one request of the collector.
	Points []otlp.Point
	// PreviousRestarts is the second result of the reduction before this one. Nil means
	// that there is none.
	PreviousRestarts map[string]int
}

type deployment struct {
	desired, available       int
	hasDesired, hasAvailable bool
}

type container struct {
	ready    bool
	restarts int
	tag      string
}

type pod struct {
	running    bool
	containers map[string]*container
}

type cluster struct {
	deployments map[string]*deployment
	pods        map[string]*pod
}

// FromKubernetes reduces the points of a collector on Kubernetes to the record. It reads
// only the deployments and the pods of the components of the pack in the namespace of the
// targets. A component with no data has the state StateUnknown.
//
// Args:
//
//	data: the pack, the targets, the points and the restart counts of the last reduction.
//
// Returns:
//
//	The record, and for each instance in it the sum of the restart counts of its
//	containers as the collector sent them. The next reduction takes the difference.
func FromKubernetes(data KubernetesData) (Observation, map[string]int) {
	seen := readCluster(data.Points, data.Targets.Namespace)
	record := Observation{Schema: Schema, Env: data.Env, At: data.At,
		Application: data.App.Name,
		Version:     Version{Wanted: data.WantedVersion, Source: SourceImage},
		Components:  []Component{}, Checks: []Check{}, Volumes: []Volume{}}
	restarts := map[string]int{}
	for _, part := range data.App.Components {
		component := Component{Name: part.Name, Runtime: part.Runtime, State: StateUnknown,
			Instances: []Instance{}}
		if !workloadPattern.MatchString(part.Workload) {
			record.Components = append(record.Components, component)
			continue
		}
		since := 0
		for _, name := range podsOf(seen.pods, part.Workload) {
			instance, sum := instanceOf(name, seen.pods[name], data.PreviousRestarts)
			restarts[name] = sum
			since += instance.Restarts
			component.Instances = append(component.Instances, instance)
		}
		workload := KubernetesWorkload{RestartsSinceLast: since}
		if found, ok := seen.deployments[part.Workload]; ok {
			// The collector has no metric for the instances of a deployment that have
			// the new specification. The reducer takes each wanted instance as updated,
			// so a rollout in progress shows only through the count of ready instances.
			workload.Wanted, workload.Updated = found.desired, found.desired
			workload.Ready = found.available
			workload.Known = found.hasDesired && found.hasAvailable
		}
		component.Wanted, component.Ready = workload.Wanted, workload.Ready
		component.State = KubernetesState(workload)
		record.Components = append(record.Components, component)
	}
	return record, restarts
}

// readCluster sorts the points of one namespace by deployment and by pod.
func readCluster(points []otlp.Point, namespace string) cluster {
	seen := cluster{deployments: map[string]*deployment{}, pods: map[string]*pod{}}
	if namespace == "" {
		return seen
	}
	for _, point := range points {
		if point.Attributes[attributeNamespace] != namespace {
			continue
		}
		switch point.Name {
		case metricDesired, metricAvailable:
			seen.addDeployment(point)
		case metricPhase, metricReady, metricRestarts:
			seen.addPod(point)
		}
	}
	return seen
}

func (c cluster) addDeployment(point otlp.Point) {
	name := point.Attributes[attributeDeployment]
	if name == "" {
		return
	}
	found, ok := c.deployments[name]
	if !ok {
		found = &deployment{}
		c.deployments[name] = found
	}
	if point.Name == metricDesired {
		found.desired, found.hasDesired = int(point.Value), true
		return
	}
	found.available, found.hasAvailable = int(point.Value), true
}

func (c cluster) addPod(point otlp.Point) {
	name := point.Attributes[attributePod]
	if name == "" {
		return
	}
	found, ok := c.pods[name]
	if !ok {
		found = &pod{containers: map[string]*container{}}
		c.pods[name] = found
	}
	if point.Name == metricPhase {
		found.running = int(point.Value) == phaseRunning
		return
	}
	part, ok := found.containers[point.Attributes[attributeContainer]]
	if !ok {
		part = &container{tag: point.Attributes[attributeImageTag]}
		found.containers[point.Attributes[attributeContainer]] = part
	}
	if point.Name == metricReady {
		part.ready = int(point.Value) == 1
		return
	}
	part.restarts = max(int(point.Value), 0)
}

// podsOf returns the sorted names of the pods of a deployment: the name of the
// deployment, the hash of its replica set and five characters. The pod of a deployment
// with a longer name has one part more and does not match.
func podsOf(pods map[string]*pod, workload string) []string {
	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(workload) + `-[a-z0-9]+-[a-z0-9]{5}$`)
	names := []string{}
	for name := range pods {
		if pattern.MatchString(name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// instanceOf returns the instance of one pod and the sum of the restart counts of its
// containers. The instance is StateOK when the pod runs and each container is ready.
func instanceOf(name string, found *pod, previous map[string]int) (Instance, int) {
	instance := Instance{ID: name, State: StateOK}
	if !found.running {
		instance.State = StateDown
	}
	sum := 0
	tags := []string{}
	for _, part := range found.containers {
		sum += part.restarts
		if !part.ready {
			instance.State = StateDown
		}
		if !slices.Contains(tags, part.tag) {
			tags = append(tags, part.tag)
		}
	}
	instance.Version = versionOf(tags)
	if before, ok := previous[name]; ok && sum > before {
		instance.Restarts = sum - before
	}
	return instance, sum
}

// versionOf returns the one image tag of the containers of an instance when it has the
// form of a version. A tag is text of the cluster, so no other tag enters the record.
func versionOf(tags []string) string {
	if len(tags) == 1 && IsVersion(tags[0]) {
		return tags[0]
	}
	return ""
}

// AllUnknown returns a copy of the record in which the state of each component, each
// instance and each check is StateUnknown. It is the record of an environment whose
// collector is silent: the last data says nothing about now.
func (o Observation) AllUnknown() Observation {
	o.Components = slices.Clone(o.Components)
	for i := range o.Components {
		o.Components[i].State = StateUnknown
		o.Components[i].Instances = slices.Clone(o.Components[i].Instances)
		for j := range o.Components[i].Instances {
			o.Components[i].Instances[j].State = StateUnknown
		}
	}
	o.Checks = slices.Clone(o.Checks)
	for i := range o.Checks {
		o.Checks[i].State = StateUnknown
	}
	return o
}
