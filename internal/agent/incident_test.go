package agent

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// scaleDown takes a deployment as its input, as the restart does, and is not marked as a
// mitigation for an incident.
var scaleDown = fstest.MapFS{
	"scale-down.json": {Data: []byte(`{"description":"Scale a deployment to zero.","tier":"T1",
		"inputs":{"deployment":{"type":"string","description":"The deployment.",
		"values":["paperboat-api"]}}}`)},
}

func agentWithRunbooks(t *testing.T) *Agent {
	t.Helper()
	db := storetest.Open(t)
	f := forge.New(db)
	if err := f.Seed(orchestrator.RunbooksRepo, map[string]fs.FS{
		".": os.DirFS("../../runbooks"), "jobs": scaleDown}, "Seed runbooks"); err != nil {
		t.Fatal(err)
	}
	return &Agent{Orch: &orchestrator.Orchestrator{DB: db, Forge: f}}
}

func TestTheDefinitionDecidesWhichMitigationRuns(t *testing.T) {
	a := agentWithRunbooks(t)
	for _, deployment := range []string{"paperboat-api", "paperboat-web", "paperboat-worker"} {
		err := a.checkMitigation(diagnosis{ProposedRunbook: "restart-deployment",
			Deployment: deployment})
		if err != nil {
			t.Errorf("restart of %s: %v", deployment, err)
		}
	}
	refused := map[string]diagnosis{
		"no mitigation": {ProposedRunbook: "none"},
		"no answer":     {},
		"a deployment outside the set": {ProposedRunbook: "restart-deployment",
			Deployment: "postgres"},
		"no deployment": {ProposedRunbook: "restart-deployment"},
		"a runbook that does not exist": {ProposedRunbook: "db-drop-and-restore",
			Deployment: "paperboat-api"},
		"a runbook that is not a mitigation": {ProposedRunbook: "db-backup"},
		"a runbook with the same input that is not a mitigation": {
			ProposedRunbook: "scale-down", Deployment: "paperboat-api"},
		"text that is not a name": {ProposedRunbook: "restart-deployment; db-backup",
			Deployment: "paperboat-api"},
	}
	for label, d := range refused {
		if err := a.checkMitigation(d); !errors.Is(err, orchestrator.ErrInvalid) {
			t.Errorf("%s: error = %v, want a refusal that sends the incident to a human", label,
				err)
		}
	}
}

// TestTheInvestigatorPromptOffersOnlyMitigationsThatExist compares the fixed text of the
// prompt with the definitions in runbooks/jobs of this repository.
func TestTheInvestigatorPromptOffersOnlyMitigationsThatExist(t *testing.T) {
	a := agentWithRunbooks(t)
	definitions, err := a.Orch.Runbooks()
	if err != nil {
		t.Fatal(err)
	}
	mitigations := 0
	for _, d := range definitions {
		named := strings.Contains(investigatorSystem, d.Name)
		if named != d.IncidentMitigation {
			t.Errorf("runbook %s: named in the prompt = %v, mitigation in its definition = %v",
				d.Name, named, d.IncidentMitigation)
		}
		if !d.IncidentMitigation {
			continue
		}
		mitigations++
		for _, value := range d.Inputs["deployment"].Values {
			if !strings.Contains(investigatorSystem, value) {
				t.Errorf("the prompt does not name the deployment %s of runbook %s", value, d.Name)
			}
		}
	}
	if mitigations == 0 {
		t.Error("no runbook is a mitigation for an incident")
	}
}
