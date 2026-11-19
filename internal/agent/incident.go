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
	"github.com/statik/site-reliability-clanker/internal/store"
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
	history, err := a.recentChanges(wi.Env)
	if err != nil {
		return diagnosis{}, "", err
	}
	resp, err := a.complete(ctx, RoleInvestigator, wi.ID, Request{
		ReplayKey: "investigate_incident",
		Messages: []ChatMessage{
			{Role: "system", Content: a.App.Render(investigatorSystem)},
			{Role: "user", Content: frame(map[string]any{"alert": f,
				"change_history": strings.Split(history, "\n"),
				"debug_session":  truncate(transcript, 14000)})},
		}})
	if errors.Is(err, ErrNoRecording) {
		return diagnosis{Diagnosis: "No model diagnosis is available in replay mode.",
			Evidence: "See the recorded debug session.", ProposedRunbook: "restart-deployment",
			Deployment: a.App.IncidentDeployment}, "template", nil
	}
	if err != nil {
		return diagnosis{}, "", err
	}
	var d diagnosis
	content, err := firstObject(resp.Message.Content)
	if err == nil {
		err = json.Unmarshal([]byte(content), &d)
	}
	if err != nil {
		return diagnosis{}, "", fmt.Errorf("the investigator did not return the JSON object: %w",
			err)
	}
	return d, resp.Model, nil
}

// recentChanges lists the last finished changes of an environment with their age, so the
// Investigator can tell a fresh release from one that ran for weeks.
func (a *Agent) recentChanges(env string) (string, error) {
	items, err := a.Orch.List()
	if err != nil {
		return "", err
	}
	var lines []string
	for _, wi := range items {
		if wi.Env != env || wi.Kind == "incident" || wi.Tier == "" || wi.Tier == "T0" {
			continue
		}
		age := a.Clock.Now().Sub(store.ParseTime(wi.UpdatedAt)).Round(time.Minute)
		lines = append(lines, fmt.Sprintf("%s %s: %s, %s ago", wi.ID, wi.Title, wi.State, age))
		if len(lines) == 8 {
			break
		}
	}
	if len(lines) == 0 {
		return "no change is on record", nil
	}
	return strings.Join(lines, "\n"), nil
}

// mitigationParams are the parameters of the runbook that the Investigator proposes.
func mitigationParams(d diagnosis) map[string]string {
	return map[string]string{"deployment": d.Deployment}
}

// checkMitigation checks the proposal of the Investigator against the definition of the
// runbook in the runbooks repository. The model proposes; the definition decides: the
// runbook must exist, it must be marked as a mitigation for an incident, and the
// deployment must be one of the values of its input.
func (a *Agent) checkMitigation(d diagnosis) error {
	definition, err := a.Orch.CheckRunbook(d.ProposedRunbook, mitigationParams(d))
	if err != nil {
		return err
	}
	if !definition.IncidentMitigation {
		return fmt.Errorf("%w: runbook %s is not a mitigation for an incident",
			orchestrator.ErrInvalid, definition.Name)
	}
	return nil
}

func (a *Agent) recovered(ctx context.Context, env string) (bool, string) {
	deadline := time.Now().Add(90 * time.Second)
	for {
		sample, err := a.Monitor.Probe(ctx, env, 20)
		if err == nil && sample.Total > 0 && sample.OK == sample.Total {
			if recErr := a.Monitor.Record(sample); recErr != nil {
				return false, recErr.Error()
			}
			return true, fmt.Sprintf("%d of %d %s requests succeed", sample.OK,
				sample.Total, a.App.Health.ProbeName)
		}
		if time.Now().After(deadline) {
			return false, "the error rate did not return to zero in 90 s"
		}
		time.Sleep(5 * time.Second)
	}
}

// openIncident creates the work item of an incident and links the finding to it.
func (a *Agent) openIncident(f detect.Finding) (orchestrator.WorkItem, error) {
	wi, err := a.Orch.Create(orchestrator.NewWorkItem{Title: "Incident: " + f.Title,
		Kind: "incident", Env: f.Env, Trigger: f.Title, Actor: "detector:health"})
	if err != nil {
		return orchestrator.WorkItem{}, err
	}
	a.emit(audit.Event{Type: "incident.opened", Actor: "detector:health", Env: f.Env,
		WorkItem: wi.ID, Summary: "Incident opened: " + f.Title, Data: f.Detail})
	if _, err := a.DB.Exec(`UPDATE findings SET work_item = $1, state = 'drafted'
		WHERE key = $2`, wi.ID, f.Key); err != nil {
		return orchestrator.WorkItem{}, fmt.Errorf("link incident finding to %s: %w", wi.ID, err)
	}
	return a.Orch.Transition(wi.ID, orchestrator.StateDrafting, RoleInvestigator,
		"investigating under a debug grant")
}

// escalate ends an incident that no routine runbook can mitigate. A human must decide.
func (a *Agent) escalate(wi orchestrator.WorkItem, f detect.Finding, d diagnosis,
	model string) error {
	a.emit(audit.Event{Type: "incident.escalated", Actor: RoleInvestigator, Env: f.Env,
		WorkItem: wi.ID, Summary: "Incident needs a human: " + d.Diagnosis,
		Data: map[string]any{"proposed_runbook": d.ProposedRunbook, "evidence": d.Evidence}})
	explanation := orchestrator.Explanation{Sentence: d.Diagnosis, Why: d.WriteUp,
		UserEffect: f.Title, IfDeferred: "Evidence: " + d.Evidence, Source: model,
		Rollback: "No routine mitigation applies. A human must decide the next step."}
	if err := a.Orch.SetExplanation(wi.ID, explanation); err != nil {
		return err
	}
	return a.failIncident(wi, fmt.Errorf("the proposed mitigation %q on %q is not a "+
		"routine runbook; a human must decide", d.ProposedRunbook, d.Deployment))
}

// HandleIncident runs use case 17: alert, diagnosis under a debug grant, mitigation with a
// tier T1 runbook, and the write-up.
func (a *Agent) HandleIncident(ctx context.Context, f detect.Finding) error {
	opened := a.Clock.Now()
	wi, err := a.openIncident(f)
	if err != nil {
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
	if err := a.checkMitigation(d); errors.Is(err, orchestrator.ErrInvalid) {
		return a.escalate(wi, f, d, model)
	} else if err != nil {
		return a.failIncident(wi, err)
	}
	mitigation, err := a.Orch.RunRunbook(ctx, orchestrator.RunbookRequest{Env: f.Env,
		Name: d.ProposedRunbook, Params: mitigationParams(d),
		Actor: RoleInvestigator, Trigger: "Mitigation for " + wi.ID,
		Title: fmt.Sprintf("Restart %s to mitigate %s", d.Deployment, wi.ID)})
	if err != nil {
		return a.failIncident(wi, err)
	}
	return a.resolve(ctx, wi, f, incidentEnd{diagnosis: d, model: model,
		mitigation: mitigation.ID, opened: opened})
}

// incidentEnd is what the write-up of a mitigated incident needs.
type incidentEnd struct {
	diagnosis  diagnosis
	model      string
	mitigation string
	opened     time.Time
}

// resolve waits for the recovery after a mitigation and writes the incident up.
func (a *Agent) resolve(ctx context.Context, wi orchestrator.WorkItem, f detect.Finding,
	end incidentEnd) error {
	ok, check := a.recovered(ctx, f.Env)
	if !ok {
		return a.failIncident(wi, fmt.Errorf("mitigation %s did not help: %s", end.mitigation,
			check))
	}
	minutes := int(a.Clock.Now().Sub(end.opened).Minutes()) + 1
	explanation := orchestrator.Explanation{
		Sentence: end.diagnosis.Diagnosis,
		Why:      end.diagnosis.WriteUp,
		Downtime: fmt.Sprintf("About %d minutes of failed %s requests", minutes,
			a.App.Health.ProbeName),
		UserEffect: f.Title,
		PreChecks: []string{"Debug session recorded: read-only, namespace " +
			a.App.Namespace},
		Rollback:     "The restart is reversible; no configuration changed",
		Verification: check,
		Backup:       "Not needed; no data changed",
		Duration:     fmt.Sprintf("%d minutes from alert to recovery", minutes),
		CostChange:   "None",
		IfDeferred:   "Mitigation: " + end.mitigation + ". Evidence: " + end.diagnosis.Evidence,
		Source:       end.model,
	}
	if err := a.Orch.SetExplanation(wi.ID, explanation); err != nil {
		return err
	}
	a.emit(audit.Event{Type: "incident.resolved", Actor: RoleInvestigator, Env: f.Env,
		WorkItem: wi.ID, Summary: "Incident resolved: " + check,
		Data: map[string]any{"mitigation": end.mitigation, "minutes": minutes}})
	_, err := a.Orch.Transition(wi.ID, orchestrator.StateDone, RoleInvestigator,
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
