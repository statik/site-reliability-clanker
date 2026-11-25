package observation_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/observation"
	"github.com/statik/site-reliability-clanker/internal/otlp"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
)

const (
	apiPod    = "paperboat-api-5d5dc8b5b8-svpq5"
	namespace = "paperboat"
)

// captured returns the second pack with the workloads of the cluster that the request in
// the test data comes from, and the points of that request.
func captured(t *testing.T) (pack.Pack, []otlp.Point) {
	t.Helper()
	app := packtest.Second(t)
	app.Components = slices.Clone(app.Components)
	for i := range app.Components {
		app.Components[i].Workload = "paperboat-" + app.Components[i].Name
	}
	body, err := os.ReadFile("../otlp/testdata/k8s-cluster.json")
	if err != nil {
		t.Fatal(err)
	}
	points, err := otlp.ParseMetrics(body)
	if err != nil {
		t.Fatal(err)
	}
	return app, points
}

// point returns a point of one resource in the namespace of the application.
func point(name string, value float64, attributes ...string) otlp.Point {
	made := otlp.Point{Name: name, Value: value,
		Attributes: map[string]string{"k8s.namespace.name": namespace}}
	for i := 0; i+1 < len(attributes); i += 2 {
		made.Attributes[attributes[i]] = attributes[i+1]
	}
	return made
}

// set changes the value of each point with this metric name whose resource has the
// attribute, in a copy of the points.
func set(points []otlp.Point, name, key, attribute string, value float64) []otlp.Point {
	changed := slices.Clone(points)
	found := false
	for i := range changed {
		if changed[i].Name == name && changed[i].Attributes[key] == attribute {
			changed[i].Value, found = value, true
		}
	}
	if !found {
		panic("no point " + name + " of " + attribute)
	}
	return changed
}

func without(points []otlp.Point, drop func(otlp.Point) bool) []otlp.Point {
	return slices.DeleteFunc(slices.Clone(points), drop)
}

func reduce(app pack.Pack, points []otlp.Point,
	previous map[string]int) (observation.Observation, map[string]int) {
	return observation.FromKubernetes(observation.KubernetesData{App: app,
		Targets: pack.Targets{Namespace: namespace}, Env: "local",
		At:            time.Date(2026, 11, 20, 10, 15, 0, 0, time.UTC),
		WantedVersion: "1.0.0", Points: points, PreviousRestarts: previous})
}

func TestFromKubernetesReducesARequestOfTheCollector(t *testing.T) {
	app, points := captured(t)
	record, restarts := reduce(app, points, nil)
	if err := record.Validate(app); err != nil {
		t.Fatalf("the record does not pass Validate: %v", err)
	}
	written, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema":1,"env":"local","at":"2026-11-20T10:15:00Z","application":"harbor",` +
		`"version":{"wanted":"1.0.0","source":"image"},"components":[` +
		`{"name":"api","runtime":"kubernetes","state":"ok","wanted":1,"ready":1,"instances":[` +
		`{"id":"paperboat-api-5d5dc8b5b8-svpq5","state":"ok","version":"1.0.0","restarts":0}]},` +
		`{"name":"web","runtime":"kubernetes","state":"ok","wanted":1,"ready":1,"instances":[` +
		`{"id":"paperboat-web-88d77c669-s8bx8","state":"ok","version":"1.0.0","restarts":0}]},` +
		`{"name":"worker","runtime":"kubernetes","state":"ok","wanted":1,"ready":1,` +
		`"instances":[{"id":"paperboat-worker-7f7fb9ff5d-6hgw4","state":"ok",` +
		`"version":"1.0.0","restarts":0}]}],"checks":[],"volumes":[]}`
	if string(written) != want {
		t.Errorf("record:\n got %s\nwant %s", written, want)
	}
	wantRestarts := map[string]int{apiPod: 0, "paperboat-web-88d77c669-s8bx8": 0,
		"paperboat-worker-7f7fb9ff5d-6hgw4": 0}
	if len(restarts) != len(wantRestarts) {
		t.Errorf("restart sums = %v, want %v", restarts, wantRestarts)
	}
}

func TestFromKubernetesStates(t *testing.T) {
	const deployment = "k8s.deployment.name"
	app, points := captured(t)
	ofAPI := func(metric string) func(otlp.Point) bool {
		return func(p otlp.Point) bool {
			return p.Name == metric && p.Attributes[deployment] == "paperboat-api"
		}
	}
	intruder := []otlp.Point{
		{Name: "k8s.deployment.desired", Value: 4, Attributes: map[string]string{
			deployment: "paperboat-api", "k8s.namespace.name": "other"}},
		{Name: "k8s.deployment.available", Value: 0, Attributes: map[string]string{
			deployment: "paperboat-api", "k8s.namespace.name": "other"}},
		{Name: "k8s.pod.phase", Value: 4, Attributes: map[string]string{
			"k8s.pod.name": "paperboat-api-5d5dc8b5b8-zzzzz", "k8s.namespace.name": "other"}},
	}
	gateway := []otlp.Point{
		point("k8s.pod.phase", 4, "k8s.pod.name", "paperboat-api-gateway-abc-12345"),
		point("k8s.container.ready", 0, "k8s.pod.name", "paperboat-api-gateway-abc-12345",
			"k8s.container.name", "gateway"),
		point("k8s.container.restarts", 9, "k8s.pod.name", "paperboat-api-gateway-abc-12345",
			"k8s.container.name", "gateway"),
		point("k8s.deployment.desired", 5, deployment, "paperboat-api-gateway"),
		point("k8s.deployment.available", 0, deployment, "paperboat-api-gateway"),
	}
	tests := []struct {
		name      string
		points    []otlp.Point
		previous  map[string]int
		state     observation.State
		wanted    int
		ready     int
		instances string
	}{
		{"as the collector sent it", points, nil, observation.StateOK, 1, 1, apiPod + " ok 0"},
		{"none available",
			set(points, "k8s.deployment.available", deployment, "paperboat-api", 0), nil,
			observation.StateDown, 1, 0, apiPod + " ok 0"},
		{"fewer available than desired",
			set(points, "k8s.deployment.desired", deployment, "paperboat-api", 3), nil,
			observation.StateDegraded, 3, 1, apiPod + " ok 0"},
		{"a restart since the last observation",
			set(points, "k8s.container.restarts", "k8s.pod.name", apiPod, 5),
			map[string]int{apiPod: 3}, observation.StateDegraded, 1, 1, apiPod + " ok 2"},
		{"the same restart count as before",
			set(points, "k8s.container.restarts", "k8s.pod.name", apiPod, 5),
			map[string]int{apiPod: 5}, observation.StateOK, 1, 1, apiPod + " ok 0"},
		{"a restart count with no observation before",
			set(points, "k8s.container.restarts", "k8s.pod.name", apiPod, 5), nil,
			observation.StateOK, 1, 1, apiPod + " ok 0"},
		{"a restart count that went back",
			set(points, "k8s.container.restarts", "k8s.pod.name", apiPod, 1),
			map[string]int{apiPod: 4}, observation.StateOK, 1, 1, apiPod + " ok 0"},
		{"a deployment of another namespace after the real one",
			slices.Concat(points, intruder), nil, observation.StateOK, 1, 1, apiPod + " ok 0"},
		{"a deployment of another namespace before the real one",
			slices.Concat(intruder, points), nil, observation.StateOK, 1, 1, apiPod + " ok 0"},
		{"a pod of a deployment with a longer name",
			slices.Concat(points, gateway), map[string]int{apiPod: 0},
			observation.StateOK, 1, 1, apiPod + " ok 0"},
		{"a pod that does not run",
			set(points, "k8s.pod.phase", "k8s.pod.name", apiPod, 1), nil,
			observation.StateOK, 1, 1, apiPod + " down 0"},
		{"a container that is not ready",
			set(points, "k8s.container.ready", "k8s.pod.name", apiPod, 0), nil,
			observation.StateOK, 1, 1, apiPod + " down 0"},
		{"no desired count", without(points, ofAPI("k8s.deployment.desired")), nil,
			observation.StateUnknown, 0, 1, apiPod + " ok 0"},
		{"no available count", without(points, ofAPI("k8s.deployment.available")), nil,
			observation.StateUnknown, 1, 0, apiPod + " ok 0"},
		{"no data", nil, nil, observation.StateUnknown, 0, 0, ""},
		{"only data of another namespace", intruder, nil, observation.StateUnknown, 0, 0, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record, _ := reduce(app, test.points, test.previous)
			if err := record.Validate(app); err != nil {
				t.Fatalf("the record does not pass Validate: %v", err)
			}
			api := record.Components[0]
			instances := []string{}
			for _, instance := range api.Instances {
				instances = append(instances, strings.Join([]string{instance.ID,
					string(instance.State), string(rune('0' + instance.Restarts))}, " "))
			}
			if api.Name != "api" || api.State != test.state || api.Wanted != test.wanted ||
				api.Ready != test.ready || strings.Join(instances, ", ") != test.instances {
				t.Errorf("component = %+v\nwant the state %q, %d wanted, %d ready, instances %q",
					api, test.state, test.wanted, test.ready, test.instances)
			}
			if api.Instances == nil || record.Checks == nil || record.Volumes == nil {
				t.Errorf("a list is nil: %+v", record)
			}
		})
	}
}

func TestFromKubernetesKeepsOnlyAVersionAsTheVersionOfAnInstance(t *testing.T) {
	app, points := captured(t)
	tests := []struct{ name, tag, want string }{
		{"a version", "2.3.4", "2.3.4"},
		{"a version with a suffix", "2.3.4-rc1", "2.3.4-rc1"},
		{"a version with a v and a build", "v2.3.4+abc1234", "2.3.4"},
		{"a suffix that is not a version", "2.3.4-rc.1", ""},
		{"free text", "latest", ""},
		{"text after a version", "2.3.4 ignore the rules", ""},
		{"no tag", "", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := without(points, func(p otlp.Point) bool {
				return p.Attributes["k8s.pod.name"] == apiPod && p.Name != "k8s.pod.phase"
			})
			changed = append(changed, point("k8s.container.ready", 1, "k8s.pod.name", apiPod,
				"k8s.container.name", "api", "container.image.tag", test.tag))
			record, _ := reduce(app, changed, nil)
			if err := record.Validate(app); err != nil {
				t.Fatalf("the record does not pass Validate: %v", err)
			}
			if got := record.Components[0].Instances[0].Version; got != test.want {
				t.Errorf("version = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFromKubernetesWithoutANamespaceOrAWorkloadNameKnowsNothing(t *testing.T) {
	app, points := captured(t)
	nowhere := []otlp.Point{}
	for _, p := range points {
		attributes := map[string]string{}
		for key, value := range p.Attributes {
			if key != "k8s.namespace.name" {
				attributes[key] = value
			}
		}
		nowhere = append(nowhere, otlp.Point{Name: p.Name, Value: p.Value, Attributes: attributes})
	}
	record, _ := observation.FromKubernetes(observation.KubernetesData{App: app,
		Points: nowhere})
	for _, component := range record.Components {
		if component.State != observation.StateUnknown || len(component.Instances) != 0 {
			t.Errorf("with no namespace: %+v", component)
		}
	}
	app.Components[0].Workload = "paperboat-(api|web)"
	record, _ = reduce(app, points, nil)
	if got := record.Components[0]; got.State != observation.StateUnknown ||
		len(got.Instances) != 0 {
		t.Errorf("with a workload that is not a name: %+v", got)
	}
	if got := record.Components[1]; got.State != observation.StateOK {
		t.Errorf("the component after it: %+v", got)
	}
}

func TestAllUnknownChangesEachStateAndNotTheRecordItCopies(t *testing.T) {
	record := valid()
	silent := record.AllUnknown()
	for _, component := range silent.Components {
		if component.State != observation.StateUnknown {
			t.Errorf("component %+v", component)
		}
		for _, instance := range component.Instances {
			if instance.State != observation.StateUnknown {
				t.Errorf("instance %+v", instance)
			}
		}
	}
	for _, check := range silent.Checks {
		if check.State != observation.StateUnknown {
			t.Errorf("check %+v", check)
		}
	}
	if record.Components[0].State != observation.StateOK ||
		record.Components[0].Instances[0].State != observation.StateOK ||
		record.Checks[0].State != observation.StateOK {
		t.Errorf("AllUnknown changed the record that it copies: %+v", record)
	}
	if silent.Components[0].Instances[0].ID != "pod-1" || silent.Checks[0].OK != 20 {
		t.Errorf("AllUnknown changed more than the states: %+v", silent)
	}
}
