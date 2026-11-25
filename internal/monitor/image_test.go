package monitor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/observation"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/otlp"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
)

const appNamespace = "harbor"

// imageDeployed returns a monitor of the second pack with the version source image on its
// component api, its application at a test server where each check passes, and a short
// wait in Verify. The version path of the server answers another version, so a test fails
// when the monitor reads it.
func imageDeployed(t *testing.T) *Monitor {
	t.Helper()
	app := packtest.Second(t)
	app.AppVersion = pack.AppVersion{Source: pack.VersionImage, Component: "api"}
	m, served := deployed(t, app, map[string]int{"/api/healthz": 200, "/v2/berths": 200})
	served.version = "9.9.9"
	m.verifyWait, m.verifyPause = 300*time.Millisecond, 10*time.Millisecond
	return m
}

// gatewayPod is one pod of the deployment of the component api. ready is the value of
// the metric of its container, and tag is the tag of its image.
type gatewayPod struct {
	name  string
	ready float64
	tag   string
}

// gatewayPoints returns the points of a request of the collector with the pods of the
// component api.
func gatewayPoints(pods ...gatewayPod) []otlp.Point {
	inNamespace := func(attributes ...string) map[string]string {
		made := map[string]string{"k8s.namespace.name": appNamespace}
		for i := 0; i+1 < len(attributes); i += 2 {
			made[attributes[i]] = attributes[i+1]
		}
		return made
	}
	deployment := inNamespace("k8s.deployment.name", "harbor-gateway")
	points := []otlp.Point{
		{Name: "k8s.deployment.desired", Value: float64(len(pods)), Attributes: deployment},
		{Name: "k8s.deployment.available", Value: float64(len(pods)), Attributes: deployment},
	}
	for _, pod := range pods {
		points = append(points,
			otlp.Point{Name: "k8s.pod.phase", Value: 2,
				Attributes: inNamespace("k8s.pod.name", pod.name)},
			otlp.Point{Name: "k8s.container.ready", Value: pod.ready,
				Attributes: inNamespace("k8s.pod.name", pod.name,
					"k8s.container.name", "gateway", "container.image.tag", pod.tag)})
	}
	return points
}

// observe stores the observation of the pods of the component api, received at the
// wall-clock time at.
func observe(t *testing.T, m *Monitor, at time.Time, pods ...gatewayPod) {
	t.Helper()
	before := m.Wall
	m.Wall = func() time.Time { return at }
	defer func() { m.Wall = before }()
	_, err := m.RecordObservation("local", pack.Targets{Namespace: appNamespace},
		gatewayPoints(pods...))
	if err != nil {
		t.Fatal(err)
	}
}

func readyPod(name, tag string) gatewayPod { return gatewayPod{name: name, ready: 1, tag: tag} }

const (
	firstPod  = "harbor-gateway-5d5dc8b5b8-svpq5"
	secondPod = "harbor-gateway-5d5dc8b5b8-k2x7z"
)

func TestVerifyWithTheImageSourceReadsOnlyAnObservationAfterTheApply(t *testing.T) {
	applied := time.Date(2026, 11, 25, 18, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		receive time.Duration
		pods    []gatewayPod
		reason  string
	}{
		{"each instance runs the wanted version", time.Second,
			[]gatewayPod{readyPod(firstPod, "v3.4.0+abc"), readyPod(secondPod, "3.4.0")}, ""},
		{"the same record before the apply", -time.Second,
			[]gatewayPod{readyPod(firstPod, "3.4.0"), readyPod(secondPod, "3.4.0")},
			"no data from the collector since the apply"},
		{"the same record at the end of the apply", 0,
			[]gatewayPod{readyPod(firstPod, "3.4.0")},
			"no data from the collector since the apply"},
		{"an instance at another version", time.Second,
			[]gatewayPod{readyPod(firstPod, "3.4.0"), readyPod(secondPod, "3.3.0")},
			"instance " + secondPod + " of api runs version 3.3.0"},
		{"an instance with a tag that is not a version", time.Second,
			[]gatewayPod{readyPod(firstPod, "latest")},
			"instance " + firstPod + " of api runs version unknown"},
		{"an instance that is not ready", time.Second,
			[]gatewayPod{readyPod(firstPod, "3.4.0"), {name: secondPod, ready: 0, tag: "3.4.0"}},
			"instance " + secondPod + " of api is down"},
		{"no instance", time.Second, nil, "the collector reports no instance of api"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := imageDeployed(t)
			observe(t, m, applied.Add(test.receive), test.pods...)
			m.Wall = func() time.Time { return applied }
			result, err := m.Verify(context.Background(), orchestrator.WorkItem{Env: "local"})
			if err != nil {
				t.Fatal(err)
			}
			if test.reason == "" {
				if !result.OK || !strings.Contains(strings.Join(result.Checks, "; "),
					"running version is 3.4.0") {
					t.Errorf("Verify: %+v", result)
				}
				return
			}
			if result.OK || !strings.Contains(result.Reason, test.reason) {
				t.Errorf("Verify: %+v, want a reason with %q", result, test.reason)
			}
		})
	}
}

func TestVerifyWithTheImageSourceAndNoObservationFails(t *testing.T) {
	m := imageDeployed(t)
	result, err := m.Verify(context.Background(), orchestrator.WorkItem{Env: "local"})
	want := "after 0 s the application does not report healthy version 3.4.0 (each check " +
		"passes, no data from the collector)"
	if err != nil || result.OK || result.Reason != want {
		t.Errorf("Verify: %+v, %v; want the reason %q", result, err, want)
	}
}

func TestVerifyWithTheImageSourceWaitsForAnObservationAfterTheApply(t *testing.T) {
	m := imageDeployed(t)
	m.verifyWait = 5 * time.Second
	observe(t, m, time.Now().Add(-time.Minute), readyPod(firstPod, "3.4.0"))
	recorded := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, err := m.RecordObservation("local", pack.Targets{Namespace: appNamespace},
			gatewayPoints(readyPod(firstPod, "3.4.0")))
		recorded <- err
	}()
	start := time.Now()
	result, err := m.Verify(context.Background(), orchestrator.WorkItem{Env: "local"})
	if err := <-recorded; err != nil {
		t.Fatal(err)
	}
	if err != nil || !result.OK {
		t.Errorf("Verify: %+v, %v", result, err)
	}
	if waited := time.Since(start); waited < 100*time.Millisecond {
		t.Errorf("Verify passed after %s, before the observation after the apply", waited)
	}
}

func TestProbeWithTheImageSourceTakesTheVersionOfTheNewestObservation(t *testing.T) {
	now := time.Date(2026, 11, 25, 18, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		age  time.Duration
		pods []gatewayPod
		want string
	}{
		{"one version", 10 * time.Second,
			[]gatewayPod{readyPod(firstPod, "v3.4.0"), readyPod(secondPod, "3.4.0")}, "3.4.0"},
		{"two versions", 10 * time.Second,
			[]gatewayPod{readyPod(firstPod, "3.4.0"), readyPod(secondPod, "3.3.0")}, ""},
		{"a tag that is not a version", 10 * time.Second,
			[]gatewayPod{readyPod(firstPod, "3.4.0"), readyPod(secondPod, "latest")}, ""},
		{"a stale observation", 2 * StaleAfter,
			[]gatewayPod{readyPod(firstPod, "3.4.0")}, ""},
		{"no instance", 10 * time.Second, nil, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := imageDeployed(t)
			observe(t, m, now.Add(-test.age), test.pods...)
			m.Wall = func() time.Time { return now }
			sample, err := m.Probe(context.Background(), "local", 1)
			if err != nil || sample.Version != test.want || sample.OK != 1 {
				t.Errorf("Probe: %+v, %v; want the version %q", sample, err, test.want)
			}
		})
	}
	m := imageDeployed(t)
	if sample, err := m.Probe(context.Background(), "local", 1); err != nil ||
		sample.Version != "" {
		t.Errorf("Probe with no observation: %+v, %v", sample, err)
	}
}

func TestComponentVersionNeedsOneVersionOnEachInstance(t *testing.T) {
	record := observation.Observation{Components: []observation.Component{
		{Name: "web", Instances: []observation.Instance{{ID: "a", Version: "1.0.0"}}},
		{Name: "api", Instances: []observation.Instance{{ID: "b", Version: "2.0.0"},
			{ID: "c", Version: "2.0.0"}}},
	}}
	if got := componentVersion(record, "api"); got != "2.0.0" {
		t.Errorf("componentVersion(api) = %q", got)
	}
	if got := componentVersion(record, "worker"); got != "" {
		t.Errorf("componentVersion of a component with no instance = %q", got)
	}
}
