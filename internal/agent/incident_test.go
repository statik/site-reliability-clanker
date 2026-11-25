package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
	"github.com/statik/site-reliability-clanker/internal/runbook"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// scaleDown takes a component as its input, as a mitigation does, and is not marked as a
// mitigation for an incident.
var scaleDown = fstest.MapFS{
	"scale-down.json": {Data: []byte(`{"description":"Scale a component to zero.","tier":"T1",
		"inputs":{"component":{"type":"string","description":"The component.",
		"values":["api"]}}}`)},
}

func agentWithRunbooks(t *testing.T) *Agent {
	t.Helper()
	db := storetest.Open(t)
	f := forge.New(db)
	if err := f.Seed(orchestrator.RunbooksRepo, packtest.Runbooks(t, scaleDown),
		"Seed runbooks"); err != nil {
		t.Fatal(err)
	}
	return &Agent{App: packtest.Example(t), Orch: &orchestrator.Orchestrator{DB: db, Forge: f}}
}

func TestTheDefinitionDecidesWhichMitigationRuns(t *testing.T) {
	a := agentWithRunbooks(t)
	for _, component := range a.App.Components {
		definition, err := a.checkMitigation(diagnosis{ProposedRunbook: a.App.Incident.Runbook,
			Component: component.Name})
		if err != nil || definition.Name != a.App.Incident.Runbook {
			t.Errorf("mitigation of %s: definition %q, %v", component.Name, definition.Name, err)
		}
	}
	refused := map[string]diagnosis{
		"no mitigation": {ProposedRunbook: "none"},
		"no answer":     {},
		"a component outside the set": {ProposedRunbook: "restart-component",
			Component: "postgres"},
		"the deployment of a component, not its name": {ProposedRunbook: "restart-component",
			Component: "paperboat-api"},
		"no component": {ProposedRunbook: "restart-component"},
		"a runbook that does not exist": {ProposedRunbook: "db-drop-and-restore",
			Component: "api"},
		"a runbook that is not a mitigation": {ProposedRunbook: "db-backup"},
		"a runbook with the same input that is not a mitigation": {
			ProposedRunbook: "scale-down", Component: "api"},
		"text that is not a name": {ProposedRunbook: "restart-component; db-backup",
			Component: "api"},
	}
	for label, d := range refused {
		if _, err := a.checkMitigation(d); !errors.Is(err, orchestrator.ErrInvalid) {
			t.Errorf("%s: error = %v, want a refusal that sends the incident to a human", label,
				err)
		}
	}
}

// With no answer of a model, the incident proposes the mitigation that the pack names. A
// pack with none proposes nothing, so the incident goes to a person.
func TestWithNoModelThePackDecidesTheMitigation(t *testing.T) {
	a := agentWithRunbooks(t)
	d := a.templateDiagnosis()
	if d.ProposedRunbook != "restart-component" || d.Component != "api" {
		t.Errorf("the diagnosis proposes %q on %q", d.ProposedRunbook, d.Component)
	}
	if _, err := a.checkMitigation(d); err != nil {
		t.Errorf("the mitigation of the pack is refused: %v", err)
	}
	a.App.Incident = nil
	d = a.templateDiagnosis()
	if _, err := a.checkMitigation(d); !errors.Is(err, orchestrator.ErrInvalid) {
		t.Errorf("a pack with no incident mitigation proposes %q: %v", d.ProposedRunbook, err)
	}
}

// The title of the work item of a mitigation has the title of the runbook, which the
// definition gives, and the component.
func TestTheWorkItemOfAMitigationHasTheTitleOfTheRunbook(t *testing.T) {
	a := agentWithRunbooks(t)
	d := diagnosis{ProposedRunbook: "restart-component", Component: "worker"}
	definition, err := a.checkMitigation(d)
	if err != nil {
		t.Fatal(err)
	}
	want := "Restart a component (worker) to mitigate WI-0007"
	if got := mitigationTitle(definition, d, "WI-0007"); got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

// The recorded answer of the Investigator must still pass the check of the definition.
func TestTheRecordedDiagnosisProposesAMitigationThatExists(t *testing.T) {
	a := agentWithRunbooks(t)
	raw, err := os.ReadFile(filepath.Join("replay", a.App.Name+"_investigate_incident.json"))
	if err != nil {
		t.Fatal(err)
	}
	var recorded []struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &recorded); err != nil || len(recorded) == 0 {
		t.Fatalf("the recording has %d answers: %v", len(recorded), err)
	}
	var d diagnosis
	if err := json.Unmarshal([]byte(recorded[0].Content), &d); err != nil {
		t.Fatal(err)
	}
	if d.Component != "api" {
		t.Errorf("the recorded diagnosis names the component %q", d.Component)
	}
	if _, err := a.checkMitigation(d); err != nil {
		t.Errorf("the recorded mitigation is refused: %v", err)
	}
}

// TestTheInvestigatorPromptOffersOnlyMitigationsThatExist compares the prompt of the
// example pack with the definitions of that pack.
func TestTheInvestigatorPromptOffersOnlyMitigationsThatExist(t *testing.T) {
	a := agentWithRunbooks(t)
	definitions, err := a.Orch.Runbooks()
	if err != nil {
		t.Fatal(err)
	}
	prompt := a.App.Render(investigatorSystem)
	mitigations := 0
	for _, d := range definitions {
		named := strings.Contains(prompt, d.Name)
		if named != d.IncidentMitigation {
			t.Errorf("runbook %s: named in the prompt = %v, mitigation in its definition = %v",
				d.Name, named, d.IncidentMitigation)
		}
		if !d.IncidentMitigation {
			continue
		}
		mitigations++
		for _, value := range d.Inputs[runbook.MitigationInput].Values {
			if !strings.Contains(prompt, value+" (the deployment ") {
				t.Errorf("the prompt does not name the component %s of runbook %s", value, d.Name)
			}
		}
	}
	if mitigations == 0 {
		t.Error("no runbook is a mitigation for an incident")
	}
}
