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

func TestTheJobsOfTheLocalTargetGetTheEndpointAndTheTokenOfTheCollector(t *testing.T) {
	db := storetest.Open(t)
	for _, before := range []map[string]string{nil, {"TF_VAR_admin_token": "kept-value"}} {
		orch := &orchestrator.Orchestrator{DB: db, JobVariables: before}
		if err := addCollectorVariables(orch, "local", "127.0.0.1:7171", "local"); err != nil {
			t.Fatal(err)
		}
		token, err := monitor.CollectorToken(db, "local")
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"TF_VAR_collector_token": token,
			"TF_VAR_collector_endpoint": "http://host.docker.internal:7171/otlp"}
		if before != nil {
			want["TF_VAR_admin_token"] = "kept-value"
		}
		if !reflect.DeepEqual(orch.JobVariables, want) {
			t.Errorf("variables = %v, want %v", orch.JobVariables, want)
		}
		if got := scrub.Text("the token is " + token); strings.Contains(got, token) {
			t.Errorf("the scrubber does not know the token: %s", got)
		}
	}
	other := &orchestrator.Orchestrator{DB: db}
	if err := addCollectorVariables(other, "aws", "127.0.0.1:7171", "prod"); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetKV(db, "collector.token.prod")
	if other.JobVariables != nil || stored != "" || err != nil {
		t.Errorf("another cloud: variables %v, token %q, %v", other.JobVariables, stored, err)
	}
	err = addCollectorVariables(&orchestrator.Orchestrator{DB: db}, "local", "localhost",
		"local")
	want := `the listen address "localhost" has no port for the collector: ` +
		`address localhost: missing port in address`
	if err == nil || err.Error() != want {
		t.Errorf("an address with no port: %v, want %s", err, want)
	}
}
