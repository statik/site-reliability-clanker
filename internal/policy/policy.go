// Package policy turns a saved plan into a risk tier. The rules are Rego files in the
// runbooks repository. This package evaluates them with the OPA library, in this process,
// so the control plane starts no program for a policy decision.
package policy

import (
	"context"
	"encoding/json"
	"fmt"
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
	Kind  string
	Layer string
	// PlanJSON is the JSON form of the plan.
	PlanJSON []byte
	// Modules are the Rego files of the runbooks repository: name to source.
	Modules map[string]string
}

const query = `{"decision": data.clanker.tier.decision, "create_only": data.clanker.tier.create_only}`

// Evaluate runs the tier policy over the plan JSON.
func Evaluate(ctx context.Context, in Input) (Decision, error) {
	var plan any
	if err := json.Unmarshal(in.PlanJSON, &plan); err != nil {
		return Decision{}, fmt.Errorf("decode plan JSON of layer %s for policy: %w", in.Layer, err)
	}
	options := []func(*rego.Rego){}
	for name, source := range in.Modules {
		// Test files are not rules.
		if strings.HasSuffix(name, "_test.rego") || !strings.HasSuffix(name, ".rego") {
			continue
		}
		options = append(options, rego.Module(name, source))
	}
	if len(options) == 0 {
		return Decision{}, fmt.Errorf("the policy for layer %s has no Rego file", in.Layer)
	}
	options = append(options, rego.Query(query),
		rego.Input(map[string]any{"kind": in.Kind, "layer": in.Layer, "plan": plan}))

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	results, err := rego.New(options...).Eval(ctx)
	if err != nil {
		return Decision{}, fmt.Errorf("evaluate the policies for layer %s: %w", in.Layer, err)
	}
	if len(results) == 0 || len(results[0].Expressions) == 0 {
		return Decision{}, fmt.Errorf("the policy gave no decision for layer %s", in.Layer)
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
