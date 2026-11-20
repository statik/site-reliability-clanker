package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/runbook"
)

// The limits of the optional parts of a pack. A pack comes from outside the core, so no
// part of it can be large without a bound.
const (
	maxRunbooks      = 50
	maxPartFileBytes = 256 << 10
	maxEvalFiles     = 20
	maxEvalSamples   = 200
)

// EvalSchema is the version of the format of an eval case file that this code reads.
const EvalSchema = 2

var (
	evalTaskPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}$`)
	evalSamplePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,60}$`)
)

// Check is the result of one check of a pack, for a person or a program to read.
type Check struct {
	// Name says what was checked. Detail says what was found. Problem is empty when the
	// check passed.
	Name, Detail, Problem string
}

// Files returns the files of the pack, with the directory of the pack as the root.
func (p Pack) Files() fs.FS { return p.files }

// ReadReleaseNote returns the note of a release of the application.
func (p Pack) ReadReleaseNote(version string) ([]byte, error) {
	return fs.ReadFile(p.files, p.ReleaseNote(version))
}

// ChartFiles returns the files of the deployment chart.
func (p Pack) ChartFiles() (fs.FS, error) { return fs.Sub(p.files, p.Deploy.Chart) }

// RunbookFiles returns the runbooks of the pack: one definition `<name>.json` and one
// script `<name>.sh` for each. It returns nil when the pack has no runbooks.
func (p Pack) RunbookFiles() (fs.FS, error) {
	if p.Runbooks == "" {
		return nil, nil
	}
	return fs.Sub(p.files, p.Runbooks)
}

// CheckParts checks the optional parts of the pack, which are files: the runbooks and the
// eval cases. It returns one result for each part that the manifest names.
func (p Pack) CheckParts() []Check {
	checks, _ := p.checkParts()
	return checks
}

// checkParts returns the results of CheckParts and the names of the runbooks that are
// mitigations for an incident, in the order of the names.
func (p Pack) checkParts() ([]Check, []string) {
	checks, mitigations := []Check{}, []string{}
	if p.Runbooks == "" && p.Incident != nil {
		checks = append(checks, Check{Name: "runbooks", Problem: "the pack has an incident " +
			"runbook and no directory of runbooks"})
	}
	if p.Runbooks != "" {
		found, err := p.checkRunbooks()
		checks = append(checks, result("runbooks", strings.Join(found.names, ", "), err))
		mitigations = sortedKeys(found.mitigations)
	}
	if p.Evals != "" {
		tasks, err := p.checkEvals()
		checks = append(checks, result("eval cases", strings.Join(tasks, ", "), err))
	}
	return checks, mitigations
}

// runbooks are the runbooks of a pack that passed the checks.
type runbooks struct {
	names []string
	// mitigations are the definitions that are mitigations for an incident, by name.
	mitigations map[string]runbook.Definition
}

func sortedKeys(definitions map[string]runbook.Definition) []string {
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func result(name, detail string, err error) Check {
	if err != nil {
		return Check{Name: name, Problem: err.Error()}
	}
	return Check{Name: name, Detail: detail}
}

// partFiles returns the names of the files of one optional directory. A part has files
// only: with no directory below it, a runbook cannot get a path outside the directory of
// the jobs, and a pack cannot bring a policy.
func (p Pack) partFiles(dir string) ([]string, error) {
	entries, err := fs.ReadDir(p.files, dir)
	if err != nil {
		return nil, fmt.Errorf("the pack does not have the directory %s", dir)
	}
	names := []string{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("read %s/%s: %w", dir, entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s/%s is not a file; the directory can hold files only",
				dir, entry.Name())
		}
		if info.Size() > maxPartFileBytes {
			return nil, fmt.Errorf("%s/%s has %d bytes; the limit is %d", dir, entry.Name(),
				info.Size(), maxPartFileBytes)
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

// checkRunbooks reads each definition with the code that reads it at run time, and
// returns the runbooks.
func (p Pack) checkRunbooks() (runbooks, error) {
	files, err := p.partFiles(p.Runbooks)
	if err != nil {
		return runbooks{}, err
	}
	found := runbooks{names: []string{}, mitigations: map[string]runbook.Definition{}}
	for _, file := range files {
		if err := p.addRunbook(&found, files, file); err != nil {
			return runbooks{}, err
		}
	}
	if err := p.checkIncidentRunbook(found.mitigations); err != nil {
		return runbooks{}, err
	}
	if len(found.names) == 0 || len(found.names) > maxRunbooks {
		return runbooks{}, fmt.Errorf("%s has %d runbooks; a pack can have 1 to %d", p.Runbooks,
			len(found.names), maxRunbooks)
	}
	return found, nil
}

// addRunbook checks one file of the directory of the runbooks. A definition adds its
// runbook to found. The script of a definition adds nothing.
func (p Pack) addRunbook(found *runbooks, files []string, file string) error {
	name, isDefinition := strings.CutSuffix(file, ".json")
	script, isScript := strings.CutSuffix(file, ".sh")
	switch {
	case isScript && slices.Contains(files, script+".json"):
		return nil
	case !isDefinition:
		return fmt.Errorf("%s/%s is not a definition (.json) and not the script (.sh) of one",
			p.Runbooks, file)
	case !slices.Contains(files, name+".sh"):
		return fmt.Errorf("runbook %s has a definition and no script %s.sh", name, name)
	}
	definition, err := p.readRunbook(name)
	if err != nil {
		return err
	}
	if definition.IncidentMitigation {
		found.mitigations[name] = definition
	}
	found.names = append(found.names, name)
	return nil
}

// readRunbook reads and checks the definition of one runbook of the pack.
func (p Pack) readRunbook(name string) (runbook.Definition, error) {
	raw, err := fs.ReadFile(p.files, path.Join(p.Runbooks, name+".json"))
	if err != nil {
		return runbook.Definition{}, err
	}
	definition, err := runbook.Parse(name, raw)
	if err != nil {
		return runbook.Definition{}, err
	}
	return definition, p.checkMitigation(definition)
}

// checkMitigation checks a runbook that the Investigator can propose: it can name only
// components of the pack.
func (p Pack) checkMitigation(definition runbook.Definition) error {
	if !definition.IncidentMitigation {
		return nil
	}
	for _, value := range definition.Inputs[runbook.MitigationInput].Values {
		if _, ok := p.Component(value); !ok {
			return fmt.Errorf("runbook %s permits the component %q, which is not a component "+
				"of the pack", definition.Name, value)
		}
	}
	return nil
}

// checkIncidentRunbook checks the mitigation that the pack names for an incident with no
// diagnosis of a model: it must be a mitigation of the pack, and it must permit the
// component of the incident. A pack that fails this check would send each such incident
// to a person with no word about the cause.
func (p Pack) checkIncidentRunbook(mitigations map[string]runbook.Definition) error {
	if p.Incident == nil {
		return nil
	}
	definition, ok := mitigations[p.Incident.Runbook]
	if !ok {
		return fmt.Errorf("incident.runbook %q is not a runbook of the pack that is a "+
			"mitigation for an incident", p.Incident.Runbook)
	}
	permitted := definition.Inputs[runbook.MitigationInput].Values
	if !slices.Contains(permitted, p.Incident.Component) {
		return fmt.Errorf("incident.runbook %s does not permit incident.component %q",
			definition.Name, p.Incident.Component)
	}
	return nil
}

// evalFile is the part of an eval case file that every task has. The eval suite reads
// the other fields of a sample; they are data for that suite and never reach the core.
type evalFile struct {
	Schema  int               `json:"schema"`
	Task    string            `json:"task"`
	Samples []json.RawMessage `json:"samples"`
}

// checkEvals checks the form of each eval case file and returns the names of the tasks.
func (p Pack) checkEvals() ([]string, error) {
	files, err := p.partFiles(p.Evals)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 || len(files) > maxEvalFiles {
		return nil, fmt.Errorf("%s has %d files; a pack can have 1 to %d", p.Evals, len(files),
			maxEvalFiles)
	}
	tasks := []string{}
	for _, file := range files {
		task, err := p.checkEvalFile(file)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", p.Evals, file, err)
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (p Pack) checkEvalFile(file string) (string, error) {
	task, ok := strings.CutSuffix(file, ".json")
	if !ok || !evalTaskPattern.MatchString(task) {
		return "", fmt.Errorf("the name must be <task>.json, and the task must match %s",
			evalTaskPattern)
	}
	raw, err := fs.ReadFile(p.files, path.Join(p.Evals, file))
	if err != nil {
		return "", err
	}
	var cases evalFile
	if err := json.Unmarshal(raw, &cases); err != nil {
		return "", fmt.Errorf("not a correct eval case file: %w", err)
	}
	if cases.Schema != EvalSchema || cases.Task != task {
		return "", fmt.Errorf("schema must be %d and task must be %q", EvalSchema, task)
	}
	if len(cases.Samples) == 0 || len(cases.Samples) > maxEvalSamples {
		return "", fmt.Errorf("%d samples; a file can have 1 to %d", len(cases.Samples),
			maxEvalSamples)
	}
	return task, checkSampleIDs(cases.Samples)
}

// checkSampleIDs checks that each sample is an object with its own id.
func checkSampleIDs(samples []json.RawMessage) error {
	seen := map[string]bool{}
	for index, raw := range samples {
		var sample struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &sample); err != nil {
			return fmt.Errorf("sample %d is not an object: %w", index+1, err)
		}
		if !evalSamplePattern.MatchString(sample.ID) || seen[sample.ID] {
			return fmt.Errorf("sample %d has the id %q; an id must match %s and be in the "+
				"file one time", index+1, sample.ID, evalSamplePattern)
		}
		seen[sample.ID] = true
	}
	return nil
}

// Hash returns the SHA-256 of the files of a tree: for each file in the order of the
// paths, its path, whether a program can run it, its length and its content. Two trees
// with the same hash have the same files.
func Hash(files fs.FS) (string, error) {
	paths := []string{}
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			paths = append(paths, name)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("list the files of the pack: %w", err)
	}
	sort.Strings(paths)
	sum := sha256.New()
	for _, name := range paths {
		info, err := fs.Stat(files, name)
		if err != nil {
			return "", err
		}
		content, err := fs.ReadFile(files, name)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(sum, "%s\x00%t\x00%d\x00", name, info.Mode()&0o111 != 0, len(content))
		sum.Write(content)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
