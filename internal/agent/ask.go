package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// Citation points an answer at its source.
type Citation struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ChatEntry is one message of the Ask conversation.
type ChatEntry struct {
	ID        int64      `json:"id"`
	WorkItem  string     `json:"work_item"`
	User      string     `json:"user"`
	Role      string     `json:"role"`
	Body      string     `json:"body"`
	Citations []Citation `json:"citations"`
	At        string     `json:"at"`
}

// grounding is the set of records that an answer may use.
type grounding struct {
	lines     []string
	citations []Citation
}

func (g *grounding) add(line string, c ...Citation) {
	g.lines = append(g.lines, line)
	g.citations = append(g.citations, c...)
}

func cite(wi orchestrator.WorkItem) Citation {
	return Citation{Type: "work_item", ID: wi.ID, Label: wi.ID + " " + wi.Title}
}

type intent struct {
	name     string
	keywords []string
	gather   func(a *Agent, workItem string) (grounding, error)
}

var intents = []intent{
	{"waiting", []string{"waiting", "leave it", "pending", "need me", "needs me"}, (*Agent).gatherWaiting},
	{"patches", []string{"patch", "behind", "cve", "advisor", "vulnerab"}, (*Agent).gatherPatches},
	{"changes", []string{"changed", "last 30", "who approved", "what happened"}, (*Agent).gatherChanges},
	{"restore", []string{"restore", "backup"}, (*Agent).gatherRestore},
	{"duration", []string{"longer", "how long", "why did"}, (*Agent).gatherDuration},
	{"upcoming", []string{"next month", "upcoming", "maintenance look", "scheduled"}, (*Agent).gatherUpcoming},
	{"access", []string{"access", "permission", "credential"}, (*Agent).gatherAccess},
}

func (a *Agent) gatherWaiting(string) (grounding, error) {
	var g grounding
	items, err := a.Orch.List()
	if err != nil {
		return g, err
	}
	for _, wi := range items {
		if wi.State != orchestrator.StateAwaitingApproval {
			continue
		}
		age := a.Clock.Now().Sub(store.ParseTime(wi.UpdatedAt)).Round(time.Minute)
		g.add(fmt.Sprintf("%s (%s, tier %s) waits for approval for %s. If deferred: %s",
			wi.ID, wi.Title, wi.Tier, age, wi.Explanation.IfDeferred), cite(wi))
	}
	if len(g.lines) == 0 {
		g.add("Nothing waits for a decision.")
	}
	return g, nil
}

func (a *Agent) gatherPatches(string) (grounding, error) {
	var g grounding
	rows, err := a.DB.Query(`SELECT title, state, work_item, detail FROM findings
		WHERE detector IN ('advisory', 'release') ORDER BY id DESC LIMIT 20`)
	if err != nil {
		return g, fmt.Errorf("read findings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var title, state, workItem, detail string
		if err := rows.Scan(&title, &state, &workItem, &detail); err != nil {
			return g, fmt.Errorf("scan finding: %w", err)
		}
		status := "no work item yet"
		citations := []Citation{}
		if workItem != "" {
			wi, err := a.Orch.Get(workItem)
			if err != nil {
				return g, err
			}
			status = fmt.Sprintf("%s is in state %s", wi.ID, wi.State)
			citations = append(citations, cite(wi))
		}
		g.add(fmt.Sprintf("%s: %s", title, status), citations...)
	}
	if len(g.lines) == 0 {
		g.add("No patch or release finding is on record.")
	}
	return g, rows.Err()
}

func (a *Agent) gatherChanges(string) (grounding, error) {
	var g grounding
	since := store.FormatTime(a.Clock.Now().Add(-30 * 24 * time.Hour))
	items, err := a.Orch.List()
	if err != nil {
		return g, err
	}
	for _, wi := range items {
		closed := wi.State == orchestrator.StateDone || wi.State == orchestrator.StateRolledBack
		if !closed || wi.UpdatedAt < since || wi.Tier == "T0" || wi.Tier == "" {
			continue
		}
		approvals, err := a.Orch.Approvals(wi.ID)
		if err != nil {
			return g, err
		}
		names := []string{}
		for _, ap := range approvals {
			if ap.Decision == "approve" {
				names = append(names, ap.Approver)
			}
		}
		approvedBy := strings.Join(names, " and ")
		if approvedBy == "" {
			approvedBy = "a standing approval or the approval of the original change"
		}
		g.add(fmt.Sprintf("%s %s: %s on %s, tier %s, approved by %s", wi.ID, wi.Title,
			wi.State, wi.UpdatedAt[:10], wi.Tier, approvedBy), cite(wi))
	}
	if len(g.lines) == 0 {
		g.add("No change finished in the last 30 days.")
	}
	return g, nil
}

func (a *Agent) gatherRestore(string) (grounding, error) {
	var g grounding
	items, err := a.Orch.List()
	if err != nil {
		return g, err
	}
	for _, kind := range []string{"runbook:restore-drill", "runbook:db-backup"} {
		found := false
		for _, wi := range items {
			if wi.Kind == kind && wi.State == orchestrator.StateDone {
				g.add(fmt.Sprintf("Last successful %s: %s on %s", strings.TrimPrefix(kind,
					"runbook:"), wi.ID, wi.UpdatedAt[:16]), cite(wi))
				found = true
				break
			}
		}
		if !found {
			g.add(fmt.Sprintf("No successful %s is on record.", strings.TrimPrefix(kind,
				"runbook:")))
		}
	}
	return g, nil
}

func (a *Agent) gatherDuration(workItem string) (grounding, error) {
	var g grounding
	if workItem == "" {
		g.add("Ask this question on the page of a work item to see the time in each state.")
		return g, nil
	}
	events, err := a.Audit.Query(audit.Filter{WorkItem: workItem,
		Types: []string{"workitem.state_changed"}})
	if err != nil {
		return g, err
	}
	for i, e := range events {
		end := a.Clock.Now()
		if i+1 < len(events) {
			end = store.ParseTime(events[i+1].TS)
		}
		to, _ := e.Data["to"].(string)
		g.add(fmt.Sprintf("%s spent %s in state %s", workItem,
			end.Sub(store.ParseTime(e.TS)).Round(time.Second), to),
			Citation{Type: "event", ID: fmt.Sprint(e.Seq), Label: e.Summary})
	}
	return g, nil
}

func (a *Agent) gatherUpcoming(string) (grounding, error) {
	var g grounding
	items, err := a.Orch.List()
	if err != nil {
		return g, err
	}
	for _, wi := range items {
		if wi.State == orchestrator.StateScheduled {
			g.add(fmt.Sprintf("%s %s is scheduled for %s", wi.ID, wi.Title, wi.ScheduledFor),
				cite(wi))
		}
	}
	raw, err := store.GetKV(a.DB, "feed.advisories")
	if err != nil {
		return g, err
	}
	var advisories []struct{ Title, Due string }
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &advisories); err != nil {
			return g, fmt.Errorf("decode advisory feed: %w", err)
		}
	}
	for _, adv := range advisories {
		if adv.Due != "" && adv.Due > a.now() {
			g.add(fmt.Sprintf("Forecast: %s, due %s", adv.Title, adv.Due[:10]))
		}
	}
	if len(g.lines) == 0 {
		g.add("No work is scheduled and no forecast has a date.")
	}
	return g, nil
}

func (a *Agent) gatherAccess(string) (grounding, error) {
	var g grounding
	g.add("Standing access: observer and planner are read-only. Every applier identity, the " +
		"runbook identity and the debugger have no standing access.")
	rows, err := a.DB.Query(`SELECT id, identity, work_item, expires FROM grants
		WHERE status IN ('issued', 'active') AND kind != 'plan'`)
	if err != nil {
		return g, fmt.Errorf("read grants: %w", err)
	}
	defer rows.Close()
	active := 0
	for rows.Next() {
		var id, identity, workItem, expires string
		if err := rows.Scan(&id, &identity, &workItem, &expires); err != nil {
			return g, fmt.Errorf("scan grant: %w", err)
		}
		active++
		g.add(fmt.Sprintf("Grant %s is active: %s for %s until %s", id, identity, workItem,
			expires), Citation{Type: "grant", ID: id, Label: "Grant " + id})
	}
	if active == 0 {
		g.add("No write grant is active right now.")
	}
	return g, rows.Err()
}

func (a *Agent) gatherWorkItem(workItem string) (grounding, error) {
	var g grounding
	if workItem == "" {
		g.add("I can answer about waiting approvals, patches, recent changes, restore tests, " +
			"upcoming maintenance and current access.")
		return g, nil
	}
	wi, err := a.Orch.Get(workItem)
	if err != nil {
		return g, err
	}
	g.add(fmt.Sprintf("%s %s is in state %s, tier %s by rule %s. %s", wi.ID, wi.Title,
		wi.State, wi.Tier, wi.TierRule, wi.Explanation.Sentence), cite(wi))
	plans, err := a.Orch.Plans(workItem, false)
	if err != nil {
		return g, err
	}
	for _, p := range plans {
		g.add(fmt.Sprintf("Plan of %s: %d to add, %d to change, %d to destroy, %d to replace",
			p.Layer, p.Facts.Add, p.Facts.Change, p.Facts.Destroy, p.Facts.Replace))
		for _, rc := range p.Facts.Resources {
			g.add(fmt.Sprintf("%s %s: %s", rc.Action, rc.Address, strings.Join(rc.Details, "; ")))
		}
	}
	g.add("Rollback: " + wi.Explanation.Rollback)
	return g, nil
}

func (a *Agent) saveChat(e ChatEntry) (ChatEntry, error) {
	citations, _ := json.Marshal(e.Citations)
	e.At = a.now()
	err := a.DB.QueryRow(`INSERT INTO chat (work_item, username, role, body, citations, at)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`, e.WorkItem, e.User, e.Role, e.Body, string(citations), e.At).Scan(&e.ID)
	if err != nil {
		return ChatEntry{}, fmt.Errorf("store chat message: %w", err)
	}
	return e, nil
}

// Ask answers a question from the records of the control plane. The records are gathered by
// plain code. A live model writes the prose; replay mode returns the records as a list.
func (a *Agent) Ask(ctx context.Context, user, workItem, question string) (ChatEntry, error) {
	if strings.TrimSpace(question) == "" {
		return ChatEntry{}, fmt.Errorf("the question is empty")
	}
	if _, err := a.saveChat(ChatEntry{WorkItem: workItem, User: user, Role: "user",
		Body: question, Citations: []Citation{}}); err != nil {
		return ChatEntry{}, err
	}
	gather := (*Agent).gatherWorkItem
	lower := strings.ToLower(question)
	for _, in := range intents {
		if containsAny(lower, in.keywords) {
			gather = in.gather
			break
		}
	}
	g, err := gather(a, workItem)
	if err != nil {
		return ChatEntry{}, err
	}
	body := strings.Join(g.lines, "\n")
	if a.LLM.Mode() != "replay" {
		resp, err := a.complete(ctx, RoleExplainer, workItem, Request{
			Messages: []ChatMessage{
				{Role: "system", Content: askSystem},
				{Role: "user", Content: "Question: " + question + "\n" + frame(g.lines)},
			}})
		if err != nil && !errors.Is(err, ErrNoRecording) {
			return ChatEntry{}, err
		}
		if err == nil {
			body = resp.Message.Content
		}
	}
	if g.citations == nil {
		g.citations = []Citation{}
	}
	a.emit(audit.Event{Type: "agent.answered", Actor: RoleExplainer, WorkItem: workItem,
		Summary: "Answered a question from " + user,
		Data:    map[string]any{"question": question, "mode": a.LLM.Mode()}})
	return a.saveChat(ChatEntry{WorkItem: workItem, User: user, Role: "assistant", Body: body,
		Citations: g.citations})
}

func containsAny(text string, keywords []string) bool {
	for _, k := range keywords {
		if strings.Contains(text, k) {
			return true
		}
	}
	return false
}

// History returns the Ask conversation. An empty work item gives the global conversation.
func (a *Agent) History(workItem string) ([]ChatEntry, error) {
	rows, err := a.DB.Query(`SELECT id, work_item, username, role, body, citations, at FROM chat
		WHERE work_item = $1 ORDER BY id`, workItem)
	if err != nil {
		return nil, fmt.Errorf("read chat history: %w", err)
	}
	defer rows.Close()
	entries := []ChatEntry{}
	for rows.Next() {
		var e ChatEntry
		var citations string
		if err := rows.Scan(&e.ID, &e.WorkItem, &e.User, &e.Role, &e.Body, &citations,
			&e.At); err != nil {
			return nil, fmt.Errorf("scan chat message: %w", err)
		}
		if err := json.Unmarshal([]byte(citations), &e.Citations); err != nil {
			return nil, fmt.Errorf("decode citations of chat message %d: %w", e.ID, err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
