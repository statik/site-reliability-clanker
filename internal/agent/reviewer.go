package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/scrub"
)

// maxReviewDiff is how much of the diff of a change the Reviewer reads.
const maxReviewDiff = 12000

// reviewInput gathers what the Reviewer judges: the work item, the plans with the decision
// of the policy engine for each, the layers that have no plan yet, and the diff of the pull
// request when there is one.
func (a *Agent) reviewInput(wi orchestrator.WorkItem, plans []orchestrator.Plan,
	policyTier string) map[string]any {
	input := map[string]any{"title": wi.Title, "kind": wi.Kind, "env": wi.Env,
		"layers": wi.Layers, "trigger": wi.Trigger, "policy_tier": policyTier, "plans": plans,
		"deferred_layers": deferredLayers(wi, plans)}
	if wi.PRID == 0 || wi.BaseCommit == "" || wi.BaseCommit == wi.Commit {
		return input
	}
	// A diff that cannot be read is not a reason to skip the review of the plans.
	if diff, err := a.Forge.Diff(orchestrator.PlatformRepo, wi.BaseCommit, wi.Commit); err == nil {
		input["diff"] = truncate(diff, maxReviewDiff)
	}
	return input
}

// reviewFields are the fields of the answer that the Reviewer prompt asks for.
var reviewFields = []string{"tier", "concerns"}

// readReviewField reads one key and its value from the object of the answer. It refuses a
// key that the prompt does not ask for and a key that the answer has twice.
func readReviewField(decoder *json.Decoder, fields map[string]json.RawMessage) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	key, _ := token.(string)
	if !slices.Contains(reviewFields, key) {
		return fmt.Errorf("the field %q is not one of %s", key, strings.Join(reviewFields, ", "))
	}
	if _, seen := fields[key]; seen {
		return fmt.Errorf("the field %q is in the answer more than once", key)
	}
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	fields[key] = value
	return nil
}

// readReviewObject reads the answer as one JSON object and returns the raw value of each
// field. Text after the object is an error.
func readReviewObject(content string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(content))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, errors.New("the answer does not start with a JSON object")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		if err := readReviewField(decoder, fields); err != nil {
			return nil, err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("the answer has text after the JSON object")
	}
	return fields, nil
}

// reviewConcerns reads the field concerns: a JSON array of strings, which can be empty.
func reviewConcerns(raw json.RawMessage) ([]string, error) {
	var items []*string
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("the field concerns is not a list of strings: %w", err)
	}
	if items == nil {
		return nil, errors.New("the field concerns is missing or null")
	}
	concerns := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			return nil, errors.New("the field concerns has a null item")
		}
		concerns = append(concerns, *item)
	}
	return concerns, nil
}

// parseReview reads the answer of the Reviewer. It accepts only the form that the prompt
// asks for: one JSON object, alone or in a ```json fence, with the field tier, one of the
// tiers, and the field concerns, a list of strings, each field once and no other field. An
// answer in another form is an error, so that a confused model never counts as a review.
// The answer never sets the source of the review; Review sets it.
func parseReview(content string) (orchestrator.Review, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(content, "```json"), "```"))
	fields, err := readReviewObject(content)
	if err != nil {
		return orchestrator.Review{}, fmt.Errorf("the reviewer did not return the JSON object: %w", err)
	}
	concerns, err := reviewConcerns(fields["concerns"])
	if err != nil {
		return orchestrator.Review{}, fmt.Errorf("the reviewer did not return the JSON object: %w", err)
	}
	var tier string
	if raw, found := fields["tier"]; found {
		if err := json.Unmarshal(raw, &tier); err != nil {
			return orchestrator.Review{}, fmt.Errorf("the reviewer did not return the JSON "+
				"object: the field tier is not a string: %w", err)
		}
	}
	if !slices.Contains(orchestrator.Tiers, tier) {
		return orchestrator.Review{}, fmt.Errorf("the reviewer returned the tier %q, which is not "+
			"one of %s", tier, strings.Join(orchestrator.Tiers, ", "))
	}
	return orchestrator.Review{Tier: tier, Concerns: concerns}, nil
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
	// The concerns go into the audit log and onto the approval page. The prompt tells the
	// Reviewer not to repeat a secret; the scrubber removes the forms that it knows.
	for i, concern := range review.Concerns {
		review.Concerns[i] = scrub.Text(concern)
	}
	review.Source = resp.Model
	return review, nil
}
