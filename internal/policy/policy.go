// Package policy turns a saved plan into a risk tier. The rules are Rego files in the
// runbooks repository. This package evaluates them with the OPA library, in this process,
// so the control plane starts no program for a policy decision.
package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-policy-agent/opa/v1/rego"
)

// Decision is the result of the tier policy for one plan.
type Decision struct {
	Tier       string   `json:"tier"`
	Rule       string   `json:"rule"`
	Reasons    []string `json:"reasons"`
	CreateOnly bool     `json:"create_only"`
}

// Input identifies the plan to evaluate.
type Input struct {
	Kind         string
	Layer        string
	PlanJSONPath string
	// PolicyDir is the directory with the Rego files, from the runbooks repository.
	PolicyDir string
}

const query = `{"decision": data.clanker.tier.decision, "create_only": data.clanker.tier.create_only}`

// modules reads the Rego files of the policy directory. Test files are not rules.
func modules(dir string) ([]func(*rego.Rego), error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.rego"))
	if err != nil {
		return nil, fmt.Errorf("list policies in %s: %w", dir, err)
	}
	options := []func(*rego.Rego){}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.rego") {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read policy %s: %w", path, err)
		}
		options = append(options, rego.Module(path, string(source)))
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("the policy directory %s has no Rego file", dir)
	}
	return options, nil
}

// Evaluate runs the tier policy over the plan JSON.
func Evaluate(ctx context.Context, in Input) (Decision, error) {
	planRaw, err := os.ReadFile(in.PlanJSONPath)
	if err != nil {
		return Decision{}, fmt.Errorf("read plan JSON %s for policy: %w", in.PlanJSONPath, err)
	}
	var plan any
	if err := json.Unmarshal(planRaw, &plan); err != nil {
		return Decision{}, fmt.Errorf("decode plan JSON %s for policy: %w", in.PlanJSONPath, err)
	}
	options, err := modules(in.PolicyDir)
	if err != nil {
		return Decision{}, err
	}
	options = append(options, rego.Query(query),
		rego.Input(map[string]any{"kind": in.Kind, "layer": in.Layer, "plan": plan}))

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	results, err := rego.New(options...).Eval(ctx)
	if err != nil {
		return Decision{}, fmt.Errorf("evaluate the policies in %s: %w", in.PolicyDir, err)
	}
	if len(results) == 0 || len(results[0].Expressions) == 0 {
		return Decision{}, fmt.Errorf("the policy gave no decision for layer %s: check %s",
			in.Layer, in.PolicyDir)
	}
	// The value is generic JSON. A round trip through JSON gives the typed decision.
	raw, err := json.Marshal(results[0].Expressions[0].Value)
	if err != nil {
		return Decision{}, fmt.Errorf("encode the policy result: %w", err)
	}
	var value struct {
		Decision   Decision `json:"decision"`
		CreateOnly bool     `json:"create_only"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return Decision{}, fmt.Errorf("decode the policy result: %w", err)
	}
	decision := value.Decision
	decision.CreateOnly = value.CreateOnly
	if decision.Tier == "" {
		return Decision{}, fmt.Errorf("the policy decision for layer %s has no tier", in.Layer)
	}
	return decision, nil
}

// MaxTier returns the higher of two tiers. Tiers order as T0 < T1 < T2 < T3.
func MaxTier(a, b string) string {
	if a > b {
		return a
	}
	return b
}

// ApproversRequired is the number of distinct approvers that a tier needs.
func ApproversRequired(tier string) int {
	switch tier {
	case "T3":
		return 2
	case "T2":
		return 1
	default:
		return 0
	}
}
