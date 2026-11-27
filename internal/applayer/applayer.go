// Package applayer writes the part of the application layer that comes from the manifest of
// a pack. OpenTofu needs declared names, so for each target the stack of the layer has a
// file app.tf with the version variable, the settings, the URL and version outputs and the
// local value app that main.tf gives the shared module. The package also writes the lines
// of the pack in terraform.tfvars of the layer and the line app_name of 40-platform.
//
// Every name and value that goes into a file comes from a pack that Load checked, so each
// is a name, a number or a path of a fixed pattern. The package starts no program: the
// core on a cloud seeds the platform repository with it.
package applayer

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing/fstest"
	"text/template"

	"github.com/statik/site-reliability-clanker/internal/pack"
)

const (
	// AppFile is the generated file of each stack of the layer of the pack.
	AppFile = "app.tf"
	// VarsFile is the file of the values of a stack.
	VarsFile = "terraform.tfvars"
	// PlatformLayer is the layer whose app_name line names the pack.
	PlatformLayer = "40-platform"
)

//go:embed app.tf.tmpl
var appTemplate string

var (
	render = template.Must(template.New(AppFile).Funcs(template.FuncMap{
		"quote": quote, "list": list}).Parse(appTemplate))
	// assignment is a line of a tfvars file that sets one variable on one line.
	assignment  = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)\s*=\s*(\S.*)$`)
	appNameLine = regexp.MustCompile(`(?m)^(app_name\s*=\s*)"[^"\n]*"$`)
)

// Files returns the generated files of the pack by their path in the platform tree (the
// directory platform of the source files): for each target whose stack of the layer of the
// pack is in the tree, app.tf and terraform.tfvars of that stack and terraform.tfvars of
// 40-platform. It reads the tfvars files of the tree.
func Files(app pack.Pack, platform fs.FS) (map[string][]byte, error) {
	appTF, err := AppTF(app)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, cloud := range pack.Clouds {
		stack := path.Join("stacks", cloud, app.Layer)
		if _, err := fs.Stat(platform, stack); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		files[path.Join(stack, AppFile)] = appTF
		layerVars := path.Join(stack, VarsFile)
		if files[layerVars], err = rewrite(platform, layerVars, app, LayerVars); err != nil {
			return nil, err
		}
		platformVars := path.Join("stacks", cloud, PlatformLayer, VarsFile)
		if files[platformVars], err = rewrite(platform, platformVars, app,
			PlatformVars); err != nil {
			return nil, err
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("the platform has no stack of the layer %s of the pack %s for "+
			"any target (%s)", app.Layer, app.Name, strings.Join(pack.Clouds, ", "))
	}
	return files, nil
}

// rewrite returns a file of the platform tree after change.
func rewrite(platform fs.FS, file string, app pack.Pack,
	change func(pack.Pack, []byte) ([]byte, error)) ([]byte, error) {
	text, err := fs.ReadFile(platform, file)
	if err == nil {
		text, err = change(app, text)
	}
	if err != nil {
		return nil, fmt.Errorf("write platform/%s for the pack %s: %w", file, app.Name, err)
	}
	return text, nil
}

// Platform returns the platform tree with the generated files of the pack in place of the
// files of the same path. Each generated file is in a directory of the tree.
func Platform(app pack.Pack, platform fs.FS) (fs.FS, error) {
	files, err := Files(app, platform)
	if err != nil {
		return nil, err
	}
	generated := fstest.MapFS{}
	for name, data := range files {
		generated[name] = &fstest.MapFile{Data: data, Mode: 0o644}
	}
	return overlay{base: platform, generated: generated}, nil
}

// overlay is a file system with the files of generated in place of those of base.
type overlay struct {
	base      fs.FS
	generated fstest.MapFS
}

func (o overlay) Open(name string) (fs.File, error) {
	if _, ok := o.generated[name]; ok {
		return o.generated.Open(name)
	}
	file, err := o.base.Open(name)
	if err != nil {
		return nil, err
	}
	dir, ok := file.(fs.ReadDirFile)
	if info, err := file.Stat(); !ok || err != nil || !info.IsDir() {
		return file, nil
	}
	return &overlayDir{ReadDirFile: dir, list: func() ([]fs.DirEntry, error) {
		return o.ReadDir(name)
	}}, nil
}

// overlayDir is an open directory of an overlay. It lists the entries of ReadDir.
type overlayDir struct {
	fs.ReadDirFile
	list    func() ([]fs.DirEntry, error)
	entries []fs.DirEntry
	listed  bool
}

func (d *overlayDir) ReadDir(count int) ([]fs.DirEntry, error) {
	if !d.listed {
		entries, err := d.list()
		if err != nil {
			return nil, err
		}
		d.entries, d.listed = entries, true
	}
	if count <= 0 {
		entries := d.entries
		d.entries = nil
		return entries, nil
	}
	if len(d.entries) == 0 {
		return nil, io.EOF
	}
	entries := d.entries[:min(count, len(d.entries))]
	d.entries = d.entries[len(entries):]
	return entries, nil
}

// ReadDir lists a directory of base, with the generated files of the directory in it.
func (o overlay) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(o.base, name)
	if err != nil {
		return nil, err
	}
	added, err := fs.ReadDir(o.generated, name)
	if errors.Is(err, fs.ErrNotExist) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range added {
		if entry.IsDir() {
			continue
		}
		entries = slices.DeleteFunc(entries, func(e fs.DirEntry) bool {
			return e.Name() == entry.Name()
		})
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})
	return entries, nil
}

// layer holds the values of app.tf.
type layer struct {
	Name, Title, VersionVariable, URLOutput, ReleasePattern string
	Values, Extras, Records                                 bool
	TimeoutSeconds                                          int
	Secrets                                                 []string
	Settings                                                []setting
	Route                                                   route
}

type setting struct {
	Variable, Path string
	Number         bool
	Min, Max       int64
}

type route struct {
	Service string
	Port    int
	Paths   []string
	// Timeout is the HCL value of the request timeout: a number of seconds, or null.
	Timeout string
	// Hostname and Wildcard ask for the host names <name>.<app_domain> and
	// *.<name>.<app_domain>.
	Hostname, Wildcard bool
}

// AppTF returns app.tf of the stacks of the layer of the pack.
func AppTF(app pack.Pack) ([]byte, error) {
	deploy := app.Deploy
	if deploy.Route == nil {
		return nil, fmt.Errorf("the pack %s has no deploy.route", app.Name)
	}
	values := layer{Name: app.Name, Title: app.Title, VersionVariable: app.VersionVariable,
		URLOutput: app.URLOutput, ReleasePattern: pack.ReleasePattern(), Values: deploy.Values != "",
		Extras: deploy.Extras != "", Records: app.HasRecords(),
		TimeoutSeconds: deploy.HelmTimeout(), Secrets: deploy.Secrets,
		Route: route{Service: deploy.Route.Service, Port: deploy.Route.Port,
			Paths: deploy.Route.Paths, Timeout: "null", Hostname: deploy.Route.Hostname,
			Wildcard: deploy.Route.Wildcard}}
	if deploy.Route.TimeoutSeconds != nil {
		values.Route.Timeout = strconv.Itoa(*deploy.Route.TimeoutSeconds)
	}
	for _, s := range deploy.Settings {
		entry := setting{Variable: s.Variable, Path: s.Value, Number: s.Type == pack.SettingNumber}
		if entry.Number {
			entry.Min, entry.Max = *s.Min, *s.Max
		}
		values.Settings = append(values.Settings, entry)
	}
	var out strings.Builder
	if err := render.Execute(&out, values); err != nil {
		return nil, fmt.Errorf("write %s for the pack %s: %w", AppFile, app.Name, err)
	}
	return []byte(Align(out.String())), nil
}

// LayerVars returns terraform.tfvars of a stack of the layer of the pack: the lines of the
// given file that set a variable of the core, then the version of the first release and
// the default of each setting. It keeps no other line.
func LayerVars(app pack.Pack, text []byte) ([]byte, error) {
	lines := []string{}
	core := pack.CoreVariables()
	for _, line := range strings.Split(string(text), "\n") {
		match := assignment.FindStringSubmatch(strings.TrimSpace(line))
		if match != nil && slices.Contains(core, match[1]) {
			lines = append(lines, match[1]+" = "+match[2])
		}
	}
	lines = append(lines, app.VersionVariable+" = "+quote(app.Releases.First))
	for _, s := range app.Deploy.Settings {
		value, err := settingDefault(s)
		if err != nil {
			return nil, err
		}
		lines = append(lines, s.Variable+" = "+value)
	}
	return []byte(Align(strings.Join(lines, "\n") + "\n")), nil
}

// settingDefault returns the default of a setting as an HCL value.
func settingDefault(s pack.Setting) (string, error) {
	raw := string(s.Default)
	if s.Type == pack.SettingBool {
		value, err := strconv.ParseBool(raw)
		return strconv.FormatBool(value), err
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return "", fmt.Errorf("the default of the setting %s is %q, not a whole number",
			s.Variable, raw)
	}
	return strconv.FormatInt(value, 10), nil
}

// PlatformVars returns terraform.tfvars of 40-platform with the name of the pack in the
// line app_name. The file must have that line exactly once.
func PlatformVars(app pack.Pack, text []byte) ([]byte, error) {
	if found := len(appNameLine.FindAll(text, -1)); found != 1 {
		return nil, fmt.Errorf("the file must set app_name = \"...\" on one line; it does %d "+
			"times", found)
	}
	return appNameLine.ReplaceAll(text, []byte("${1}"+quote(app.Name))), nil
}

// quote returns a string as an HCL string literal. The values of a checked pack have no
// character to escape; the escapes keep any other value a plain literal.
func quote(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`,
		"${", "$${", "%{", "%%{")
	return `"` + replacer.Replace(value) + `"`
}

// list returns a list of strings as an HCL tuple.
func list(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = quote(value)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
