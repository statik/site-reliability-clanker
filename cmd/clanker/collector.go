package main

import (
	"fmt"
	"net"

	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/scrub"
)

// addCollectorVariables gives the jobs of the local target the two variables with which
// the layer of the platform installs the collector: where the core listens, as a
// container of the local cluster reaches this machine, and the token of the environment.
// The jobs of another cloud get no variable: a cluster there has no path to this address.
func addCollectorVariables(orch *orchestrator.Orchestrator, cloud, addr, env string) error {
	if cloud != "local" {
		return nil
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("the listen address %q has no port for the collector: %w", addr, err)
	}
	token, err := monitor.CollectorToken(orch.DB, env)
	if err != nil {
		return err
	}
	scrub.Add(token)
	if orch.JobVariables == nil {
		orch.JobVariables = map[string]string{}
	}
	orch.JobVariables["TF_VAR_collector_endpoint"] = "http://host.docker.internal:" + port +
		"/otlp"
	orch.JobVariables["TF_VAR_collector_token"] = token
	return nil
}
