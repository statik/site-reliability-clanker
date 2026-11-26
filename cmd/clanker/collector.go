package main

import (
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/scrub"
)

// collectorEndpoint returns where the collector of the cluster sends its data. On the local
// target a container of the local cluster reaches the port of the listen address on this
// machine. On AWS the cluster is in the network of the core and reaches the core by the
// name of its load balancer: the public URL, which the home stack sets, as it sets its
// output collector_endpoint. Another cloud, and AWS without a public URL, get no endpoint.
func collectorEndpoint(cloud, addr string, getenv func(string) string) (string, error) {
	switch cloud {
	case "local":
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return "", fmt.Errorf("the listen address %q has no port for the collector: %w",
				addr, err)
		}
		return "http://host.docker.internal:" + port + "/otlp", nil
	case "aws":
		public := strings.TrimRight(getenv(publicURLEnv), "/")
		if public == "" {
			slog.Warn("the layer of the platform installs no collector, so the monitor gets no " +
				"data: set " + publicURLEnv + " to the address of the core on its load balancer")
			return "", nil
		}
		return public + "/otlp", nil
	default:
		return "", nil
	}
}

// addCollectorVariables gives the jobs the two variables with which the layer of the
// platform installs the collector: where the core takes the data, and the token of the
// environment. A target with no endpoint gets no variable.
func addCollectorVariables(orch *orchestrator.Orchestrator, cloud, addr, env string,
	getenv func(string) string) error {
	endpoint, err := collectorEndpoint(cloud, addr, getenv)
	if err != nil || endpoint == "" {
		return err
	}
	token, err := monitor.CollectorToken(orch.DB, env)
	if err != nil {
		return err
	}
	scrub.Add(token)
	if orch.JobVariables == nil {
		orch.JobVariables = map[string]string{}
	}
	orch.JobVariables["TF_VAR_collector_endpoint"] = endpoint
	orch.JobVariables["TF_VAR_collector_token"] = token
	return nil
}
