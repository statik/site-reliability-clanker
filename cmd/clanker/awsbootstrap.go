package main

import (
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/awstarget"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// bootstrapEnv holds the outputs of layer 00-bootstrap on AWS: what `tofu output -json`
// prints for that layer. None of them is sensitive. An operator applies the layer from a
// laptop, so no apply job of the control plane reports them. Parameter Store can give the
// setting like the other settings.
const bootstrapEnv = "CLANKER_AWS_BOOTSTRAP"

// awsEnvironment reads the outputs of layer 00-bootstrap on AWS. With them, the control
// plane records them as the outputs of that layer, so that the layer above can be planned;
// the environment gets the state backend and the region of its layers; and the broker gets
// an issuer of STS sessions for the roles of the identities, which also gives a debug
// session or a runbook a kubeconfig for the cluster of layer 20-cluster. Without the
// setting the control plane starts before layer 00-bootstrap exists: the environment has no
// backend and the broker no issuer. A setting that is not valid stops the start.
func awsEnvironment(c *components, env orchestrator.EnvConfig,
	client func(region string) awstarget.STS) (orchestrator.EnvConfig, broker.Issuer, error) {
	text := os.Getenv(bootstrapEnv)
	if text == "" {
		slog.Warn("the control plane plans no layer and the broker issues no AWS credential: " +
			"set " + bootstrapEnv + " to what `tofu output -json` prints in layer 00-bootstrap")
		return env, nil, nil
	}
	bootstrap, err := awstarget.ParseBootstrap(text)
	if err != nil {
		return env, nil, fmt.Errorf("%s: %w", bootstrapEnv, err)
	}
	region := os.Getenv(regionEnv)
	if region == "" {
		return env, nil, fmt.Errorf("%s is set, so the broker calls STS: set %s to the region "+
			"of the control plane", bootstrapEnv, regionEnv)
	}
	if err := store.SetLayerOutputs(c.db, env.Name, orchestrator.Layers[0], bootstrap.Outputs,
		store.FormatTime(c.clock.Now())); err != nil {
		return env, nil, err
	}
	env.Backend, env.Region = bootstrap.Backend, bootstrap.Region
	slog.Info("the layers keep their state in S3", "env", env.Name, "region", env.Region,
		"bucket", env.Backend["bucket"])
	slog.Info("the broker issues STS sessions", "region", region,
		"identities", slices.Sorted(maps.Keys(bootstrap.Roles)))
	return env, awstarget.Credentials{STS: client(region), Roles: bootstrap.Roles,
		Region: bootstrap.Region,
		ClusterOutputs: func(env string) (map[string]any, error) {
			return store.LayerOutputs(c.db, env, awstarget.ClusterLayer)
		},
		Record: func(e audit.Event) error {
			_, err := c.log.Append(e)
			return err
		}}, nil
}
