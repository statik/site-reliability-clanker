package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/scrub"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// Roles of the agent. Each role has its own tool set, and that tool set is its boundary.
const (
	RoleTriage       = "triage"
	RoleAuthor       = "author"
	RoleExplainer    = "explainer"
	RoleInvestigator = "investigator"
	RoleReviewer     = "reviewer"
)

// Clock gives the virtual time.
type Clock interface{ Now() time.Time }

// Agent runs the model loops for every role.
type Agent struct {
	DB      *sql.DB
	Audit   *audit.Log
	Orch    *orchestrator.Orchestrator
	Forge   *forge.Forge
	Monitor *monitor.Monitor
	Clock   Clock
	LLM     LLM
	// App is the application pack. Its names go into the prompts of the roles.
	App pack.Pack
	// Models maps a role to the name of a model on the endpoint of the LLM.
	Models map[string]string
	// DailyBudgetUSD stops model calls for the day when the spend reaches it.
	DailyBudgetUSD float64
	// DailyBudgetTokens stops model calls for the day when the calls that report no cost
	// reach this number of tokens. Zero means no limit.
	DailyBudgetTokens int64
}

// DefaultDailyTokens is the daily token limit for calls that report no cost. An endpoint
// that the operator runs reports no cost, so the limit in USD never stops a loop there.
const DefaultDailyTokens = 5_000_000

// DefaultModels are the OpenRouter models for each role.
func DefaultModels() map[string]string {
	return map[string]string{
		RoleTriage:       "anthropic/claude-haiku-5.5",
		RoleExplainer:    "anthropic/claude-haiku-5.5",
		RoleAuthor:       "anthropic/claude-sonnet-5.5",
		RoleInvestigator: "anthropic/claude-sonnet-5.5",
		RoleReviewer:     "anthropic/claude-sonnet-5.5",
	}
}

func (a *Agent) now() string { return store.FormatTime(a.Clock.Now()) }

func (a *Agent) spentToday() (float64, error) {
	day := a.now()[:10]
	var spent sql.NullFloat64
	err := a.DB.QueryRow(`SELECT SUM(cost_usd) FROM agent_calls WHERE at >= $1`, day).Scan(&spent)
	if err != nil {
		return 0, fmt.Errorf("read inference spend: %w", err)
	}
	return spent.Float64, nil
}

// unpricedTokensToday counts the tokens of the calls of today that report no cost. Replay
// calls record zero tokens, so they add nothing.
func (a *Agent) unpricedTokensToday() (int64, error) {
	day := a.now()[:10]
	var tokens int64
	err := a.DB.QueryRow(`SELECT COALESCE(SUM(prompt_tokens + completion_tokens), 0)::BIGINT
		FROM agent_calls WHERE at >= $1 AND cost_usd = 0`, day).Scan(&tokens)
	if err != nil {
		return 0, fmt.Errorf("read the tokens of calls with no cost: %w", err)
	}
	return tokens, nil
}

// usage is the model use of one day.
type usage struct {
	spentUSD       float64
	unpricedTokens int64
}

// budgetReached says which daily limit stops the next call. The summary is for the audit
// log and the error is for the caller. Both are empty when no limit is reached.
func (a *Agent) budgetReached(used usage) (summary string, err error) {
	if a.DailyBudgetUSD > 0 && used.spentUSD >= a.DailyBudgetUSD {
		return fmt.Sprintf("Inference spend cap reached: %.2f of %.2f USD today", used.spentUSD,
				a.DailyBudgetUSD),
			fmt.Errorf("the inference budget of %.2f USD for today is spent", a.DailyBudgetUSD)
	}
	if a.DailyBudgetTokens > 0 && used.unpricedTokens >= a.DailyBudgetTokens {
		return fmt.Sprintf("Inference token cap reached: %d of %d tokens with no cost today",
				used.unpricedTokens, a.DailyBudgetTokens),
			fmt.Errorf("the inference budget of %d tokens for today is spent on calls with "+
				"no cost", a.DailyBudgetTokens)
	}
	return "", nil
}

// complete sends one request for a role, enforces the budget, puts the name of the pack in
// the replay key, scrubs the input and the answer, and records the call with its transcript.
func (a *Agent) complete(ctx context.Context, role, workItem string, req Request) (Response, error) {
	spent, err := a.spentToday()
	if err != nil {
		return Response{}, err
	}
	tokens, err := a.unpricedTokensToday()
	if err != nil {
		return Response{}, err
	}
	if summary, err := a.budgetReached(usage{spentUSD: spent, unpricedTokens: tokens}); err != nil {
		a.emit(audit.Event{Type: "agent.budget_reached", Actor: role, WorkItem: workItem,
			Summary: summary})
		return Response{}, err
	}
	req.Model = a.Models[role]
	if req.ReplayKey != "" {
		req.ReplayKey = a.App.Name + "_" + req.ReplayKey
	}
	for i := range req.Messages {
		req.Messages[i].Content = scrub.Text(req.Messages[i].Content)
	}
	resp, err := a.LLM.Complete(ctx, req)
	if err != nil {
		return Response{}, err
	}
	// Every role gets its answer from here, and the transcript below is the stored record.
	// A model can repeat a secret that passed the scrubber on the way in, so the answer and
	// the arguments of each tool call are scrubbed before any other code reads them.
	resp.Message.Content = scrubAnswer(resp.Message.Content)
	for i := range resp.Message.ToolCalls {
		call := &resp.Message.ToolCalls[i]
		call.Function.Arguments = scrubAnswer(call.Function.Arguments)
	}
	transcript, _ := json.Marshal(map[string]any{"request": req.Messages,
		"response": resp.Message, "replay_key": req.ReplayKey, "step": req.Step})
	if _, err := a.DB.Exec(`INSERT INTO agent_calls (role, work_item, model, mode,
		prompt_tokens, completion_tokens, cost_usd, transcript, at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		role, workItem, resp.Model, a.LLM.Mode(), resp.PromptTokens, resp.CompletionTokens,
		resp.CostUSD, string(transcript), a.now()); err != nil {
		return Response{}, fmt.Errorf("record agent call: %w", err)
	}
	return resp, nil
}

// frame wraps untrusted input for a model. json.Marshal writes < and > as escapes, so no
// text inside the value can close the <data> element.
func frame(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		raw = []byte(`"input could not be encoded"`)
	}
	return "<data>" + string(raw) + "</data>"
}

func (a *Agent) emit(e audit.Event) {
	if _, err := a.Audit.Append(e); err != nil {
		panic(fmt.Sprintf("audit log is not writable, stopping: %v", err))
	}
}

// Call is one recorded model call.
type Call struct {
	ID               int64   `json:"id"`
	Role             string  `json:"role"`
	WorkItem         string  `json:"work_item"`
	Model            string  `json:"model"`
	Mode             string  `json:"mode"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	Transcript       any     `json:"transcript"`
	At               string  `json:"at"`
}

// Calls returns the newest model calls. A work item filter is optional.
func (a *Agent) Calls(workItem string, limit int) ([]Call, error) {
	query := `SELECT id, role, work_item, model, mode, prompt_tokens, completion_tokens,
		cost_usd, transcript, at FROM agent_calls`
	args := []any{}
	if workItem != "" {
		query += ` WHERE work_item = $1`
		args = append(args, workItem)
	}
	args = append(args, limit)
	rows, err := a.DB.Query(query+fmt.Sprintf(` ORDER BY id DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("list agent calls: %w", err)
	}
	defer rows.Close()
	calls := []Call{}
	for rows.Next() {
		var c Call
		var transcript string
		if err := rows.Scan(&c.ID, &c.Role, &c.WorkItem, &c.Model, &c.Mode, &c.PromptTokens,
			&c.CompletionTokens, &c.CostUSD, &transcript, &c.At); err != nil {
			return nil, fmt.Errorf("scan agent call: %w", err)
		}
		if err := json.Unmarshal([]byte(transcript), &c.Transcript); err != nil {
			c.Transcript = transcript
		}
		calls = append(calls, c)
	}
	return calls, rows.Err()
}

// detailKey gives the part of a replay key that identifies the change, for example the
// target version of an application upgrade.
func detailKey(wi orchestrator.WorkItem, plans []orchestrator.Plan) string {
	for _, p := range plans {
		for _, rc := range p.Facts.Resources {
			for _, detail := range rc.Details {
				path, change, _ := strings.Cut(detail, ": ")
				_, after, ok := strings.Cut(change, " → ")
				if !ok {
					continue
				}
				if strings.HasSuffix(path, "image.tag") {
					return after
				}
				if strings.HasSuffix(path, "container.image") {
					_, tag, _ := strings.Cut(after, ":")
					return strings.TrimSuffix(tag, "-alpine")
				}
			}
		}
	}
	return ""
}

// deferredLayers returns the layers of a work item that have no plan yet.
func deferredLayers(wi orchestrator.WorkItem, plans []orchestrator.Plan) []string {
	planned := map[string]bool{}
	for _, p := range plans {
		planned[p.Layer] = true
	}
	deferred := []string{}
	for _, layer := range wi.Layers {
		if !planned[layer] {
			deferred = append(deferred, layer)
		}
	}
	return deferred
}

// Explain writes the prose of the approval page. It is the Explain hook of the orchestrator.
// Without a recording in replay mode it returns the template text.
func (a *Agent) Explain(ctx context.Context, wi orchestrator.WorkItem,
	plans []orchestrator.Plan) (orchestrator.Explanation, error) {
	deferred := deferredLayers(wi, plans)
	detail := detailKey(wi, plans)
	input := map[string]any{"title": wi.Title, "kind": wi.Kind, "env": wi.Env,
		"layers": wi.Layers, "tier": wi.Tier, "tier_rule": wi.TierRule, "trigger": wi.Trigger,
		"plans": plans, "deferred_layers": deferred}
	note, err := a.Forge.ReadFile(a.App.Releases.Repository, "main", a.App.ReleaseNote(detail))
	if err == nil {
		input["release_note"] = truncate(note, 4000)
	}
	key := "explain_" + wi.Kind
	if detail != "" {
		key += "_" + detail
	}
	resp, err := a.complete(ctx, RoleExplainer, wi.ID, Request{ReplayKey: key,
		Messages: []ChatMessage{
			{Role: "system", Content: explainerSystem},
			{Role: "user", Content: frame(input)},
		}})
	if errors.Is(err, ErrNoRecording) {
		return orchestrator.TemplateExplanation(wi, plans), nil
	}
	if err != nil {
		return orchestrator.Explanation{}, err
	}
	var explanation orchestrator.Explanation
	content, err := firstObject(resp.Message.Content)
	if err == nil {
		err = json.Unmarshal([]byte(content), &explanation)
	}
	if err != nil {
		return orchestrator.Explanation{}, fmt.Errorf("the explainer did not return the JSON "+
			"object: %w", err)
	}
	if explanation.Sentence == "" {
		return orchestrator.Explanation{}, fmt.Errorf("the explainer returned no sentence")
	}
	explanation.Source = resp.Model
	a.emit(audit.Event{Type: "agent.explained", Actor: RoleExplainer, Env: wi.Env,
		WorkItem: wi.ID, Summary: "Explainer wrote the approval summary",
		Data: map[string]any{"model": resp.Model, "mode": a.LLM.Mode(),
			"cost_usd": resp.CostUSD}})
	return explanation, nil
}
