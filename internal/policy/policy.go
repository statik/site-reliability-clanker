// Package policy turns a saved plan into a risk tier. The rules are Rego files in the
// runbooks repository. This package only runs them with the opa CLI.
package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
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

// Evaluate runs the tier policy over the plan JSON.
func Evaluate(ctx context.Context, in Input) (Decision, error) {
	planRaw, err := os.ReadFile(in.PlanJSONPath)
	if err != nil {
		return Decision{}, fmt.Errorf("read plan JSON %s for policy: %w", in.PlanJSONPath, err)
	}
	payload, err := json.Marshal(map[string]any{
		"kind": in.Kind, "layer": in.Layer, "plan": json.RawMessage(planRaw),
	})
	if err != nil {
		return Decision{}, fmt.Errorf("encode policy input: %w", err)
	}
	inputFile, err := os.CreateTemp("", "clanker-policy-*.json")
	if err != nil {
		return Decision{}, fmt.Errorf("create policy input file: %w", err)
	}
	defer os.Remove(inputFile.Name())
	if _, err := inputFile.Write(payload); err != nil {
		return Decision{}, fmt.Errorf("write policy input file: %w", err)
	}
	inputFile.Close()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "opa", "eval", "--format", "json",
		"--data", filepath.Clean(in.PolicyDir), "--input", inputFile.Name(),
		"{\"decision\": data.clanker.tier.decision, \"create_only\": data.clanker.tier.create_only}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return Decision{}, fmt.Errorf("opa eval with policies in %s: %w: %s", in.PolicyDir, err,
			stderr.String())
	}
	var result struct {
		Result []struct {
			Expressions []struct {
				Value struct {
					Decision   Decision `json:"decision"`
					CreateOnly bool     `json:"create_only"`
				} `json:"value"`
			} `json:"expressions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return Decision{}, fmt.Errorf("decode opa result: %w", err)
	}
	if len(result.Result) == 0 || len(result.Result[0].Expressions) == 0 {
		return Decision{}, fmt.Errorf("opa gave no decision for layer %s: check %s", in.Layer,
			in.PolicyDir)
	}
	value := result.Result[0].Expressions[0].Value
	decision := value.Decision
	decision.CreateOnly = value.CreateOnly
	if decision.Tier == "" {
		return Decision{}, fmt.Errorf("opa decision for layer %s has no tier", in.Layer)
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
