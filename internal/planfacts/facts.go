// Package planfacts reads the facts of an OpenTofu plan from its JSON form: what changes,
// and how. The control plane shows these facts and gives them to the policy. They come
// from the plan, never from a language model.
package planfacts

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/scrub"
)

// ResourceChange is one changed resource, taken from the plan JSON.
type ResourceChange struct {
	Address string   `json:"address"`
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Action  string   `json:"action"`
	Details []string `json:"details"`
}

// Facts are the numbers and names on the approval page. They come from the plan JSON only.
// No model output can change them.
type Facts struct {
	Add       int              `json:"add"`
	Change    int              `json:"change"`
	Destroy   int              `json:"destroy"`
	Replace   int              `json:"replace"`
	Resources []ResourceChange `json:"resources"`
	Outputs   []string         `json:"outputs"`
	// Warnings are for the approver. The core writes them from the plan and the code of
	// the change, for example when the plan lowers the version of the application across
	// a release that changed the schema of its database.
	Warnings []string `json:"warnings,omitempty"`
}

// HasChanges reports whether the plan changes a resource or an output. A plan that changes
// only outputs must be applied too: the apply writes the outputs into the state, where the
// layers above read them, and the control plane stores them for the plan of the next layer.
func (f Facts) HasChanges() bool {
	return f.Add+f.Change+f.Destroy+f.Replace > 0 || len(f.Outputs) > 0
}

type planJSON struct {
	ResourceChanges []struct {
		Address string `json:"address"`
		Type    string `json:"type"`
		Name    string `json:"name"`
		Change  struct {
			Actions         []string       `json:"actions"`
			Before          map[string]any `json:"before"`
			After           map[string]any `json:"after"`
			BeforeSensitive any            `json:"before_sensitive"`
			AfterSensitive  any            `json:"after_sensitive"`
		} `json:"change"`
	} `json:"resource_changes"`
	OutputChanges map[string]struct {
		Actions []string `json:"actions"`
	} `json:"output_changes"`
}

func actionName(actions []string) string {
	joined := strings.Join(actions, ",")
	switch joined {
	case "create":
		return "create"
	case "update":
		return "update"
	case "delete":
		return "destroy"
	case "delete,create", "create,delete":
		return "replace"
	default:
		return ""
	}
}

// Parse extracts the facts from the JSON form of a plan.
func Parse(raw []byte) (Facts, error) {
	var plan planJSON
	if err := json.Unmarshal(raw, &plan); err != nil {
		return Facts{}, fmt.Errorf("decode plan JSON: %w", err)
	}
	facts := Facts{Resources: []ResourceChange{}, Outputs: []string{}}
	for _, rc := range plan.ResourceChanges {
		action := actionName(rc.Change.Actions)
		if action == "" {
			continue
		}
		switch action {
		case "create":
			facts.Add++
		case "update":
			facts.Change++
		case "destroy":
			facts.Destroy++
		case "replace":
			facts.Replace++
		}
		change := ResourceChange{Address: rc.Address, Type: rc.Type, Name: rc.Name,
			Action: action, Details: []string{}}
		registerSensitive(rc.Change.Before, rc.Change.BeforeSensitive)
		registerSensitive(rc.Change.After, rc.Change.AfterSensitive)
		if action == "update" || action == "replace" {
			sensitive := sensitivePaths(rc.Change.BeforeSensitive, rc.Change.AfterSensitive)
			change.Details = diffAttributes(rc.Change.Before, rc.Change.After, sensitive)
		}
		facts.Resources = append(facts.Resources, change)
	}
	for name, oc := range plan.OutputChanges {
		if action := actionName(oc.Actions); action != "" {
			facts.Outputs = append(facts.Outputs, name)
		}
	}
	sort.Strings(facts.Outputs)
	return facts, nil
}

// OutputChange returns the value of a string output before and after the plan. Both are
// empty when the plan does not update the output, or when a value is not a string.
func OutputChange(raw []byte, name string) (before, after string, err error) {
	var plan struct {
		OutputChanges map[string]struct {
			Actions []string `json:"actions"`
			Before  any      `json:"before"`
			After   any      `json:"after"`
		} `json:"output_changes"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		return "", "", fmt.Errorf("decode plan JSON: %w", err)
	}
	change := plan.OutputChanges[name]
	before, beforeOK := change.Before.(string)
	after, afterOK := change.After.(string)
	if actionName(change.Actions) != "update" || !beforeOK || !afterOK {
		return "", "", nil
	}
	return before, after, nil
}

// flatten turns a nested value into leaf paths. List indexes are left out of the path, so
// "spec[0].template[0]" reads as "spec.template". A multi-line string, as in the values of a
// Helm release, becomes one leaf for each YAML line.
func flatten(prefix string, v any, into map[string]string) {
	switch val := v.(type) {
	case map[string]any:
		for k, child := range val {
			flatten(strings.TrimPrefix(prefix+"."+k, "."), child, into)
		}
	case []any:
		for i, child := range val {
			childPrefix := prefix
			if i > 0 {
				childPrefix = fmt.Sprintf("%s[%d]", prefix, i)
			}
			flatten(childPrefix, child, into)
		}
	case string:
		if !strings.Contains(val, "\n") {
			into[prefix] = val
			return
		}
		for path, value := range yamlLeaves(val) {
			into[prefix+"."+path] = value
		}
	case nil:
	default:
		raw, _ := json.Marshal(val)
		into[prefix] = string(raw)
	}
}

// yamlLeaves reads the simple block YAML that yamlencode writes and returns leaf paths.
func yamlLeaves(text string) map[string]string {
	leaves := map[string]string{}
	parents := []string{}
	indents := []int{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.ReplaceAll(line, `"`, "")
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(trimmed)
		for len(indents) > 0 && indents[len(indents)-1] >= indent {
			indents = indents[:len(indents)-1]
			parents = parents[:len(parents)-1]
		}
		key, value, hasColon := strings.Cut(trimmed, ":")
		if !hasColon {
			continue
		}
		if strings.TrimSpace(value) == "" {
			parents = append(parents, key)
			indents = append(indents, indent)
			continue
		}
		leaves[strings.Join(append(append([]string{}, parents...), key), ".")] =
			strings.TrimSpace(value)
	}
	return leaves
}

// registerSensitive gives the scrubber each string that the plan marks sensitive. The
// details of a plan never show such a value, but the same value can be in a log line or
// in a file that a role reads later. The mark has the structure of the value, with true
// in the place of a sensitive part.
func registerSensitive(value, mark any) {
	switch m := mark.(type) {
	case bool:
		if m {
			registerStrings(value)
		}
	case map[string]any:
		fields, _ := value.(map[string]any)
		for name, child := range m {
			registerSensitive(fields[name], child)
		}
	case []any:
		items, _ := value.([]any)
		for i := range min(len(m), len(items)) {
			registerSensitive(items[i], m[i])
		}
	}
}

// registerStrings registers every string below a value. A block with a name and a value,
// such as an entry of set_sensitive of a Helm release, has one mark for both. Its name is
// the name of a setting and is in the chart and in the code, so only its value is a secret.
func registerStrings(value any) {
	switch v := value.(type) {
	case string:
		scrub.Add(v)
	case map[string]any:
		if _, named := v["name"].(string); named {
			v = map[string]any{"value": v["value"]}
		}
		for _, child := range v {
			registerStrings(child)
		}
	case []any:
		for _, child := range v {
			registerStrings(child)
		}
	}
}

func sensitivePaths(values ...any) map[string]bool {
	paths := map[string]bool{}
	for _, v := range values {
		flat := map[string]string{}
		flatten("", v, flat)
		for path, flag := range flat {
			if flag == "true" {
				paths[path] = true
			}
		}
	}
	return paths
}

func isSensitive(path string, sensitive map[string]bool) bool {
	for prefix := range sensitive {
		if path == prefix || strings.HasPrefix(path, prefix+".") {
			return true
		}
	}
	return false
}

const maxDetails = 12

func diffAttributes(before, after map[string]any, sensitive map[string]bool) []string {
	flatBefore, flatAfter := map[string]string{}, map[string]string{}
	flatten("", map[string]any(before), flatBefore)
	flatten("", map[string]any(after), flatAfter)
	paths := map[string]bool{}
	for p := range flatBefore {
		paths[p] = true
	}
	for p := range flatAfter {
		paths[p] = true
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	details := []string{}
	for _, p := range sorted {
		b, hasBefore := flatBefore[p]
		a, hasAfter := flatAfter[p]
		if b == a || strings.HasPrefix(p, "metadata.") || p == "id" {
			continue
		}
		if len(details) == maxDetails {
			details = append(details, "more attributes change; see the raw plan")
			break
		}
		switch {
		case isSensitive(p, sensitive):
			details = append(details, p+": (sensitive value changes)")
		case !hasBefore:
			details = append(details, fmt.Sprintf("%s: (not set) → %s", p, short(a)))
		case !hasAfter:
			details = append(details, fmt.Sprintf("%s: %s → (known after apply)", p, short(b)))
		default:
			details = append(details, fmt.Sprintf("%s: %s → %s", p, short(b), short(a)))
		}
	}
	return details
}

func short(text string) string {
	if len(text) > 80 {
		return text[:77] + "..."
	}
	return text
}
