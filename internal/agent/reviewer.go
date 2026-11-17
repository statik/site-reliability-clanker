package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/orchestrator"
)

// maxReviewDiff is how much of the diff of a change the Reviewer reads.
const maxReviewDiff = 12000

// reviewInput gathers what the Reviewer judges: the work item, the plans with the decision
// of the policy engine for each, and the diff of the pull request when there is one.
func (a *Agent) reviewInput(wi orchestrator.WorkItem, plans []orchestrator.Plan,
	policyTier string) map[string]any {
	input := map[string]any{"title": wi.Title, "kind": wi.Kind, "env": wi.Env,
		"layers": wi.Layers, "trigger": wi.Trigger, "policy_tier": policyTier, "plans": plans}
	if wi.PRID == 0 || wi.BaseCommit == "" || wi.BaseCommit == wi.Commit {
		return input
	}
	// A diff that cannot be read is not a reason to skip the review of the plans.
	if diff, err := a.Forge.Diff(orchestrator.PlatformRepo, wi.BaseCommit, wi.Commit); err == nil {
		input["diff"] = truncate(diff, maxReviewDiff)
	}
	return input
}

// parseReview reads the answer of the Reviewer. An answer in another form is an error, so
// that a confused model never counts as a review.
func parseReview(content string) (orchestrator.Review, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(content, "```json"), "```"))
	var review orchestrator.Review
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&review); err != nil {
		return orchestrator.Review{}, fmt.Errorf("the reviewer did not return the JSON object: %w", err)
	}
	if !slices.Contains(orchestrator.Tiers, review.Tier) {
		return orchestrator.Review{}, fmt.Errorf("the reviewer returned the tier %q, which is not "+
			"one of %s", review.Tier, strings.Join(orchestrator.Tiers, ", "))
	}
	return review, nil
}

// Review is the adversarial review of a planned change. A second model call, with its own
// role and prompt, looks for reasons why the change needs a higher tier than the policy
// engine gave it. It is the Review hook of the orchestrator, which lets the result make the
// tier higher and never lower.
//
// In replay mode with no recording there is no review: the result has an empty tier.
func (a *Agent) Review(ctx context.Context, wi orchestrator.WorkItem, plans []orchestrator.Plan,
	policyTier string) (orchestrator.Review, error) {
	key := "review_" + wi.Kind
	if detail := detailKey(wi, plans); detail != "" {
		key += "_" + detail
	}
	resp, err := a.complete(ctx, RoleReviewer, wi.ID, Request{ReplayKey: key,
		Messages: []ChatMessage{
			{Role: "system", Content: reviewerSystem},
			{Role: "user", Content: frame(a.reviewInput(wi, plans, policyTier))},
		}})
	if errors.Is(err, ErrNoRecording) {
		return orchestrator.Review{}, nil
	}
	if err != nil {
		return orchestrator.Review{}, err
	}
	review, err := parseReview(resp.Message.Content)
	if err != nil {
		return orchestrator.Review{}, err
	}
	review.Source = resp.Model
	return review, nil
}
