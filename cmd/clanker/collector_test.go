package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/scrub"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

func TestTheJobsGetTheEndpointAndTheTokenOfTheCollector(t *testing.T) {
	db := storetest.Open(t)
	public := environment(map[string]string{publicURLEnv: "https://src-prod.example.com/"})
	cases := []struct {
		cloud, env string
		getenv     func(string) string
		endpoint   string
	}{
		{"local", "local", environment(nil), "http://host.docker.internal:7171/otlp"},
		// The home stack on AWS gives the collector the name of the core on its load
		// balancer: the public URL.
		{"aws", "prod", public, "https://src-prod.example.com/otlp"},
	}
	for _, c := range cases {
		for _, before := range []map[string]string{nil, {"TF_VAR_admin_token": "kept-value"}} {
			orch := &orchestrator.Orchestrator{DB: db, JobVariables: before}
			if err := addCollectorVariables(orch, c.cloud, "127.0.0.1:7171", c.env,
				c.getenv); err != nil {
				t.Fatal(err)
			}
			token, err := monitor.CollectorToken(db, c.env)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"TF_VAR_collector_token": token,
				"TF_VAR_collector_endpoint": c.endpoint}
			if before != nil {
				want["TF_VAR_admin_token"] = "kept-value"
			}
			if !reflect.DeepEqual(orch.JobVariables, want) {
				t.Errorf("%s: variables = %v, want %v", c.cloud, orch.JobVariables, want)
			}
			if got := scrub.Text("the token is " + token); strings.Contains(got, token) {
				t.Errorf("%s: the scrubber does not know the token: %s", c.cloud, got)
			}
		}
	}
}

// AWS without a public URL and the other clouds have no address that the cluster reaches,
// so their jobs get no variable and the environment gets no token.
func TestATargetWithoutAnEndpointGetsNoCollector(t *testing.T) {
	db := storetest.Open(t)
	for _, cloud := range []string{"aws", "gcp"} {
		orch := &orchestrator.Orchestrator{DB: db}
		if err := addCollectorVariables(orch, cloud, "127.0.0.1:7171", "prod",
			environment(nil)); err != nil {
			t.Fatal(err)
		}
		stored, err := store.GetKV(db, "collector.token.prod")
		if orch.JobVariables != nil || stored != "" || err != nil {
			t.Errorf("%s: variables %v, token %q, %v", cloud, orch.JobVariables, stored, err)
		}
	}
}

func TestTheLocalTargetNeedsAPortForTheCollector(t *testing.T) {
	err := addCollectorVariables(&orchestrator.Orchestrator{DB: storetest.Open(t)}, "local",
		"localhost", "local", environment(nil))
	want := `the listen address "localhost" has no port for the collector: ` +
		`address localhost: missing port in address`
	if err == nil || err.Error() != want {
		t.Errorf("an address with no port: %v, want %s", err, want)
	}
}
