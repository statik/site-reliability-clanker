package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// runbooksNow is the time of the clock of withRunbooks.
var runbooksNow = time.Date(2026, 11, 21, 2, 0, 0, 0, time.UTC)

// withRunbooks returns the handler of a server whose forge has the runbooks of this
// repository and the extra definitions, and its orchestrator. The user carol is an admin,
// alice an approver and dave a viewer. With identify, people sign in and identify names
// the user; without it, the server is the demo, where the header X-Clanker-User does.
func withRunbooks(t *testing.T, identify func(*http.Request) (orchestrator.User, error),
	extra ...fs.FS) (http.Handler, *orchestrator.Orchestrator) {
	t.Helper()
	db := storetest.Open(t)
	f := forge.New(db)
	if err := f.Seed(orchestrator.RunbooksRepo, packtest.Runbooks(t, extra...),
		"Seed runbooks"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (name, display, role, email) VALUES
		('carol', 'Carol Reyes', 'admin', 'carol@example.com'),
		('alice', 'Alice Ng', 'approver', 'alice@example.com'),
		('dave', 'Dave Lindqvist', 'viewer', 'dave@example.com')`); err != nil {
		t.Fatal(err)
	}
	clock := fixedClock{runbooksNow}
	log := audit.New(db, clock)
	orch := &orchestrator.Orchestrator{DB: db, Forge: f, Audit: log, Clock: clock}
	server := &Server{Forge: f, Orch: orch, Audit: log, Identify: identify}
	if identify != nil {
		server.SignIn = http.NotFoundHandler()
	}
	handler, err := server.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler, orch
}

// send sends a request as the demo user and returns the answer.
func send(handler http.Handler, method, path, user, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("X-Clanker-User", user)
	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, request)
	return answer
}

// eventsOf returns the events of a type, oldest first.
func eventsOf(t *testing.T, orch *orchestrator.Orchestrator, eventType string) []audit.Event {
	t.Helper()
	events, err := orch.Audit.Query(audit.Filter{Types: []string{eventType}})
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestTheRunbookListHasTheDefinitions(t *testing.T) {
	handler, orch := withRunbooks(t, nil)
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
		"standing_signed_at": "",
		"description": "Back up the Paperboat database, check the backup, apply the retention " +
			"policy.",
		"outputs": map[string]any{
			"backup_file": map[string]any{"type": "string",
				"description": "The path of the new backup on the database volume."},
			"objects": map[string]any{"type": "integer", "description": "The number of lines " +
				"in the object list of the backup. A run fails below 5."}}}
	if !reflect.DeepEqual(rows[0], backup) {
		t.Errorf("first row = %#v\nwant %#v", rows[0], backup)
	}
	restart := rows[2]
	got := []any{restart["name"], restart["title"], restart["standing_approver"],
		restart["incident_mitigation"]}
	want := []any{"restart-component", "Restart a component", "", true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("third row has %#v, want %#v", got, want)
	}
	inputs, _ := restart["inputs"].(map[string]any)
	component, _ := inputs["component"].(map[string]any)
	values := []any{"api", "web", "worker"}
	if !reflect.DeepEqual(component["values"], values) {
		t.Errorf("values of component = %#v, want %#v", component["values"], values)
	}
}

func TestARunbookRequestThatDoesNotFitTheDefinitionGetsTheReason(t *testing.T) {
	handler, orch := withRunbooks(t, nil)
	cases := map[string]struct{ path, body, want string }{
		"an unknown runbook": {"/api/v1/runbooks/delete-namespace", `{"env":"local"}`,
			"has no definition"},
		"a missing input": {"/api/v1/runbooks/restart-component", `{"env":"local"}`,
			"needs the input"},
		"a value outside the set": {"/api/v1/runbooks/restart-component",
			`{"env":"local","params":{"component":"postgres"}}`, "is not one of"},
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

func TestAnApproverSignsAndAnAdminRevokesAStandingApproval(t *testing.T) {
	handler, orch := withRunbooks(t, nil)
	path := "/api/v1/runbooks/db-backup/standing-approval"
	answer := send(handler, "POST", path, "alice", "")
	signed := `{"runbook":"db-backup","signer":"alice","signed_at":"2026-11-21T02:00:00.000Z"}`
	if answer.Code != http.StatusOK || strings.TrimSpace(answer.Body.String()) != signed {
		t.Fatalf("sign: status %d, body %s; want 200 with %s", answer.Code, answer.Body, signed)
	}
	var rows []map[string]any
	if err := json.Unmarshal(ask(handler, "GET", "/api/v1/runbooks").Body.Bytes(),
		&rows); err != nil {
		t.Fatal(err)
	}
	if got := []any{rows[0]["name"], rows[0]["standing_approver"],
		rows[0]["standing_signed_at"]}; !reflect.DeepEqual(got,
		[]any{"db-backup", "alice", "2026-11-21T02:00:00.000Z"}) {
		t.Errorf("the list shows %v for the signed runbook", got)
	}
	if events := eventsOf(t, orch, "standing_approval.signed"); len(events) != 1 ||
		events[0].Actor != "alice" {
		t.Errorf("signed events = %+v, want one by alice", events)
	}

	answer = send(handler, "DELETE", path, "carol", "")
	revoked := `{"runbook":"db-backup","signer":"","signed_at":""}`
	if answer.Code != http.StatusOK || strings.TrimSpace(answer.Body.String()) != revoked {
		t.Fatalf("revoke: status %d, body %s; want 200 with %s", answer.Code, answer.Body,
			revoked)
	}
	events := eventsOf(t, orch, "standing_approval.revoked")
	if len(events) != 1 || events[0].Actor != "carol" || events[0].Data["signer"] != "alice" {
		t.Errorf("revoked events = %+v, want one by carol of the signature of alice", events)
	}
	answer = send(handler, "DELETE", path, "carol", "")
	if answer.Code != http.StatusConflict ||
		!strings.Contains(answer.Body.String(), "has no standing approval to revoke") {
		t.Errorf("a second revocation: status %d, body %s; want 409", answer.Code, answer.Body)
	}
}

func TestASignedInPersonSignsAsThemselves(t *testing.T) {
	handler, orch := withRunbooks(t, func(*http.Request) (orchestrator.User, error) {
		return orchestrator.User{Name: "alice", Display: "Alice Ng", Role: "approver"}, nil
	})
	// ask names carol in the header of the demo, which means nothing with a sign-in.
	answer := ask(handler, "POST", "/api/v1/runbooks/restart-component/standing-approval")
	if answer.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", answer.Code, answer.Body)
	}
	if signer, err := orch.StandingApprover("restart-component"); signer != "alice" ||
		err != nil {
		t.Errorf("signer = %q, %v; want alice", signer, err)
	}
}

func TestAStandingApprovalThatTheRulesDoNotAllowIsRefused(t *testing.T) {
	handler, orch := withRunbooks(t, nil, fstest.MapFS{
		"drop-database.json": {Data: []byte(`{"description":"Drop the database.","tier":"T3"}`)},
		"drop-database.sh":   {Data: []byte("#!/bin/bash\n"), Mode: 0o755},
	})
	if err := orch.SignStandingApproval("restart-component", "carol"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		method, runbook, user string
		status                int
		want                  string
	}{
		"a viewer signs": {"POST", "db-backup", "dave", http.StatusForbidden, "needs one of"},
		"a viewer revokes": {"DELETE", "restart-component", "dave", http.StatusForbidden,
			"needs one of"},
		"a runbook that does not exist": {"POST", "delete-namespace", "carol",
			http.StatusBadRequest, "has no definition"},
		"a runbook whose tier needs more": {"POST", "drop-database", "carol",
			http.StatusBadRequest, "a standing approval covers only tier T1"},
		"a name that is not valid": {"DELETE", "Restart", "carol", http.StatusBadRequest,
			"is not valid"},
	}
	for label, tc := range cases {
		answer := send(handler, tc.method, "/api/v1/runbooks/"+tc.runbook+"/standing-approval",
			tc.user, "")
		if answer.Code != tc.status || !strings.Contains(answer.Body.String(), tc.want) {
			t.Errorf("%s: status %d, body %s; want %d with %q", label, answer.Code, answer.Body,
				tc.status, tc.want)
		}
	}
	for runbook, want := range map[string]string{"db-backup": "", "drop-database": "",
		"restart-component": "carol"} {
		if signer, err := orch.StandingApprover(runbook); signer != want || err != nil {
			t.Errorf("%s has the standing approval of %q, %v; want %q", runbook, signer, err,
				want)
		}
	}
}

func TestARunbookWithNoStandingApprovalIsRefusedAtOnce(t *testing.T) {
	handler, orch := withRunbooks(t, nil)
	answer := send(handler, "POST", "/api/v1/runbooks/db-backup", "carol", `{"env":"local"}`)
	if answer.Code != http.StatusConflict ||
		!strings.Contains(answer.Body.String(), "an approver must sign one first") {
		t.Errorf("status %d, body %s; want 409 with the reason", answer.Code, answer.Body)
	}
	var items int
	if err := orch.DB.QueryRow(`SELECT COUNT(*) FROM work_items`).Scan(&items); err != nil ||
		items != 0 {
		t.Errorf("%d work items after the refusal, %v", items, err)
	}
}
