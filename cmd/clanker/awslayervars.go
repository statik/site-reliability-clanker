package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/scrub"
)

// layerVariablesEnv holds the inputs of the layers on AWS that belong to one account, such
// as existing_network_id, image_registry or instance_tags: a JSON object from the name of a
// variable to its value. A value is a string, a list of strings or a map of strings.
// Parameter Store can give the setting like the other settings. The values can be personal
// or tell about the account, so the scrubber learns each one.
const layerVariablesEnv = "CLANKER_AWS_LAYER_VARIABLES"

// coreInputs are the variables that the core gives the layers itself. The setting cannot
// give them.
var coreInputs = []string{"env_name", "state_location", "region", "admin_token",
	"collector_endpoint", "collector_token"}

var (
	variableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	// declaredVariable finds the name of each variable block of a stack.
	declaredVariable = regexp.MustCompile(`(?m)^variable\s+"([^"]+)"`)
	// assignedVariable finds the name of each value of a terraform.tfvars file. A value
	// inside a map or a list has an indent.
	assignedVariable = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_-]*)\s*=`)
)

// layerVariables reads the setting and returns the variables of the jobs of the layers, as
// TF_VAR_<name> to the value. Without the setting it returns none. It gives the scrubber
// every value before any plan exists. A name that no layer declares, that a
// terraform.tfvars file of a layer sets (the file wins over TF_VAR_), or that the core sets
// itself stops the start.
func layerVariables(source fs.FS, cloud string, getenv func(string) string) (
	map[string]string, error) {
	text := getenv(layerVariablesEnv)
	if text == "" {
		return nil, nil
	}
	variables, values, err := parseLayerVariables(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", layerVariablesEnv, err)
	}
	if err := checkLayerVariables(source, cloud, variables); err != nil {
		return nil, fmt.Errorf("%s: %w", layerVariablesEnv, err)
	}
	short := []string{}
	for name, list := range values {
		for _, value := range list {
			scrub.Add(value)
			if len(value) < scrub.MinKnownLength {
				short = append(short, name)
			}
		}
	}
	if len(short) > 0 {
		slog.Warn("values of these layer variables are shorter than the scrubber needs: a "+
			"model or a notification can see them", "variables", slices.Compact(slices.Sorted(
			slices.Values(short))), "min_length", scrub.MinKnownLength)
	}
	jobVariables := map[string]string{}
	for name, value := range variables {
		jobVariables["TF_VAR_"+name] = value
	}
	slog.Info("the layers get these variables of the account", "cloud", cloud,
		"variables", slices.Sorted(maps.Keys(variables)))
	return jobVariables, nil
}

// parseLayerVariables reads the JSON object of the setting. It returns the text of each
// variable as OpenTofu reads it from TF_VAR_<name>: a string as it is, a list or a map in
// JSON, which is also HCL. It also returns, for each variable, the strings in its value:
// the string, the items of the list or the values of the map. No error holds a value.
func parseLayerVariables(text string) (map[string]string, map[string][]string, error) {
	raw := map[string]json.RawMessage{}
	decoder := json.NewDecoder(strings.NewReader(text))
	if err := decoder.Decode(&raw); err != nil {
		return nil, nil, fmt.Errorf("it is not a JSON object from the name of a variable to "+
			"its value: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, nil, errors.New("it has more text after its JSON object")
	}
	variables, values := map[string]string{}, map[string][]string{}
	for name, value := range raw {
		if !variableName.MatchString(name) {
			return nil, nil, fmt.Errorf("%q is not the name of an OpenTofu variable", name)
		}
		if slices.Contains(coreInputs, name) {
			return nil, nil, fmt.Errorf("the core sets the variable %s itself: remove it. The "+
				"core sets %s", name, strings.Join(coreInputs, ", "))
		}
		textValue, items, err := variableValue(value)
		if err != nil {
			return nil, nil, fmt.Errorf("the value of %s: %w", name, err)
		}
		variables[name], values[name] = textValue, items
	}
	return variables, values, nil
}

// variableValue returns the text of one value for TF_VAR_ and the strings in it.
func variableValue(raw json.RawMessage) (string, []string, error) {
	var text string
	if string(raw) == "null" {
		return "", nil, errors.New("it is null: give a value, or leave the variable out")
	}
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, []string{text}, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil && list != nil {
		return hclValue(list, list)
	}
	var object map[string]string
	if err := json.Unmarshal(raw, &object); err == nil && object != nil {
		return hclValue(object, slices.Collect(maps.Values(object)))
	}
	return "", nil, fmt.Errorf("it is not a string, a list of strings or a map of strings. " +
		"Write a number or a bool as a string, such as \"2\"")
}

// hclValue writes a list or a map as JSON, which OpenTofu reads as an HCL expression. In an
// HCL string, ${ and %{ start a template, so a value with them is refused.
func hclValue(value any, items []string) (string, []string, error) {
	for _, item := range items {
		if strings.Contains(item, "${") || strings.Contains(item, "%{") {
			return "", nil, fmt.Errorf("a string in it has ${ or %%{, which OpenTofu reads " +
				"as a template")
		}
	}
	var text bytes.Buffer
	encoder := json.NewEncoder(&text)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", nil, fmt.Errorf("encode it: %w", err)
	}
	return strings.TrimSpace(text.String()), items, nil
}

// checkLayerVariables refuses a variable that no layer of the cloud above 00-bootstrap
// declares, and one that a terraform.tfvars file of such a layer sets.
func checkLayerVariables(source fs.FS, cloud string, variables map[string]string) error {
	declared, assigned := map[string]bool{}, map[string]string{}
	for _, layer := range orchestrator.Layers[1:] {
		stack := path.Join("platform/stacks", cloud, layer)
		files, err := fs.Glob(source, stack+"/*.tf")
		if err != nil {
			return fmt.Errorf("list the files of %s: %w", stack, err)
		}
		for _, file := range files {
			content, err := fs.ReadFile(source, file)
			if err != nil {
				return fmt.Errorf("read %s: %w", file, err)
			}
			for _, match := range declaredVariable.FindAllSubmatch(content, -1) {
				declared[string(match[1])] = true
			}
		}
		tfvars := stack + "/terraform.tfvars"
		content, err := fs.ReadFile(source, tfvars)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("read %s: %w", tfvars, err)
		}
		for _, match := range assignedVariable.FindAllSubmatch(content, -1) {
			assigned[string(match[1])] = tfvars
		}
	}
	for _, name := range slices.Sorted(maps.Keys(variables)) {
		if !declared[name] {
			return fmt.Errorf("no layer of %s above %s declares the variable %s", cloud,
				orchestrator.Layers[0], name)
		}
		if file, found := assigned[name]; found {
			return fmt.Errorf("%s sets the variable %s, and the file wins over TF_VAR_%s: "+
				"remove %s from the setting, or change the file with a pull request", file,
				name, name, name)
		}
	}
	return nil
}
