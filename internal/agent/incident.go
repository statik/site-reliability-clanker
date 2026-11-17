package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/detect"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
)

type diagnosis struct {
	Diagnosis       string `json:"diagnosis"`
	Evidence        string `json:"evidence"`
	ProposedRunbook string `json:"proposed_runbook"`
	Deployment      string `json:"deployment"`
	WriteUp         string `json:"write_up"`
}

func (a *Agent) diagnose(ctx context.Context, wi orchestrator.WorkItem, f detect.Finding,
	transcript string) (diagnosis, string, error) {
	alert, _ := json.Marshal(f)
	resp, err := a.complete(ctx, RoleInvestigator, wi.ID, Request{
		ReplayKey: "investigate_incident",
		Messages: []ChatMessage{
			{Role: "system", Content: investigatorSystem},
			{Role: "user", Content: "<data>alert: " + string(alert) + "\n\ndebug session:\n" +
				truncate(transcript, 8000) + "</data>"},
		}})
	if errors.Is(err, ErrNoRecording) {
		return diagnosis{Diagnosis: "No model diagnosis is available in replay mode.",
			Evidence: "See the recorded debug session.", ProposedRunbook: "restart-deployment",
			Deployment: "paperboat-api"}, "template", nil
	}
	if err != nil {
		return diagnosis{}, "", err
	}
	var d diagnosis
	content := strings.TrimSpace(resp.Message.Content)
	content = strings.TrimSuffix(strings.TrimPrefix(content, "```json"), "```")
	if err := json.Unmarshal([]byte(content), &d); err != nil {
		return diagnosis{}, "", fmt.Errorf("the investigator did not return the JSON object: %w",
			err)
	}
	return d, resp.Model, nil
}

// allowedMitigations are the deployments that the restart runbook may target after an
// incident. The model proposes; this list decides.
var allowedMitigations = map[string]bool{"paperboat-api": true, "paperboat-web": true,
	"paperboat-worker": true}

func (a *Agent) recovered(ctx context.Context, env string) (bool, string) {
	deadline := time.Now().Add(90 * time.Second)
	for {
		sample, err := a.Monitor.Probe(ctx, env, 20)
		if err == nil && sample.Total > 0 && sample.OK == sample.Total {
			if recErr := a.Monitor.Record(sample); recErr != nil {
				return false, recErr.Error()
			}
			return true, fmt.Sprintf("%d of %d inventory requests succeed", sample.OK,
				sample.Total)
		}
		if time.Now().After(deadline) {
			return false, "the error rate did not return to zero in 90 s"
		}
		time.Sleep(5 * time.Second)
	}
}

// HandleIncident runs use case 17: alert, diagnosis under a debug grant, mitigation with a
// tier T1 runbook, and the write-up.
func (a *Agent) HandleIncident(ctx context.Context, f detect.Finding) error {
	wi, err := a.Orch.Create(orchestrator.NewWorkItem{Title: "Incident: " + f.Title,
		Kind: "incident", Env: f.Env, Trigger: f.Title, Actor: "detector:health"})
	if err != nil {
		return err
	}
	opened := a.Clock.Now()
	a.emit(audit.Event{Type: "incident.opened", Actor: "detector:health", Env: f.Env,
		WorkItem: wi.ID, Summary: "Incident opened: " + f.Title, Data: f.Detail})
	if _, err := a.DB.Exec(`UPDATE findings SET work_item = ?, state = 'drafted'
		WHERE key = ?`, wi.ID, f.Key); err != nil {
		return fmt.Errorf("link incident finding to %s: %w", wi.ID, err)
	}
	if wi, err = a.Orch.Transition(wi.ID, orchestrator.StateDrafting, RoleInvestigator,
		"investigating under a debug grant"); err != nil {
		return err
	}
	transcript, err := a.Orch.DebugSession(ctx, wi, RoleInvestigator, f.Title)
	if err != nil {
		return a.failIncident(wi, err)
	}
	d, model, err := a.diagnose(ctx, wi, f, transcript)
	if err != nil {
		return a.failIncident(wi, err)
	}
	a.emit(audit.Event{Type: "incident.diagnosed", Actor: RoleInvestigator, Env: f.Env,
		WorkItem: wi.ID, Summary: "Diagnosis: " + d.Diagnosis,
		Data: map[string]any{"evidence": d.Evidence, "proposed_runbook": d.ProposedRunbook,
			"deployment": d.Deployment, "model": model}})
	if d.ProposedRunbook != "restart-deployment" || !allowedMitigations[d.Deployment] {
		return a.failIncident(wi, fmt.Errorf("the proposed mitigation %q on %q is not a "+
			"routine runbook; a human must decide", d.ProposedRunbook, d.Deployment))
	}
	mitigation, err := a.Orch.RunRunbook(ctx, orchestrator.RunbookRequest{Env: f.Env,
		Name: "restart-deployment", Params: map[string]string{"deployment": d.Deployment},
		Actor: RoleInvestigator, Trigger: "Mitigation for " + wi.ID,
		Title: fmt.Sprintf("Restart %s to mitigate %s", d.Deployment, wi.ID)})
	if err != nil {
		return a.failIncident(wi, err)
	}
	ok, check := a.recovered(ctx, f.Env)
	if !ok {
		return a.failIncident(wi, fmt.Errorf("mitigation %s did not help: %s", mitigation.ID,
			check))
	}
	minutes := int(a.Clock.Now().Sub(opened).Minutes()) + 1
	explanation := orchestrator.Explanation{
		Sentence:     d.Diagnosis,
		Why:          d.WriteUp,
		Downtime:     fmt.Sprintf("About %d minutes of failed inventory requests", minutes),
		UserEffect:   f.Title,
		PreChecks:    []string{"Debug session recorded: read-only, namespace paperboat"},
		Rollback:     "The restart is reversible; no configuration changed",
		Verification: check,
		Backup:       "Not needed; no data changed",
		Duration:     fmt.Sprintf("%d minutes from alert to recovery", minutes),
		CostChange:   "None",
		IfDeferred:   "Mitigation: " + mitigation.ID + ". Evidence: " + d.Evidence,
		Source:       model,
	}
	if err := a.Orch.SetExplanation(wi.ID, explanation); err != nil {
		return err
	}
	a.emit(audit.Event{Type: "incident.resolved", Actor: RoleInvestigator, Env: f.Env,
		WorkItem: wi.ID, Summary: "Incident resolved: " + check,
		Data: map[string]any{"mitigation": mitigation.ID, "minutes": minutes}})
	_, err = a.Orch.Transition(wi.ID, orchestrator.StateDone, RoleInvestigator,
		"mitigated and verified")
	return err
}

func (a *Agent) failIncident(wi orchestrator.WorkItem, cause error) error {
	if _, err := a.Orch.Transition(wi.ID, orchestrator.StateFailed, RoleInvestigator,
		cause.Error()); err != nil {
		return err
	}
	return cause
}
