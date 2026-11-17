package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// withRunbooks returns the handler of a demo server whose forge has the runbooks of this
// repository, and its orchestrator. The user carol is an admin.
func withRunbooks(t *testing.T) (http.Handler, *orchestrator.Orchestrator) {
	t.Helper()
	db := storetest.Open(t)
	f := forge.New(db)
	if err := f.Seed(orchestrator.RunbooksRepo, map[string]fs.FS{
		".": os.DirFS("../../runbooks")}, "Seed runbooks"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (name, display, role, email) VALUES
		('carol', 'Carol Reyes', 'admin', 'carol@example.com')`); err != nil {
		t.Fatal(err)
	}
	orch := &orchestrator.Orchestrator{DB: db, Forge: f}
	handler, err := (&Server{Forge: f, Orch: orch}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler, orch
}

func TestTheRunbookListHasTheDefinitions(t *testing.T) {
	handler, orch := withRunbooks(t)
	if err := store.SetKV(orch.DB, "standing_approval.db-backup", "carol"); err != nil {
		t.Fatal(err)
	}
	answer := ask(handler, "GET", "/api/v1/runbooks")
	if answer.Code != http.StatusOK {
		t.Fatalf("status %d: %s", answer.Code, answer.Body)
	}
	var rows []map[string]any
	if err := json.Unmarshal(answer.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("%d rows: %s", len(rows), answer.Body)
	}
	backup := map[string]any{"name": "db-backup", "tier": "T1", "standing_approver": "carol",
		"description": "Take a logical backup of the Paperboat database, check that it can be " +
			"listed, and delete the backups that are older than the retention policy.",
		"outputs": map[string]any{
			"backup_file": map[string]any{"type": "string",
				"description": "The path of the new backup on the database volume."},
			"objects": map[string]any{"type": "integer", "description": "The number of lines " +
				"in the object list of the backup. A run fails below 5."}}}
	if !reflect.DeepEqual(rows[0], backup) {
		t.Errorf("first row = %#v\nwant %#v", rows[0], backup)
	}
	restart := rows[2]
	got := []any{restart["name"], restart["standing_approver"], restart["incident_mitigation"]}
	if want := []any{"restart-deployment", "", true}; !reflect.DeepEqual(got, want) {
		t.Errorf("third row has %#v, want %#v", got, want)
	}
	inputs, _ := restart["inputs"].(map[string]any)
	deployment, _ := inputs["deployment"].(map[string]any)
	want := []any{"paperboat-api", "paperboat-web", "paperboat-worker"}
	if !reflect.DeepEqual(deployment["values"], want) {
		t.Errorf("values of deployment = %#v, want %#v", deployment["values"], want)
	}
}

func TestARunbookRequestThatDoesNotFitTheDefinitionGetsTheReason(t *testing.T) {
	handler, orch := withRunbooks(t)
	cases := map[string]struct{ path, body, want string }{
		"an unknown runbook": {"/api/v1/runbooks/delete-namespace", `{"env":"local"}`,
			"has no definition"},
		"a missing input": {"/api/v1/runbooks/restart-deployment", `{"env":"local"}`,
			"needs the input"},
		"a value outside the set": {"/api/v1/runbooks/restart-deployment",
			`{"env":"local","params":{"deployment":"postgres"}}`, "is not one of"},
		"an input that is not declared": {"/api/v1/runbooks/db-backup",
			`{"env":"local","params":{"file":"/etc/passwd"}}`, "has no input"},
	}
	for label, tc := range cases {
		request := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		request.Header.Set("X-Clanker-User", "carol")
		answer := httptest.NewRecorder()
		handler.ServeHTTP(answer, request)
		if answer.Code != http.StatusBadRequest || !strings.Contains(answer.Body.String(), tc.want) {
			t.Errorf("%s: status %d, body %s; want 400 with %q", label, answer.Code, answer.Body,
				tc.want)
		}
	}
	var items int
	if err := orch.DB.QueryRow(`SELECT COUNT(*) FROM work_items`).Scan(&items); err != nil ||
		items != 0 {
		t.Errorf("%d work items after the refusals, %v", items, err)
	}
}
