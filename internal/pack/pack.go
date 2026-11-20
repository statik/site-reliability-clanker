// Package pack reads an application pack: the directory that tells the control plane what
// it must know about one managed application. The core names no application. It reads the
// names of the repository, the variables, the namespaces, the deployments and the checks
// from the manifest `pack.json` of the pack.
//
// A pack comes from outside the core, so its content is not trusted. Load refuses a field
// that it does not know and checks every value against a fixed pattern.
package pack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
)

// ManifestName is the name of the manifest in the directory of a pack.
const ManifestName = "pack.json"

// Schema is the version of the manifest format that this code reads.
const Schema = 2

// Pack is the manifest of one application pack.
type Pack struct {
	Schema int `json:"schema"`
	// Name is the short name of the application. The API uses it as the key of the
	// application in its lists of versions.
	Name string `json:"name"`
	// Title is the name of the application as a person reads it.
	Title string `json:"title"`
	// Version is the version of the pack, not of the application.
	Version     string `json:"version"`
	Description string `json:"description"`
	// Layer is the platform layer that deploys the application.
	Layer string `json:"layer"`
	// VersionVariable is the variable of the layer that holds the version to deploy. The
	// layer has an output of the same name with the version that runs.
	VersionVariable string `json:"version_variable"`
	// URLOutput is the output of the layer with the address of the application.
	URLOutput string   `json:"url_output"`
	Releases  Releases `json:"releases"`
	// Deploy says how the layer deploys the application.
	Deploy Deploy `json:"deploy"`
	// Components are the parts of the application that run. A runbook, a diagnostic read
	// and the Investigator can name only these.
	Components []Component `json:"components"`
	// Incident is the mitigation that an incident proposes when no model gives a
	// diagnosis. A pack can have none: the incident then goes to a person.
	Incident *Incident `json:"incident,omitempty"`
	// Diagnostics are the reads of a debug session. The core makes the commands.
	Diagnostics []Read `json:"diagnostics"`
	// Checks are the tests of the application over HTTP. Exactly one is the primary check.
	Checks []HealthCheck `json:"checks"`
	// AppVersion says where the monitor reads the version of the application that runs.
	AppVersion AppVersion `json:"app_version"`
	Topology   Topology   `json:"topology"`
	// Runbooks is the directory of the runbooks of the application, inside the pack: one
	// definition and one script for each. The core puts them in the directory of the jobs
	// of the runbooks repository. A pack can have none.
	Runbooks string `json:"runbooks,omitempty"`
	// Evals is the directory of the eval cases of the application, inside the pack: one
	// file `<task>.json` for each task of the eval suite. A pack can have none.
	Evals string `json:"evals,omitempty"`

	// dir is the directory of the pack in the file system that Load read it from, and
	// files are the files of that directory.
	dir   string
	files fs.FS
}

// Releases says where the releases of the application are published.
type Releases struct {
	// Repository is the repository of the forge with one note for each release.
	Repository string `json:"repository"`
	// Directory holds the notes, one file `<version>.md` for each release. It has the
	// same name in the pack and in the repository.
	Directory string `json:"directory"`
	// First is the release that a new environment starts with.
	First string `json:"first"`
}

// Deploy kinds.
const DeployHelm = "helm"

// Deploy says how the layer of the application deploys it.
type Deploy struct {
	// Kind is DeployHelm: the layer installs a chart of the pack.
	Kind string `json:"kind"`
	// Chart is the directory of the deployment chart, inside the pack.
	Chart string `json:"chart"`
	// Mount is the path of the pack in the platform repository. The stacks of the
	// platform read the chart from there.
	Mount string `json:"mount"`
	// Build is the program of the pack that builds the images of every release and
	// pushes them to a registry. The local target runs it with the registry as argument.
	// A pack whose images are in a registry already has none.
	Build string `json:"build,omitempty"`
}

// Runtimes of a component.
const RuntimeKubernetes = "kubernetes"

// Component is one part of the application that runs.
type Component struct {
	// Name is the name in findings, in the topology view and in a diagnostic read.
	Name string `json:"name"`
	// Runtime is RuntimeKubernetes.
	Runtime string `json:"runtime"`
	// Workload is the name of the deployment of the component.
	Workload string `json:"workload"`
	// Required means that the application does not work without the component. No code
	// reads it yet: the health rules that do come with the state of the components.
	Required bool `json:"required"`
}

// Incident is the mitigation that an incident proposes when no model gives a diagnosis.
type Incident struct {
	// Runbook is a runbook of the pack that is a mitigation for an incident.
	Runbook string `json:"runbook"`
	// Component is the component that the runbook gets.
	Component string `json:"component"`
}

// Check kinds.
const CheckHTTP = "http"

// HealthCheck is one test of the application.
type HealthCheck struct {
	// Name names the check in a finding: "fails 9 of 20 <name> requests".
	Name string `json:"name"`
	// Kind is CheckHTTP: a GET request on the address of the application.
	Kind string `json:"kind"`
	Path string `json:"path"`
	// Status is the status that passes. The default is 200.
	Status int `json:"status,omitempty"`
	// Primary marks the check that the canary, the incident rule and the recovery rule
	// read. The monitor repeats it to measure the error rate.
	Primary bool `json:"primary,omitempty"`
}

// PassStatus returns the HTTP status that passes the check.
func (c HealthCheck) PassStatus() int {
	if c.Status == 0 {
		return 200
	}
	return c.Status
}

// Version sources.
const VersionHTTPJSON = "http-json"

// AppVersion says where the monitor reads the version of the application that runs.
type AppVersion struct {
	// Source is VersionHTTPJSON: Path answers with a JSON object that has the field
	// `version`.
	Source string `json:"source"`
	Path   string `json:"path"`
}

// Topology is the part of the topology view that belongs to the application. Each
// component is a node.
type Topology struct {
	// Extra are nodes of the application that are not components, for example a load
	// generator.
	Extra []string `json:"extra,omitempty"`
	// Edges join two nodes of the application, or one of them and a node of the platform.
	Edges [][2]string `json:"edges"`
}

// PlatformNodes are the nodes of the topology view that the platform has. An edge of a
// pack can end at one of them.
var PlatformNodes = []string{"network", "registry", "cluster", "nodes", "postgres", "sharedfs",
	"backups", "lb", "secrets"}

// reservedRepositories are the repositories of the core. A pack cannot publish there.
var reservedRepositories = []string{"platform-live", "runbooks"}

// ReservedRepository reports whether the name belongs to a repository of the core.
func ReservedRepository(name string) bool { return slices.Contains(reservedRepositories, name) }

// Load reads and checks the manifest in the directory dir of fsys.
func Load(fsys fs.FS, dir string) (Pack, error) {
	file := path.Join(dir, ManifestName)
	raw, err := fs.ReadFile(fsys, file)
	if err != nil {
		return Pack{}, fmt.Errorf("read the manifest of the application pack: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var p Pack
	if err := decoder.Decode(&p); err != nil {
		return Pack{}, fmt.Errorf("%s is not a correct manifest: %w", file, err)
	}
	if decoder.More() {
		return Pack{}, fmt.Errorf("%s has text after the manifest object", file)
	}
	p.dir = dir
	if err := p.Validate(); err != nil {
		return Pack{}, fmt.Errorf("%s: %w", file, err)
	}
	if p.files, err = fs.Sub(fsys, dir); err != nil {
		return Pack{}, fmt.Errorf("open the application pack in %s: %w", dir, err)
	}
	needed := []string{p.Deploy.Chart, p.ReleaseNote(p.Releases.First)}
	if p.Deploy.Build != "" {
		needed = append(needed, p.Deploy.Build)
	}
	for _, name := range needed {
		if _, err := fs.Stat(p.files, name); err != nil {
			return Pack{}, fmt.Errorf("%s names %s, which the pack does not have", file, name)
		}
	}
	for _, part := range p.CheckParts() {
		if part.Problem != "" {
			return Pack{}, fmt.Errorf("%s: %s: %s", file, part.Name, part.Problem)
		}
	}
	return p, nil
}

// Discover returns the directory of the one pack below root in fsys. It is an error when
// there is none, or more than one: then the caller must name the pack.
func Discover(fsys fs.FS, root string) (string, error) {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return "", fmt.Errorf("look for an application pack in %s: %w", root, err)
	}
	found := []string{}
	for _, entry := range entries {
		dir := path.Join(root, entry.Name())
		if _, err := fs.Stat(fsys, path.Join(dir, ManifestName)); err == nil {
			found = append(found, dir)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no directory in %s has a %s", root, ManifestName)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("%s has %d application packs (%s); name one with --pack", root,
		len(found), strings.Join(found, ", "))
}

// Dir returns the directory of the pack in the file system that Load read it from.
func (p Pack) Dir() string { return p.dir }

// ChartMount returns the path of the chart in the platform repository.
func (p Pack) ChartMount() string { return path.Join(p.Deploy.Mount, p.Deploy.Chart) }

// ReleaseNote returns the path of the note of a release, in the pack and in the releases
// repository.
func (p Pack) ReleaseNote(version string) string {
	return path.Join(p.Releases.Directory, version+".md")
}

// ReleaseOf returns the version of a file in the directory of the release notes.
func (p Pack) ReleaseOf(file string) string { return strings.TrimSuffix(file, ".md") }

// Workloads returns the deployments of the components, in the order of the manifest.
func (p Pack) Workloads() []string {
	names := make([]string, 0, len(p.Components))
	for _, component := range p.Components {
		names = append(names, component.Workload)
	}
	return names
}

// Component returns the component with the name.
func (p Pack) Component(name string) (Component, bool) {
	for _, component := range p.Components {
		if component.Name == name {
			return component, true
		}
	}
	return Component{}, false
}

// PrimaryCheck returns the check that the canary, the incident rule and the recovery rule
// read. Validate makes sure that there is exactly one.
func (p Pack) PrimaryCheck() HealthCheck {
	for _, check := range p.Checks {
		if check.Primary {
			return check
		}
	}
	return HealthCheck{}
}

// TopologyNodes returns the nodes of the application in the topology view: the
// components, then the extra nodes.
func (p Pack) TopologyNodes() []string {
	names := make([]string, 0, len(p.Components)+len(p.Topology.Extra))
	for _, component := range p.Components {
		names = append(names, component.Name)
	}
	return append(names, p.Topology.Extra...)
}

var (
	namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	// A title is one to three words, with no punctuation that can make a sentence.
	titlePattern = regexp.MustCompile(
		`^[A-Za-z][A-Za-z0-9-]{0,19}( [A-Za-z0-9][A-Za-z0-9-]{0,19}){0,2}$`)
	versionPattern  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	layerPattern    = regexp.MustCompile(`^[0-9]{2}-[a-z][a-z0-9-]{0,30}$`)
	variablePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	pathPattern     = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	urlPathPattern  = regexp.MustCompile(`^(/[A-Za-z0-9._-]+)+$`)
)

type check struct {
	field, value string
	pattern      *regexp.Regexp
}

// Validate checks every value of the manifest. It reads no file.
func (p Pack) Validate() error {
	if p.Schema != Schema {
		return fmt.Errorf("schema is %d; this version reads schema %d", p.Schema, Schema)
	}
	if n := len(p.Description); n == 0 || n > 200 || strings.ContainsAny(p.Description, "\n\r\t") {
		return fmt.Errorf("description must be one line of 1 to 200 characters")
	}
	if ReservedRepository(p.Releases.Repository) {
		return fmt.Errorf("releases.repository %q is a repository of the core", p.Releases.Repository)
	}
	for _, validate := range []func() error{p.checkValues, p.checkPaths, p.checkComponents,
		p.checkIncident, p.checkDiagnostics, p.checkChecks, p.checkTopology} {
		if err := validate(); err != nil {
			return err
		}
	}
	return nil
}

func (p Pack) checkValues() error {
	checks := []check{
		{"name", p.Name, namePattern}, {"title", p.Title, titlePattern},
		{"version", p.Version, versionPattern}, {"layer", p.Layer, layerPattern},
		{"version_variable", p.VersionVariable, variablePattern},
		{"url_output", p.URLOutput, variablePattern},
		{"releases.repository", p.Releases.Repository, namePattern},
		{"releases.first", p.Releases.First, versionPattern},
		{"app_version.path", p.AppVersion.Path, urlPathPattern},
	}
	if p.Deploy.Kind != DeployHelm {
		return fmt.Errorf("deploy.kind is %q; this version reads %q", p.Deploy.Kind, DeployHelm)
	}
	if p.AppVersion.Source != VersionHTTPJSON {
		return fmt.Errorf("app_version.source is %q; this version reads %q",
			p.AppVersion.Source, VersionHTTPJSON)
	}
	for _, c := range checks {
		if !c.pattern.MatchString(c.value) {
			return fmt.Errorf("%s is %q; it must match %s", c.field, c.value, c.pattern)
		}
	}
	return nil
}

// checkPaths refuses a path that can leave the pack.
func (p Pack) checkPaths() error {
	paths := map[string]string{"releases.directory": p.Releases.Directory,
		"deploy.chart": p.Deploy.Chart, "deploy.mount": p.Deploy.Mount}
	fields := []string{"deploy.chart", "deploy.mount", "releases.directory"}
	if p.Deploy.Build != "" {
		paths["deploy.build"], fields = p.Deploy.Build, append(fields, "deploy.build")
	}
	if p.Runbooks != "" {
		paths["runbooks"], fields = p.Runbooks, append(fields, "runbooks")
	}
	if p.Evals != "" {
		paths["evals"], fields = p.Evals, append(fields, "evals")
	}
	for _, field := range fields {
		value := paths[field]
		if !pathPattern.MatchString(value) || path.Clean(value) != value ||
			slices.Contains(strings.Split(value, "/"), "..") {
			return fmt.Errorf("%s is %q; it must be a path inside the pack, with no \"..\"",
				field, value)
		}
	}
	return nil
}

func (p Pack) checkComponents() error {
	if len(p.Components) == 0 || len(p.Components) > 20 {
		return fmt.Errorf("components must have 1 to 20 entries")
	}
	names, workloads := []string{}, []string{}
	for _, component := range p.Components {
		if err := checkComponent(component, names, workloads); err != nil {
			return err
		}
		names, workloads = append(names, component.Name), append(workloads, component.Workload)
	}
	return nil
}

// checkComponent checks one component against the names and the workloads before it.
func checkComponent(component Component, names, workloads []string) error {
	if !namePattern.MatchString(component.Name) {
		return fmt.Errorf("component %q must match %s", component.Name, namePattern)
	}
	if slices.Contains(names, component.Name) {
		return fmt.Errorf("component %q is in the list two times", component.Name)
	}
	if slices.Contains(PlatformNodes, component.Name) {
		return fmt.Errorf("component %q has the name of a node of the platform", component.Name)
	}
	if component.Runtime != RuntimeKubernetes {
		return fmt.Errorf("component %s has the runtime %q; this version reads %q",
			component.Name, component.Runtime, RuntimeKubernetes)
	}
	if !namePattern.MatchString(component.Workload) {
		return fmt.Errorf("the workload %q of component %s must match %s", component.Workload,
			component.Name, namePattern)
	}
	if slices.Contains(workloads, component.Workload) {
		return fmt.Errorf("the workload %q is in the list two times", component.Workload)
	}
	return nil
}

func (p Pack) checkIncident() error {
	if p.Incident == nil {
		return nil
	}
	if !namePattern.MatchString(p.Incident.Runbook) {
		return fmt.Errorf("incident.runbook %q must match %s", p.Incident.Runbook, namePattern)
	}
	if _, ok := p.Component(p.Incident.Component); !ok {
		return fmt.Errorf("incident.component %q is not one of the components",
			p.Incident.Component)
	}
	return nil
}

func (p Pack) checkChecks() error {
	if len(p.Checks) == 0 || len(p.Checks) > 8 {
		return fmt.Errorf("checks must have 1 to 8 entries")
	}
	names, primary := []string{}, 0
	for _, check := range p.Checks {
		if slices.Contains(names, check.Name) {
			return fmt.Errorf("check %q is in the list two times", check.Name)
		}
		if err := checkHealthCheck(check); err != nil {
			return err
		}
		if check.Primary {
			primary++
		}
		names = append(names, check.Name)
	}
	if primary != 1 {
		return fmt.Errorf("checks must have exactly one primary check, and have %d", primary)
	}
	return nil
}

// checkHealthCheck checks the values of one check.
func checkHealthCheck(check HealthCheck) error {
	if !namePattern.MatchString(check.Name) {
		return fmt.Errorf("check %q must match %s", check.Name, namePattern)
	}
	if check.Kind != CheckHTTP {
		return fmt.Errorf("check %s has the kind %q; this version reads %q", check.Name,
			check.Kind, CheckHTTP)
	}
	if !urlPathPattern.MatchString(check.Path) {
		return fmt.Errorf("the path %q of check %s must match %s", check.Path, check.Name,
			urlPathPattern)
	}
	// The monitor follows a redirect, so a status from 300 to 399 never reaches it.
	if check.Status != 0 && (check.Status < 200 || check.Status > 299) {
		return fmt.Errorf("the status %d of check %s must be from 200 to 299", check.Status,
			check.Name)
	}
	return nil
}

func (p Pack) checkTopology() error {
	nodes := p.TopologyNodes()
	if len(nodes) > 20 {
		return fmt.Errorf("the topology must have 20 nodes or fewer")
	}
	for index, name := range p.Topology.Extra {
		at := len(p.Components) + index
		if !namePattern.MatchString(name) || slices.Contains(PlatformNodes, name) ||
			slices.Contains(nodes[:at], name) {
			return fmt.Errorf("topology node %q must match %s, be in the list one time, not "+
				"be a component, and not have the name of a node of the platform", name,
				namePattern)
		}
	}
	for _, edge := range p.Topology.Edges {
		if !p.joins(edge) {
			return fmt.Errorf("topology edge %s to %s must join two nodes that exist, and one "+
				"of them must be a node of the pack", edge[0], edge[1])
		}
	}
	return nil
}

// joins reports whether an edge joins two nodes that exist, with a node of the pack at
// one end or at both.
func (p Pack) joins(edge [2]string) bool {
	nodes := p.TopologyNodes()
	owned := 0
	for _, name := range edge {
		switch {
		case slices.Contains(nodes, name):
			owned++
		case !slices.Contains(PlatformNodes, name):
			return false
		}
	}
	return owned > 0
}
