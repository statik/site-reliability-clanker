// Package runbook has the definition of a runbook: what it does, the tier it needs, the
// inputs it takes and the outputs it gives. A definition is a JSON file beside the script
// in the runbooks repository. The control plane checks each request against the
// definition before it creates a work item or a grant, so a caller, human or model, can
// start only a reviewed runbook with values that the definition allows.
//
// The package starts no program and reads no file.
package runbook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Types of an input or an output.
const (
	TypeString  = "string"
	TypeInteger = "integer"
)

// StandingTier is the only tier that a standing approval covers.
const StandingTier = "T1"

// maxOutputLength is the most characters that one output value can have.
const maxOutputLength = 500

var (
	name      = regexp.MustCompile(`^[a-z][a-z0-9-]{1,40}$`)
	fieldName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}$`)
	// A title is one to five words, with no punctuation that can make a sentence.
	title = regexp.MustCompile(
		`^[A-Za-z][A-Za-z0-9-]{0,19}( [A-Za-z0-9][A-Za-z0-9-]{0,19}){0,4}$`)
	tiers = []string{"T0", "T1", "T2", "T3"}
)

// MitigationInput is the one input of a runbook that is a mitigation for an incident: the
// name of a component of the application.
const MitigationInput = "component"

// ValidName reports whether text can be the name of a runbook.
func ValidName(text string) bool { return name.MatchString(text) }

// Field describes one input or one output.
type Field struct {
	// Type is TypeString or TypeInteger. An input is always a string.
	Type        string `json:"type"`
	Description string `json:"description"`
	// Values are the only values that an input can have. An output has none.
	Values []string `json:"values,omitempty"`
}

// Definition describes one runbook. Its steps are the script with the same name.
type Definition struct {
	// Name comes from the name of the file, not from its content.
	Name string `json:"name"`
	// Title names the action in the title of a work item, in one to five words. A mitigation
	// for an incident must have one.
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	// Tier is the risk tier that a run needs.
	Tier string `json:"tier"`
	// IncidentMitigation means that the Investigator can propose the runbook for an incident.
	// The Investigator gives one parameter, the name of a component, so such a runbook must
	// have a title and exactly the input MitigationInput.
	IncidentMitigation bool `json:"incident_mitigation,omitempty"`
	// Inputs are the parameters. A request must give each one, and no other.
	Inputs map[string]Field `json:"inputs,omitempty"`
	// Outputs are the values that the script reports. A run must report each one, and no
	// other.
	Outputs map[string]Field `json:"outputs,omitempty"`
}

// Parse reads the definition of the runbook with the given name and checks it.
func Parse(runbookName string, raw []byte) (Definition, error) {
	if !ValidName(runbookName) {
		return Definition{}, fmt.Errorf("the runbook name %q is not valid", runbookName)
	}
	var d Definition
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return Definition{}, fmt.Errorf("decode the definition of runbook %s: %w", runbookName,
			err)
	}
	if d.Name != "" {
		return Definition{}, fmt.Errorf("the definition of runbook %s has a name: the name "+
			"comes from the name of the file", runbookName)
	}
	d.Name = runbookName
	if err := d.check(); err != nil {
		return Definition{}, fmt.Errorf("the definition of runbook %s is not valid: %w",
			runbookName, err)
	}
	return d, nil
}

func (d Definition) check() error {
	if err := d.checkText(); err != nil {
		return err
	}
	for _, field := range sortedNames(d.Inputs) {
		if err := checkInput(d.Inputs[field]); err != nil {
			return fmt.Errorf("input %q: %w", field, err)
		}
	}
	for _, field := range sortedNames(d.Outputs) {
		if err := checkOutput(d.Outputs[field]); err != nil {
			return fmt.Errorf("output %q: %w", field, err)
		}
	}
	if err := checkNames(d.Inputs, d.Outputs); err != nil {
		return err
	}
	return d.checkMitigation()
}

func (d Definition) checkText() error {
	if strings.TrimSpace(d.Description) == "" {
		return errors.New("it has no description")
	}
	if !slices.Contains(tiers, d.Tier) {
		return fmt.Errorf("the tier %q is not one of %s", d.Tier, strings.Join(tiers, ", "))
	}
	if d.Title != "" && !title.MatchString(d.Title) {
		return fmt.Errorf("the title %q is not one to five words: it must match %s", d.Title,
			title)
	}
	return nil
}

// checkMitigation checks a runbook that the Investigator can propose. The Investigator
// gives the name of a component and nothing else, and the title of the runbook goes into
// the title of the work item.
func (d Definition) checkMitigation() error {
	if !d.IncidentMitigation {
		return nil
	}
	if d.Title == "" {
		return errors.New("a mitigation for an incident must have a title")
	}
	if _, declared := d.Inputs[MitigationInput]; !declared {
		return fmt.Errorf("a mitigation for an incident must have the input %q",
			MitigationInput)
	}
	if len(d.Inputs) != 1 {
		return fmt.Errorf("a mitigation for an incident has the input %q and no other; its "+
			"inputs are %s", MitigationInput, list(sortedNames(d.Inputs)))
	}
	return nil
}

func checkNames(groups ...map[string]Field) error {
	for _, group := range groups {
		for _, field := range sortedNames(group) {
			if !fieldName.MatchString(field) {
				return fmt.Errorf("the name %q is not valid: use lower-case letters, digits "+
					"and \"_\"", field)
			}
			if strings.TrimSpace(group[field].Description) == "" {
				return fmt.Errorf("%q has no description", field)
			}
		}
	}
	return nil
}

// checkInput makes sure that no input is free text: an input is a string that names its
// values.
func checkInput(f Field) error {
	if f.Type != TypeString {
		return fmt.Errorf("the type %q is not %s: an input is a string with a list of values",
			f.Type, TypeString)
	}
	if len(f.Values) == 0 {
		return errors.New("an input must list its values")
	}
	if slices.Contains(f.Values, "") {
		return errors.New("an empty value is not permitted")
	}
	return nil
}

func checkOutput(f Field) error {
	if f.Type != TypeString && f.Type != TypeInteger {
		return fmt.Errorf("the type %q is not %s or %s", f.Type, TypeString, TypeInteger)
	}
	if len(f.Values) > 0 {
		return errors.New("an output has no list of values")
	}
	return nil
}

func sortedNames[V any](fields map[string]V) []string {
	names := make([]string, 0, len(fields))
	for field := range fields {
		names = append(names, field)
	}
	sort.Strings(names)
	return names
}

// CheckInputs checks the parameters of a request. Each declared input must be there with a
// permitted value, and a parameter that the definition does not declare is refused.
func (d Definition) CheckInputs(params map[string]string) error {
	for _, param := range sortedNames(params) {
		if _, declared := d.Inputs[param]; !declared {
			return fmt.Errorf("runbook %s has no input %q: its inputs are %s", d.Name, param,
				list(sortedNames(d.Inputs)))
		}
	}
	for _, input := range sortedNames(d.Inputs) {
		value, given := params[input]
		if !given {
			return fmt.Errorf("runbook %s needs the input %q", d.Name, input)
		}
		if permitted := d.Inputs[input].Values; !slices.Contains(permitted, value) {
			return fmt.Errorf("input %q of runbook %s: %q is not one of %s", input, d.Name,
				value, list(permitted))
		}
	}
	return nil
}

func list(items []string) string {
	if len(items) == 0 {
		return "(none)"
	}
	return strings.Join(items, ", ")
}

// CheckOutputs checks what a run reported and returns the outputs with their declared
// types: a string or an int64. Each declared output must be there, and an output that the
// definition does not declare is refused.
func (d Definition) CheckOutputs(reported map[string]any) (map[string]any, error) {
	for _, output := range sortedNames(reported) {
		if _, declared := d.Outputs[output]; !declared {
			return nil, fmt.Errorf("runbook %s reported the output %q, which its definition "+
				"does not declare", d.Name, output)
		}
	}
	outputs := map[string]any{}
	for _, output := range sortedNames(d.Outputs) {
		raw, given := reported[output]
		if !given {
			return nil, fmt.Errorf("runbook %s did not report its output %q", d.Name, output)
		}
		value, err := typedOutput(d.Outputs[output], raw)
		if err != nil {
			return nil, fmt.Errorf("output %q of runbook %s: %w", output, d.Name, err)
		}
		outputs[output] = value
	}
	return outputs, nil
}

func typedOutput(f Field, raw any) (any, error) {
	text, isText := raw.(string)
	if !isText {
		return nil, fmt.Errorf("the runner sent %T, not text", raw)
	}
	if len(text) > maxOutputLength {
		return nil, fmt.Errorf("the value has %d characters, more than %d", len(text),
			maxOutputLength)
	}
	if f.Type == TypeString {
		return text, nil
	}
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%q is not an integer", text)
	}
	return number, nil
}
