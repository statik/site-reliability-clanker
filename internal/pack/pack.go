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
const Schema = 1

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
	// Chart is the directory of the deployment chart, inside the pack.
	Chart string `json:"chart"`
	// Mount is the path of the pack in the platform repository. The stacks of the
	// platform read the chart from there.
	Mount string `json:"mount"`
	// Build is the program of the pack that builds the images of every release and
	// pushes them to a registry. The local target runs it with the registry as argument.
	Build string `json:"build"`
	// Namespace is the Kubernetes namespace of the application. A debug session reads
	// only there. DataNamespace is the namespace of its data services.
	Namespace     string `json:"namespace"`
	DataNamespace string `json:"data_namespace"`
	// Deployments are the deployments that a runbook can name. IncidentDeployment is the
	// one that an incident names when no model gives a diagnosis.
	Deployments        []string `json:"deployments"`
	IncidentDeployment string   `json:"incident_deployment"`
	// DebugCommands are the kubectl commands of a debug session, with no namespace. The
	// core adds the namespace. Each one must be a read.
	DebugCommands [][]string `json:"debug_commands"`
	Health        Health     `json:"health"`
	Topology      Topology   `json:"topology"`
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

// Health says how the monitor checks the application over HTTP.
type Health struct {
	// ReadyPath answers 200 when the application is ready.
	ReadyPath string `json:"ready_path"`
	// VersionPath answers with a JSON object that has the field `version`.
	VersionPath string `json:"version_path"`
	// ProbePath is the request that the monitor repeats to measure the error rate.
	ProbePath string `json:"probe_path"`
	// ProbeName names that request in a finding: "fails 9 of 20 <name> requests".
	ProbeName string `json:"probe_name"`
}

// Topology is the part of the topology view that belongs to the application.
type Topology struct {
	// Services are the nodes of the application.
	Services []string `json:"services"`
	// Edges join two services, or a service and a node of the platform.
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
	for _, name := range []string{p.Chart, p.Build, p.ReleaseNote(p.Releases.First)} {
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
func (p Pack) ChartMount() string { return path.Join(p.Mount, p.Chart) }

// ReleaseNote returns the path of the note of a release, in the pack and in the releases
// repository.
func (p Pack) ReleaseNote(version string) string {
	return path.Join(p.Releases.Directory, version+".md")
}

// ReleaseOf returns the version of a file in the directory of the release notes.
func (p Pack) ReleaseOf(file string) string { return strings.TrimSuffix(file, ".md") }

// DebugArgs returns the arguments of each kubectl command of a debug session: the verb,
// the namespace of the application, then the rest of the command of the pack.
func (p Pack) DebugArgs() [][]string {
	commands := make([][]string, 0, len(p.DebugCommands))
	for _, command := range p.DebugCommands {
		args := append([]string{command[0], "-n", p.Namespace}, command[1:]...)
		commands = append(commands, args)
	}
	return commands
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
	probePattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,20}( [a-z0-9][a-z0-9-]{0,20})?$`)
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
	for _, validate := range []func() error{p.checkValues, p.checkPaths, p.checkDeployments,
		p.checkDebugCommands, p.checkTopology} {
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
		{"namespace", p.Namespace, namePattern},
		{"data_namespace", p.DataNamespace, namePattern},
		{"health.ready_path", p.Health.ReadyPath, urlPathPattern},
		{"health.version_path", p.Health.VersionPath, urlPathPattern},
		{"health.probe_path", p.Health.ProbePath, urlPathPattern},
		{"health.probe_name", p.Health.ProbeName, probePattern},
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
	paths := map[string]string{"releases.directory": p.Releases.Directory, "chart": p.Chart,
		"mount": p.Mount, "build": p.Build}
	fields := []string{"build", "chart", "mount", "releases.directory"}
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

func (p Pack) checkDeployments() error {
	if len(p.Deployments) == 0 || len(p.Deployments) > 20 {
		return fmt.Errorf("deployments must have 1 to 20 names")
	}
	for _, name := range p.Deployments {
		if !namePattern.MatchString(name) {
			return fmt.Errorf("deployment %q must match %s", name, namePattern)
		}
	}
	if !slices.Contains(p.Deployments, p.IncidentDeployment) {
		return fmt.Errorf("incident_deployment %q is not one of the deployments",
			p.IncidentDeployment)
	}
	return nil
}

func (p Pack) checkTopology() error {
	services := p.Topology.Services
	if len(services) == 0 || len(services) > 20 {
		return fmt.Errorf("topology.services must have 1 to 20 names")
	}
	for index, name := range services {
		if !namePattern.MatchString(name) || slices.Contains(PlatformNodes, name) ||
			slices.Contains(services[:index], name) {
			return fmt.Errorf("topology service %q must match %s, be in the list one time, "+
				"and not have the name of a node of the platform", name, namePattern)
		}
	}
	for _, edge := range p.Topology.Edges {
		if !p.joins(edge) {
			return fmt.Errorf("topology edge %s to %s must join two nodes that exist, and one "+
				"of them must be a service of the pack", edge[0], edge[1])
		}
	}
	return nil
}

// joins reports whether an edge joins two nodes that exist, with a service of the pack at
// one end or at both.
func (p Pack) joins(edge [2]string) bool {
	services := p.Topology.Services
	owned := 0
	for _, name := range edge {
		switch {
		case slices.Contains(services, name):
			owned++
		case !slices.Contains(PlatformNodes, name):
			return false
		}
	}
	return owned > 0
}
