package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/statik/site-reliability-clanker/internal/awsparams"
)

// parameterPathEnv names the path in Parameter Store that holds the settings of the core
// on AWS, for example /src/prod. The path is not a secret, so the machine image can carry
// it. regionEnv is the region of that store.
const (
	parameterPathEnv = "CLANKER_PARAMETER_PATH"
	regionEnv        = "AWS_REGION"
)

// loadParameters puts the settings from Parameter Store into the environment of the core
// on AWS, before any part of the core reads a setting. A setting of the store replaces one
// of the environment. Without the path nothing changes, which is for a test on a laptop.
func loadParameters(ctx context.Context, o options,
	store func(region string) awsparams.Store) error {
	path := os.Getenv(parameterPathEnv)
	if o.cloud != "aws" || path == "" {
		return nil
	}
	region := os.Getenv(regionEnv)
	if region == "" {
		return fmt.Errorf("%s is set, so the control plane reads Parameter Store: set %s to "+
			"the region of the store", parameterPathEnv, regionEnv)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	settings, err := awsparams.Load(ctx, store(region), path)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(settings))
	for name, value := range settings {
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("set %s from Parameter Store: %w", name, err)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	slog.Info("settings read from Parameter Store", "path", path, "names", names)
	return nil
}
