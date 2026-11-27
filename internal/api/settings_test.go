package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/agent"
	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/ollama"
	"github.com/statik/site-reliability-clanker/internal/ollama/ollamatest"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/sim"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// canaryKey stands for the key of a model provider. No answer of the API may hold it, or
// a part of it.
const canaryKey = "sk-canary-7Q4ZKEY"

// theModel is a stand-in for Ollama that holds the model of the control plane.
var theModel = []string{ollama.Variant + ":latest"}

// settingsServer is a demo server with one user of each role. Its local inference is a
// stand-in for Ollama that holds the model of the control plane.
type settingsServer struct {
	handler http.Handler
	log     *audit.Log
	fake    *ollamatest.Fake
	program *ollamatest.Server
}

// held is the models of the stand-in for Ollama. Nil gives a target with no local control.
func withSettings(t *testing.T, llm agent.LLM, held []string) settingsServer {
	t.Helper()
	db := storetest.Open(t)
	if _, err := db.Exec(`INSERT INTO users (name, display, role, email) VALUES
		('carol', 'Carol Reyes', 'admin', 'carol@example.com'),
		('alice', 'Alice Ng', 'approver', 'alice@example.com'),
		('dave', 'Dave Lindqvist', 'viewer', 'dave@example.com')`); err != nil {
		t.Fatal(err)
	}
	clock, err := sim.NewClock(db)
	if err != nil {
		t.Fatal(err)
	}
	found := settingsServer{log: audit.New(db, clock)}
	server := &Server{Orch: &orchestrator.Orchestrator{DB: db}, Audit: found.log,
		Agent: &agent.Agent{LLM: llm, Models: map[string]string{"author": "m-author",
			"explainer": "m-explainer"}}}
	if held != nil {
		found.fake = ollamatest.New(t, held...)
		found.program = &ollamatest.Server{Fake: found.fake}
		server.LocalModel = &ollama.Control{Client: ollama.Client{Base: found.fake.URL()},
			Model: ollama.Variant, Server: found.program, StartWait: 2 * time.Second}
	}
	if found.handler, err = server.Handler(); err != nil {
		t.Fatal(err)
	}
	return found
}

// settingsAnswer is the answer of the three routes of the model settings.
type settingsAnswer struct {
	Mode     string            `json:"mode"`
	Provider string            `json:"provider"`
	Models   map[string]string `json:"models"`
	BaseURL  string            `json:"base_url"`
	Region   string            `json:"region"`
	KeySet   *bool             `json:"key_set"`
	Local    ollama.State      `json:"local"`
	Done     string            `json:"done"`
	Error    string            `json:"error"`
}

func (s settingsServer) call(t *testing.T, method, path, user string) (int, settingsAnswer,
	string) {
	t.Helper()
	request := httptest.NewRequest(method, "/api/v1/settings/model"+path, nil)
	request.Header.Set("X-Clanker-User", user)
	answer := httptest.NewRecorder()
	s.handler.ServeHTTP(answer, request)
	var parsed settingsAnswer
	if err := json.Unmarshal(answer.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("%s %s: %v: %s", method, path, err, answer.Body)
	}
	return answer.Code, parsed, answer.Body.String()
}

func (s settingsServer) modelEvents(t *testing.T) []audit.Event {
	t.Helper()
	events, err := s.log.Query(audit.Filter{TypePrefix: "model."})
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestTheModelSettingsNameTheProviderOfEachMode(t *testing.T) {
	cases := []struct {
		llm      agent.LLM
		mode     string
		provider string
		keySet   bool
		baseURL  string
		region   string
		models   int
	}{
		{agent.NewOpenRouter(canaryKey), "openrouter", "OpenRouter", true, "", "", 2},
		{agent.NewLocal("http://u:"+canaryKey+"@127.0.0.1:11434/v1?key="+canaryKey, canaryKey),
			"local", "Ollama on this machine", true, "http://127.0.0.1:11434/v1", "", 2},
		{agent.NewLocal("http://127.0.0.1:11434/v1", ""), "local", "Ollama on this machine",
			false, "http://127.0.0.1:11434/v1", "", 2},
		{&agent.Bedrock{Region: "us-west-2"}, "bedrock", "Amazon Bedrock", false, "",
			"us-west-2", 2},
		{agent.Replay{}, "replay", "Recorded answers", false, "", "", 0},
	}
	for _, tc := range cases {
		server := withSettings(t, tc.llm, theModel)
		// A viewer has the lowest role, and reads the settings.
		code, got, body := server.call(t, "GET", "", "dave")
		if code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", tc.mode, code, body)
		}
		if got.Mode != tc.mode || got.Provider != tc.provider || got.BaseURL != tc.baseURL ||
			got.Region != tc.region || got.KeySet == nil || *got.KeySet != tc.keySet || len(got.Models) != tc.models {
			t.Errorf("%s: %s", tc.mode, body)
		}
		if strings.Contains(body, "canary") || strings.Contains(body, canaryKey[10:]) {
			t.Errorf("%s: the answer holds the key: %s", tc.mode, body)
		}
		if !got.Local.Available || !got.Local.Running || !got.Local.ModelPresent ||
			got.Local.ModelLoaded || got.Local.Model != ollama.Variant {
			t.Errorf("%s: local = %+v", tc.mode, got.Local)
		}
	}
}

func TestAnAdminStartsAndStopsTheLocalInference(t *testing.T) {
	server := withSettings(t, agent.NewLocal("http://127.0.0.1:11434/v1", canaryKey), theModel)
	server.fake.SetDown(true)
	code, got, body := server.call(t, "POST", "/local/start", "carol")
	if code != http.StatusOK {
		t.Fatalf("start: status %d: %s", code, body)
	}
	if !got.Local.Running || !got.Local.ModelLoaded || !got.Local.StartedBySRC ||
		got.Done != "S.R.C. started the server; the model src-local is loaded" {
		t.Errorf("start: %s", body)
	}
	code, got, stopBody := server.call(t, "POST", "/local/stop", "carol")
	if code != http.StatusOK {
		t.Fatalf("stop: status %d: %s", code, stopBody)
	}
	if got.Local.Running || got.Local.ModelLoaded || got.Local.StartedBySRC || got.Done !=
		"the model src-local is unloaded; the server that S.R.C. started is stopped" {
		t.Errorf("stop: %s", stopBody)
	}
	if strings.Contains(body+stopBody, "canary") {
		t.Errorf("an answer holds the key: %s %s", body, stopBody)
	}
	events := server.modelEvents(t)
	if len(events) != 2 || events[0].Type != "model.local_started" ||
		events[1].Type != "model.local_stopped" {
		t.Fatalf("events = %+v", events)
	}
	for _, event := range events {
		if event.Actor != "carol" || !strings.HasPrefix(event.Summary, "Carol Reyes ") ||
			event.Data["model"] != ollama.Variant || event.Data["done"] == "" {
			t.Errorf("event = %+v", event)
		}
	}
}

func TestStopLeavesAServerThatTheControlPlaneDidNotStart(t *testing.T) {
	server := withSettings(t, agent.Replay{}, theModel)
	server.fake.SetLoaded(ollama.Variant + ":latest")
	code, got, body := server.call(t, "POST", "/local/stop", "carol")
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, body)
	}
	if !got.Local.Running || got.Local.ModelLoaded || server.program.Stops != 0 || got.Done !=
		"the model src-local is unloaded; the server was not started by S.R.C. and still runs" {
		t.Errorf("stop: %s, stops = %d", body, server.program.Stops)
	}
}

func TestAViewerDoesNotStartOrStopTheLocalInference(t *testing.T) {
	server := withSettings(t, agent.Replay{}, theModel)
	server.fake.SetLoaded(ollama.Variant + ":latest")
	for _, path := range []string{"/local/start", "/local/stop"} {
		code, got, _ := server.call(t, "POST", path, "dave")
		if code != http.StatusForbidden ||
			!strings.Contains(got.Error, "needs one of: admin, approver") {
			t.Errorf("POST %s as dave: %d %q, want 403", path, code, got.Error)
		}
	}
	if len(server.fake.Changes()) != 0 || server.program.Starts+server.program.Stops != 0 {
		t.Errorf("a refused request changed the local inference: %v", server.fake.Changes())
	}
	if events := server.modelEvents(t); len(events) != 0 {
		t.Errorf("a refused request wrote audit events: %+v", events)
	}
	// The person of the wizard is an approver, and must be able to use the buttons.
	if code, _, body := server.call(t, "POST", "/local/stop", "alice"); code != http.StatusOK {
		t.Errorf("POST /local/stop as an approver: %d %s", code, body)
	}
}

func TestATargetWithNoLocalControlRefusesStartAndStop(t *testing.T) {
	server := withSettings(t, agent.NewLocal("https://models.example/v1", canaryKey), nil)
	code, got, body := server.call(t, "GET", "", "dave")
	if code != http.StatusOK || got.Local.Available || got.Local.Reason == "" ||
		got.Provider != "A model endpoint of the operator" {
		t.Fatalf("status %d: %s", code, body)
	}
	for _, path := range []string{"/local/start", "/local/stop"} {
		code, refused, _ := server.call(t, "POST", path, "carol")
		if code != http.StatusConflict || refused.Error != got.Local.Reason {
			t.Errorf("POST %s: %d %q, want 409 with the reason", path, code, refused.Error)
		}
		// The role is checked first.
		if code, _, _ := server.call(t, "POST", path, "dave"); code != http.StatusForbidden {
			t.Errorf("POST %s as a viewer: %d, want 403", path, code)
		}
	}
	if events := server.modelEvents(t); len(events) != 0 {
		t.Errorf("a refused request wrote audit events: %+v", events)
	}
}

func TestAModelThatOllamaDoesNotHoldIsARefusalAndNoEvent(t *testing.T) {
	server := withSettings(t, agent.Replay{}, []string{"other:latest"})
	code, got, _ := server.call(t, "POST", "/local/start", "carol")
	if code != http.StatusConflict || !strings.Contains(got.Error, "clanker setup") {
		t.Errorf("start: %d %q, want 409 that names clanker setup", code, got.Error)
	}
	if events := server.modelEvents(t); len(events) != 0 {
		t.Errorf("a refused start wrote audit events: %+v", events)
	}
}
