package otlp_test

import (
	"os"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/otlp"
)

func TestParseMetricsReadsARequestOfTheCollector(t *testing.T) {
	body, err := os.ReadFile("testdata/k8s-cluster.json")
	if err != nil {
		t.Fatal(err)
	}
	points, err := otlp.ParseMetrics(body)
	if err != nil {
		t.Fatal(err)
	}
	type key struct{ name, deployment, pod string }
	values := map[key]float64{}
	for _, point := range points {
		values[key{point.Name, point.Attributes["k8s.deployment.name"],
			point.Attributes["k8s.pod.name"]}] = point.Value
		if point.Time.Year() != 2026 || point.Time.Location() != time.UTC {
			t.Fatalf("the point %s has the time %s", point.Name, point.Time)
		}
	}
	want := map[key]float64{
		{"k8s.deployment.desired", "paperboat-api", ""}:                     1,
		{"k8s.deployment.available", "paperboat-web", ""}:                   1,
		{"k8s.pod.phase", "", "paperboat-web-88d77c669-s8bx8"}:              2,
		{"k8s.pod.phase", "", "paperboat-migrate-7rh8k"}:                    3,
		{"k8s.container.ready", "", "paperboat-api-5d5dc8b5b8-svpq5"}:       1,
		{"k8s.container.restarts", "", "paperboat-worker-7f7fb9ff5d-6hgw4"}: 0,
		{"k8s.container.cpu_request", "", "paperboat-web-88d77c669-s8bx8"}:  0.025,
	}
	for k, value := range want {
		if got, ok := values[k]; !ok || got != value {
			t.Errorf("%+v = %v (found %v), want %v", k, got, ok, value)
		}
	}
	for _, point := range points {
		if point.Name == "k8s.container.ready" &&
			point.Attributes["k8s.pod.name"] == "paperboat-web-88d77c669-s8bx8" {
			if point.Attributes["container.image.tag"] != "1.0.0" ||
				point.Attributes["k8s.namespace.name"] != "paperboat" {
				t.Errorf("attributes = %v", point.Attributes)
			}
		}
	}
}

func TestParseMetricsReadsGaugesAndSums(t *testing.T) {
	body := `{"resourceMetrics":[{"resource":{"attributes":[
		{"key":"host","value":{"stringValue":"a"}},{"key":"cores","value":{"intValue":"4"}}]},
		"scopeMetrics":[{"metrics":[
		{"name":"g","gauge":{"dataPoints":[{"timeUnixNano":"1500000000","asInt":"7"}]}},
		{"name":"s","sum":{"dataPoints":[{"timeUnixNano":2000000000,"asDouble":0.5},
			{"timeUnixNano":"3000000000","asInt":9}]}},
		{"name":"h","histogram":{"dataPoints":[{"count":"3"}]}}]}]}]}`
	points, err := otlp.ParseMetrics([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	type flat struct {
		name  string
		value float64
		nanos int64
	}
	got := []flat{}
	for _, point := range points {
		got = append(got, flat{point.Name, point.Value, point.Time.UnixNano()})
		if len(point.Attributes) != 1 || point.Attributes["host"] != "a" {
			t.Errorf("attributes of %s = %v", point.Name, point.Attributes)
		}
	}
	want := []flat{{"g", 7, 1500000000}, {"s", 0.5, 2000000000}, {"s", 9, 3000000000}}
	if len(got) != len(want) {
		t.Fatalf("points = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("point %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestParseMetricsRefuses(t *testing.T) {
	const where = "resourceMetrics[0].scopeMetrics[0].metrics[0]"
	wrap := func(metric string) string {
		return `{"resourceMetrics":[{"scopeMetrics":[{"metrics":[` + metric + `]}]}]}`
	}
	tests := []struct {
		name, body, want string
	}{
		{"an empty body", ``,
			"the body is not an OTLP request for metrics in JSON: the JSON ends too early"},
		{"text that is not JSON", `metrics secret-word`,
			"the body is not an OTLP request for metrics in JSON: a syntax error at byte 1"},
		{"a list", `[]`, "the body is not an OTLP request for metrics in JSON: " +
			"the body has the JSON type array"},
		{"an object of another request", `{"resourceLogs":[]}`,
			"the body has no list resourceMetrics"},
		{"the list as null", `{"resourceMetrics":null}`, "the body has no list resourceMetrics"},
		{"the list as an object", `{"resourceMetrics":{}}`,
			"the body is not an OTLP request for metrics in JSON: " +
				"the field resourceMetrics has the JSON type object"},
		{"two objects", `{"resourceMetrics":[]} {}`, "the body has data after the JSON object"},
		{"a metric with no name", wrap(`{"gauge":{"dataPoints":[]}}`),
			where + ": the metric has no name"},
		{"a data point with no value", wrap(`{"name":"g","gauge":{"dataPoints":[
			{"timeUnixNano":"1"}]}}`),
			where + ".dataPoints[0]: the data point has no asInt and no asDouble"},
		{"a data point with no time", wrap(`{"name":"g","sum":{"dataPoints":[{"asInt":"1"}]}}`),
			where + ".dataPoints[0]: the data point has no timeUnixNano"},
		{"an integer that is text", wrap(`{"name":"g","gauge":{"dataPoints":[
			{"timeUnixNano":"1","asInt":"secret-word"}]}}`),
			where + ".dataPoints[0]: asInt is not a string of digits"},
		{"a time that is an object", wrap(`{"name":"g","gauge":{"dataPoints":[
			{"timeUnixNano":{},"asInt":"1"}]}}`),
			where + ".dataPoints[0]: timeUnixNano is not a string of digits"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			points, err := otlp.ParseMetrics([]byte(test.body))
			if err == nil {
				t.Fatalf("ParseMetrics returned %d points, want the error %q", len(points),
					test.want)
			}
			if err.Error() != test.want {
				t.Errorf("ParseMetrics:\n got %q\nwant %q", err.Error(), test.want)
			}
		})
	}
	if points, err := otlp.ParseMetrics([]byte(`{"resourceMetrics":[]}`)); err != nil ||
		len(points) != 0 {
		t.Errorf("a request with no resource: %v, %v", points, err)
	}
}
