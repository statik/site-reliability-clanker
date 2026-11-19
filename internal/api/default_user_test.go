package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// me asks who the server takes the request for, with the name in the demo header.
func me(t *testing.T, handler http.Handler, name string) (int, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	request.Header.Set("X-Clanker-User", name)
	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, request)
	var user orchestrator.User
	_ = json.Unmarshal(answer.Body.Bytes(), &user)
	return answer.Code, user.Name
}

func TestAUserThatDoesNotExistActsAsTheDefaultUser(t *testing.T) {
	db := storetest.Open(t)
	if _, err := db.Exec(`INSERT INTO users (name, display, role, email) VALUES
		('sam', 'Sam Lee', 'approver', 'sam@example.com'),
		('bob', 'Bob Okafor', 'approver', 'bob@example.com')`); err != nil {
		t.Fatal(err)
	}
	orch := &orchestrator.Orchestrator{DB: db}
	strict, err := (&Server{Orch: orch}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := me(t, strict, "alice"); code != http.StatusForbidden {
		t.Errorf("with no default user, alice got %d, want 403", code)
	}
	handler, err := (&Server{Orch: orch, DefaultUser: "sam"}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"alice": "sam", "bob": "bob", "sam": "sam"} {
		if code, got := me(t, handler, name); code != http.StatusOK || got != want {
			t.Errorf("the header %s gave %d and the user %q, want %q", name, code, got, want)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth", nil)
	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, request)
	var auth struct {
		SignIn      bool   `json:"sign_in"`
		DefaultUser string `json:"default_user"`
	}
	if err := json.Unmarshal(answer.Body.Bytes(), &auth); err != nil || auth.SignIn ||
		auth.DefaultUser != "sam" {
		t.Errorf("auth = %s, %v", answer.Body, err)
	}
}
