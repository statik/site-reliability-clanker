package pack

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Deploy kinds.
const DeployHelm = "helm"

// The Helm timeout of an apply, in seconds. The apply grant of a change lasts 30 minutes,
// so a timeout must end well inside it.
const (
	DefaultHelmTimeout = 300
	minHelmTimeout     = 60
	maxHelmTimeout     = 1200
)

// Limits of the lists of the deployment.
const (
	maxSettings     = 16
	maxSecrets      = 8
	maxRoutePaths   = 8
	maxRouteTimeout = 3600
	maxSettingValue = 1_000_000
)

// Types of a setting.
const (
	SettingNumber = "number"
	SettingBool   = "bool"
)

// Clouds are the targets that a pack can give a values file for.
var Clouds = []string{"local", "aws", "gcp", "azure"}

// coreVariables are the variables of the application layer that the core owns on some
// target. A setting of a pack cannot have one of these names.
var coreVariables = []string{"env_name", "state_location", "region", "location", "project_id",
	"subscription_id", "admin_token", "workload_runtime", "kubelogin_login"}

// platformHelmRoot is the root of the Helm values that the platform gives a chart. A
// setting cannot set one.
const platformHelmRoot = "platform"

var (
	// A Helm value path is dotted names only: no index, no escape, no list.
	helmPathPattern = regexp.MustCompile(
		`^[A-Za-z][A-Za-z0-9_-]{0,62}(\.[A-Za-z][A-Za-z0-9_-]{0,62}){0,7}$`)
	valuesFilePattern = regexp.MustCompile(`^values(-[a-z]+)?\.yaml$`)
)

// Deploy says how the layer of the application deploys it. Every file that it names goes
// to the directory PlatformDir of the platform repository (see PlatformFiles).
type Deploy struct {
	// Kind is DeployHelm: the layer installs a chart with Helm.
	Kind string `json:"kind"`
	// Chart is the directory of the main chart, inside the pack. It is optional when every
	// release has a record that names a chart.
	Chart string `json:"chart,omitempty"`
	// Values is the directory of the values files, inside the pack: values.yaml, and
	// values-<cloud>.yaml for a cloud of Clouds.
	Values string `json:"values,omitempty"`
	// Extras is the directory of a second chart, inside the pack, which the layer installs
	// after the main chart: for objects that a chart of another project does not have.
	Extras string `json:"extras,omitempty"`
	// Build is the program of the pack that builds the images of every release and
	// pushes them to a registry. The local target runs it with the registry as argument.
	// A pack whose images are in a registry already has none.
	Build string `json:"build,omitempty"`
	// Settings are the variables of the layer that a change can set, each within bounds.
	Settings []Setting `json:"settings,omitempty"`
	// Secrets are the keys of a secret that the layer makes, with a random value each.
	Secrets []string `json:"secrets,omitempty"`
	// Route is the route of the application on the gateway of the platform.
	Route *Route `json:"route"`
	// TimeoutSeconds is the Helm timeout of an apply. HelmTimeout gives the default.
	TimeoutSeconds *int `json:"timeout_seconds,omitempty"`
	// WorkloadsNamespace asks the platform for a second namespace, for the pods that the
	// application starts.
	WorkloadsNamespace bool `json:"workloads_namespace,omitempty"`
}

// Setting is one variable of the layer that sets one Helm value.
type Setting struct {
	// Variable is the name of the variable of the layer.
	Variable string `json:"variable"`
	// Type is SettingNumber, a whole number from Min to Max, or SettingBool.
	Type string `json:"type"`
	Min  *int64 `json:"min,omitempty"`
	Max  *int64 `json:"max,omitempty"`
	// Default is the value of a new environment, as a JSON number or boolean.
	Default json.RawMessage `json:"default"`
	// Value is the dotted path of the Helm value that the variable sets.
	Value string `json:"value"`
}

// Route is the route of the application on the gateway of the platform.
type Route struct {
	// Service and Port are the Kubernetes service of the application that gets the
	// requests.
	Service string `json:"service"`
	Port    int    `json:"port"`
	// Paths are the path prefixes of the route. With none, the route has every path.
	Paths []string `json:"paths,omitempty"`
	// TimeoutSeconds is the request timeout of the route. 0 is no timeout. With none, the
	// gateway has its own default.
	TimeoutSeconds *int `json:"timeout_seconds,omitempty"`
	// Hostname gives the application a host name of its own on the domain of the
	// platform. Wildcard adds every name below it.
	Hostname bool `json:"hostname,omitempty"`
	Wildcard bool `json:"wildcard,omitempty"`
}

// HelmTimeout returns the Helm timeout of an apply, in seconds.
func (d Deploy) HelmTimeout() int {
	if d.TimeoutSeconds == nil {
		return DefaultHelmTimeout
	}
	return *d.TimeoutSeconds
}

// checkDeploy checks the values of the deployment. It reads no file.
func (p Pack) checkDeploy() error {
	deploy := p.Deploy
	if deploy.Kind != DeployHelm {
		return fmt.Errorf("deploy.kind is %q; this version reads %q", deploy.Kind, DeployHelm)
	}
	if outOfRange(deploy.TimeoutSeconds, minHelmTimeout, maxHelmTimeout) {
		return fmt.Errorf("deploy.timeout_seconds is %d; it must be from %d to %d",
			*deploy.TimeoutSeconds, minHelmTimeout, maxHelmTimeout)
	}
	if err := checkSecrets(deploy.Secrets); err != nil {
		return err
	}
	if err := checkRoute(deploy.Route); err != nil {
		return err
	}
	return p.checkSettings()
}

func checkSecrets(secrets []string) error {
	if badLength(secrets, maxSecrets) {
		return fmt.Errorf("deploy.secrets must have 1 to %d keys", maxSecrets)
	}
	for index, key := range secrets {
		if !namePattern.MatchString(key) || slices.Contains(secrets[:index], key) {
			return fmt.Errorf("the key %q of deploy.secrets must match %s and be in the list "+
				"one time", key, namePattern)
		}
	}
	return nil
}

func checkRoute(route *Route) error {
	if route == nil {
		return fmt.Errorf("deploy.route is required: the checks reach the application " +
			"through it")
	}
	if !namePattern.MatchString(route.Service) {
		return fmt.Errorf("deploy.route.service is %q; it must match %s", route.Service,
			namePattern)
	}
	if route.Port < 1 || route.Port > 65535 {
		return fmt.Errorf("deploy.route.port is %d; it must be from 1 to 65535", route.Port)
	}
	if outOfRange(route.TimeoutSeconds, 0, maxRouteTimeout) {
		return fmt.Errorf("deploy.route.timeout_seconds is %d; it must be from 0 (no "+
			"timeout) to %d", *route.TimeoutSeconds, maxRouteTimeout)
	}
	if route.Wildcard && !route.Hostname {
		return fmt.Errorf("deploy.route.wildcard needs deploy.route.hostname")
	}
	return checkRoutePaths(route.Paths)
}

func checkRoutePaths(paths []string) error {
	if badLength(paths, maxRoutePaths) {
		return fmt.Errorf("deploy.route.paths must have 1 to %d prefixes; with none, the "+
			"route has every path", maxRoutePaths)
	}
	for index, prefix := range paths {
		if !urlPathPattern.MatchString(prefix) || hasDotStep(prefix) ||
			slices.Contains(paths[:index], prefix) {
			return fmt.Errorf("the prefix %q of deploy.route.paths must match %s, have no "+
				"\".\" or \"..\" step, and be in the list one time", prefix, urlPathPattern)
		}
	}
	return nil
}

// outOfRange reports whether an optional number is given and is outside low to high.
func outOfRange(value *int, low, high int) bool {
	return value != nil && (*value < low || *value > high)
}

// badLength reports whether an optional list is given and does not have 1 to most entries.
func badLength[T any](list []T, most int) bool {
	return list != nil && (len(list) == 0 || len(list) > most)
}

// hasDotStep reports whether a path with slashes has a "." or ".." step.
func hasDotStep(value string) bool {
	steps := strings.Split(value, "/")
	return slices.Contains(steps, ".") || slices.Contains(steps, "..")
}

// between reports whether value is from low to high.
func between(value, low, high int64) bool { return value >= low && value <= high }

func (p Pack) checkSettings() error {
	settings := p.Deploy.Settings
	if badLength(settings, maxSettings) {
		return fmt.Errorf("deploy.settings must have 1 to %d entries", maxSettings)
	}
	taken := append([]string{p.VersionVariable}, coreVariables...)
	paths := []string{}
	for _, setting := range settings {
		if err := checkSetting(setting, taken, paths); err != nil {
			return err
		}
		taken, paths = append(taken, setting.Variable), append(paths, setting.Value)
	}
	return nil
}

// checkSetting checks one setting against the variables and the Helm paths before it.
func checkSetting(setting Setting, taken, paths []string) error {
	name := setting.Variable
	if !variablePattern.MatchString(name) {
		return fmt.Errorf("the setting %q must match %s", name, variablePattern)
	}
	if slices.Contains(taken, name) {
		return fmt.Errorf("the setting %s has the name of the version variable, of a variable "+
			"of the core, or of another setting", name)
	}
	if err := checkSettingPath(setting, paths); err != nil {
		return err
	}
	switch setting.Type {
	case SettingNumber:
		return checkNumberSetting(setting)
	case SettingBool:
		return checkBoolSetting(setting)
	}
	return fmt.Errorf("the setting %s has the type %q; this version reads %q or %q", name,
		setting.Type, SettingNumber, SettingBool)
}

// checkSettingPath refuses a Helm path that is not plain, that is a value of the platform,
// or that is inside the path of another setting, or around it.
func checkSettingPath(setting Setting, paths []string) error {
	value := setting.Value
	if !helmPathPattern.MatchString(value) || overlaps(value, platformHelmRoot) {
		return fmt.Errorf("the value %q of the setting %s must be a dotted Helm path that "+
			"matches %s, outside %q", value, setting.Variable, helmPathPattern, platformHelmRoot)
	}
	for _, other := range paths {
		if overlaps(value, other) {
			return fmt.Errorf("the value %s of the setting %s overlaps the value %s of another "+
				"setting", value, setting.Variable, other)
		}
	}
	return nil
}

// overlaps reports whether one dotted path is the other or inside it.
func overlaps(a, b string) bool {
	return strings.HasPrefix(a+".", b+".") || strings.HasPrefix(b+".", a+".")
}

func checkBoolSetting(setting Setting) error {
	if setting.Min != nil || setting.Max != nil {
		return fmt.Errorf("the setting %s is a bool and has no min or max", setting.Variable)
	}
	if raw := string(setting.Default); raw != "true" && raw != "false" {
		return fmt.Errorf("the default of the setting %s must be true or false",
			setting.Variable)
	}
	return nil
}

func checkNumberSetting(setting Setting) error {
	name := setting.Variable
	if setting.Min == nil || setting.Max == nil {
		return fmt.Errorf("the setting %s is a number and needs min and max", name)
	}
	low, high := *setting.Min, *setting.Max
	if !between(low, 0, high) || high > maxSettingValue {
		return fmt.Errorf("the setting %s has min %d and max %d; they must be from 0 to %d, "+
			"with min not above max", name, low, high, maxSettingValue)
	}
	value, err := strconv.ParseInt(string(setting.Default), 10, 64)
	if err != nil || !between(value, low, high) {
		return fmt.Errorf("the default of the setting %s must be a whole number from %d to %d",
			name, low, high)
	}
	return nil
}

// checkDeployFiles checks the directories of the deployment, which are files of the pack.
func (p Pack) checkDeployFiles() error {
	for field, dir := range map[string]string{"deploy.chart": p.Deploy.Chart,
		"deploy.extras": p.Deploy.Extras} {
		if dir == "" {
			continue
		}
		if _, err := fs.Stat(p.files, path.Join(dir, "Chart.yaml")); err != nil {
			return fmt.Errorf("%s is %q, which has no Chart.yaml", field, dir)
		}
	}
	if p.Deploy.Values == "" {
		return nil
	}
	return p.checkValuesFiles()
}

// checkValuesFiles checks that the directory of the values has values.yaml and otherwise
// only values-<cloud>.yaml. It does not parse the YAML.
func (p Pack) checkValuesFiles() error {
	files, err := p.partFiles(p.Deploy.Values)
	if err != nil {
		return fmt.Errorf("deploy.values: %w", err)
	}
	if !slices.Contains(files, "values.yaml") {
		return fmt.Errorf("deploy.values: %s has no values.yaml", p.Deploy.Values)
	}
	for _, file := range files {
		cloud := strings.TrimSuffix(strings.TrimPrefix(file, "values-"), ".yaml")
		if !valuesFilePattern.MatchString(file) || (file != "values.yaml" &&
			!slices.Contains(Clouds, cloud)) {
			return fmt.Errorf("deploy.values: %s/%s is not values.yaml or values-<cloud>.yaml "+
				"for a cloud of %s", p.Deploy.Values, file, strings.Join(Clouds, ", "))
		}
	}
	return nil
}
