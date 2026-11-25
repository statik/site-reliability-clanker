package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/ollama"
)

func TestLocalModelControlFollowsTheSettingsOfTheModel(t *testing.T) {
	cases := []struct {
		name      string
		settings  map[string]string
		base      string
		model     string
		available bool
	}{
		{"no endpoint", map[string]string{"OPENROUTER_API_KEY": "sk-or-x"},
			"http://127.0.0.1:11434", "src-local", true},
		{"the endpoint of the wizard", map[string]string{
			"CLANKER_LLM_BASE_URL": "http://127.0.0.1:11434/v1", "CLANKER_LLM_MODEL": "src-local"},
			"http://127.0.0.1:11434", "src-local", true},
		{"another port and model on this machine", map[string]string{
			"CLANKER_LLM_BASE_URL": "http://localhost:9999/v1", "CLANKER_LLM_MODEL": "mine"},
			"http://localhost:9999", "mine", true},
		{"an endpoint on another machine", map[string]string{
			"CLANKER_LLM_BASE_URL": "https://models.example/v1", "CLANKER_LLM_MODEL": "big"},
			"https://models.example", "big", false},
		{"an endpoint on another address of a network", map[string]string{
			"CLANKER_LLM_BASE_URL": "http://10.0.0.5:11434/v1", "CLANKER_LLM_MODEL": "big"},
			"http://10.0.0.5:11434", "big", false},
	}
	for _, tc := range cases {
		control := localModelControl(environment(tc.settings), t.TempDir())
		if control.Client.Base != tc.base || control.Model != tc.model {
			t.Errorf("%s: base %q, model %q", tc.name, control.Client.Base, control.Model)
		}
		if got := control.Server != nil; got != tc.available {
			t.Errorf("%s: has a server = %t, want %t", tc.name, got, tc.available)
		}
		if !tc.available && !strings.Contains(control.Reason, "is not on this machine") {
			t.Errorf("%s: reason = %q", tc.name, control.Reason)
		}
		if server, isServer := control.Server.(*ollamaServer); isServer &&
			"http://"+server.listen != tc.base {
			t.Errorf("%s: the program listens on %q", tc.name, server.listen)
		}
	}
}

func TestAnEndpointThatIsNotAnAddressGetsNoControlAndNoError(t *testing.T) {
	// The control plane started with such a value before, for example in the mode replay.
	for _, endpoint := range []string{"127.0.0.1:11434", "not an address", "/v1"} {
		control := localModelControl(environment(map[string]string{
			"CLANKER_LLM_BASE_URL": endpoint, "CLANKER_LLM_MODEL": "mine"}), t.TempDir())
		if control.Server != nil || control.Model != "mine" ||
			!strings.Contains(control.Reason, "CLANKER_LLM_BASE_URL is not an address") {
			t.Errorf("%q: server %v, reason %q", endpoint, control.Server, control.Reason)
		}
	}
}

func TestTheModelServerGetsNoSecretOfTheControlPlane(t *testing.T) {
	got := serverEnvironment([]string{"PATH=/bin", "OPENROUTER_API_KEY=sk-or-x",
		"CLANKER_LLM_API_KEY=k", "CLANKER_SIGNING_KEY=s", "OLLAMA_HOST=0.0.0.0:1",
		"OLLAMA_MODELS=/models"}, "127.0.0.1:11434")
	want := []string{"PATH=/bin", "OLLAMA_MODELS=/models", "OLLAMA_HOST=127.0.0.1:11434",
		"OLLAMA_KEEP_ALIVE=-1"}
	if !slices.Equal(got, want) {
		t.Errorf("environment = %v, want %v", got, want)
	}
}

// standIn writes a program that takes the place of `ollama serve`: it writes its
// arguments and its address to its output, and runs until a signal.
func standIn(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ollama-stand-in")
	script := "#!/bin/sh\necho \"$1 $OLLAMA_HOST $OLLAMA_KEEP_ALIVE\"\nexec sleep 60\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheModelServerStartsAndStopsItsOwnProgram(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "logs", "ollama.log")
	server := &ollamaServer{program: standIn(t), listen: "127.0.0.1:1", logPath: logPath}
	if server.Started() {
		t.Fatal("a server that never started says that it started")
	}
	if err := server.Stop(); err != nil {
		t.Fatalf("stop with no program: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Stop() })
	if !server.Started() {
		t.Fatal("the program does not run after the start")
	}
	first := server.cmd.Process.Pid
	if err := server.Start(); err != nil || server.cmd.Process.Pid != first {
		t.Errorf("a second start made a second program: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		log, _ := os.ReadFile(logPath)
		if string(log) == "serve 127.0.0.1:1 -1\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the log of the program is %q", log)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := server.Stop(); err != nil {
		t.Fatal(err)
	}
	if server.Started() {
		t.Error("the program runs after the stop")
	}
	if err := server.cmd.Process.Signal(os.Interrupt); err == nil {
		t.Error("the program still takes a signal after the stop")
	}
}

func TestTheModelServerReportsAProgramThatIsNotInstalled(t *testing.T) {
	server := &ollamaServer{program: filepath.Join(t.TempDir(), "no-ollama"),
		listen: "127.0.0.1:1", logPath: filepath.Join(t.TempDir(), "ollama.log")}
	err := server.Start()
	if err == nil || !strings.Contains(err.Error(), "install Ollama from ollama.com") {
		t.Fatalf("err = %v", err)
	}
	if server.Started() {
		t.Error("a program that is not there runs")
	}
}

// controlPlane is a stand-in for the API of a control plane. It answers every route of
// the model settings with one status and body, and records the requests.
type controlPlane struct {
	status   int
	body     string
	requests []*http.Request
}

func (c *controlPlane) serve(t *testing.T) string {
	t.Helper()
	// The tokens of the person who runs the tests must not go to the stand-in.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.requests = append(c.requests, r)
		w.WriteHeader(c.status)
		_, _ = w.Write([]byte(c.body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func modelCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runModel(context.Background(), args, &out)
	return out.String(), err
}

const localSettings = `{"mode":"local","provider":"Ollama on this machine",
	"models":{"reviewer":"src-local","author":"src-local"},
	"base_url":"http://127.0.0.1:11434/v1","key_set":false,
	"local":{"available":true,"url":"http://127.0.0.1:11434","running":true,
		"version":"0.40.16","model":"src-local","model_present":true,"model_loaded":true,
		"started_by_src":true},
	"done":"S.R.C. started the server; the model src-local is loaded"}`

func TestModelStatusPrintsOneLineForEachFact(t *testing.T) {
	plane := &controlPlane{status: http.StatusOK, body: localSettings}
	out, err := modelCommand(t, "status", "--addr", plane.serve(t))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := `ok    mode: local
ok    provider: Ollama on this machine
ok    model of the role author: src-local
ok    model of the role reviewer: src-local
ok    endpoint: http://127.0.0.1:11434/v1
ok    key: not set
ok    local inference: available
ok    server: runs at http://127.0.0.1:11434, version 0.40.16, started by S.R.C.
ok    model src-local: in memory
ok    done: S.R.C. started the server; the model src-local is loaded
`
	if out != want {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}
	request := plane.requests[0]
	if request.Method != http.MethodGet || request.URL.Path != "/api/v1/settings/model" {
		t.Errorf("request = %s %s", request.Method, request.URL.Path)
	}
	if request.Header.Get("Authorization") != "" || request.Header.Get("X-Clanker-User") != "" {
		t.Errorf("a request with no token and no user has the headers %v", request.Header)
	}
}

func TestModelStatusSaysWhyTheLocalInferenceIsNotAvailable(t *testing.T) {
	plane := &controlPlane{status: http.StatusOK, body: `{"mode":"openrouter",
		"provider":"OpenRouter","models":{},"key_set":true,
		"local":{"available":false,"reason":"no program starts here"}}`}
	out, err := modelCommand(t, "status", "--addr", plane.serve(t))
	want := "ok    mode: openrouter\nok    provider: OpenRouter\nok    key: set\n" +
		"ok    local inference: not available: no program starts here\n"
	if err != nil || out != want {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestModelStartAndStopPostAsTheUserOfTheFlag(t *testing.T) {
	for command, path := range map[string]string{
		"start": "/api/v1/settings/model/local/start",
		"stop":  "/api/v1/settings/model/local/stop"} {
		plane := &controlPlane{status: http.StatusOK, body: localSettings}
		// The address works without its scheme too.
		address := strings.TrimPrefix(plane.serve(t), "http://")
		out, err := modelCommand(t, command, "--addr", address, "--user", "carol")
		if err != nil || !strings.Contains(out, "ok    done: S.R.C. started the server") {
			t.Fatalf("%s: %v\n%s", command, err, out)
		}
		request := plane.requests[0]
		if request.Method != http.MethodPost || request.URL.Path != path ||
			request.Header.Get("X-Clanker-User") != "carol" {
			t.Errorf("%s: request = %s %s as %q", command, request.Method, request.URL.Path,
				request.Header.Get("X-Clanker-User"))
		}
	}
}

func TestModelCommandsSendTheTokenOfThisMachine(t *testing.T) {
	plane := &controlPlane{status: http.StatusOK, body: localSettings}
	address := plane.serve(t)
	if err := writeCredentials(map[string]savedToken{
		address: {Token: "token-of-this-machine", Expires: time.Now().Add(time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	if out, err := modelCommand(t, "status", "--addr", address); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := plane.requests[0].Header.Get("Authorization"); got != "Bearer token-of-this-machine" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestModelCommandsFailWhenTheControlPlaneRefuses(t *testing.T) {
	plane := &controlPlane{status: http.StatusForbidden,
		body: `{"error":"dave has role viewer; this action needs one of: admin"}`}
	address := plane.serve(t)
	out, err := modelCommand(t, "start", "--addr", address)
	want := "FAIL  dave has role viewer; this action needs one of: admin. On a control " +
		"plane with no sign-in, give an admin with --user NAME\n"
	if err == nil || out != want {
		t.Errorf("err = %v\n%s", err, out)
	}
	plane.status, plane.body = http.StatusConflict, `{"error":"no program starts here"}`
	out, err = modelCommand(t, "stop", "--addr", address, "--user", "carol")
	if err == nil || out != "FAIL  no program starts here\n" {
		t.Errorf("err = %v\n%s", err, out)
	}
	out, err = modelCommand(t, "stop", "--addr", address, "--json")
	if err == nil || out != plane.body+"\n" {
		t.Errorf("with --json: err = %v\n%s", err, out)
	}
}

func TestModelCommandsFailWhenNoControlPlaneAnswers(t *testing.T) {
	plane := &controlPlane{status: http.StatusOK, body: "<html>not a control plane</html>"}
	address := plane.serve(t)
	out, err := modelCommand(t, "status", "--addr", address)
	if err == nil || !strings.HasPrefix(out, "FAIL  "+address+" answered 200 and no JSON") {
		t.Errorf("a page that is not JSON: err = %v\n%s", err, out)
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	out, err = modelCommand(t, "status", "--addr", closed.URL)
	if err == nil || !strings.HasPrefix(out, "FAIL  ask the control plane at "+closed.URL) {
		t.Errorf("no server: err = %v\n%s", err, out)
	}
}

func TestModelStatusPrintsTheAnswerAsJSON(t *testing.T) {
	plane := &controlPlane{status: http.StatusOK, body: localSettings}
	out, err := modelCommand(t, "status", "--addr", plane.serve(t), "--json")
	if err != nil || out != localSettings+"\n" {
		t.Errorf("err = %v\n%s", err, out)
	}
}

func TestModelRefusesACommandThatDoesNotExist(t *testing.T) {
	for _, args := range [][]string{{}, {"restart"}, {"status", "extra"}} {
		if out, err := modelCommand(t, args...); err == nil || out != "" ||
			!strings.Contains(err.Error(), "clanker model") {
			t.Errorf("%v: err = %v, out = %q", args, err, out)
		}
	}
	if out, err := modelCommand(t, "help"); err != nil || out != modelUsage {
		t.Errorf("help: %v\n%s", err, out)
	}
}

func TestTheLinesOfAServerThatDoesNotAnswer(t *testing.T) {
	got := localFacts(ollama.State{Available: true, URL: "http://127.0.0.1:11434",
		Model: "src-local"})
	want := []string{"local inference: available",
		"server: does not answer at http://127.0.0.1:11434",
		"model src-local: not known while the server does not answer"}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %q", got)
	}
	got = localFacts(ollama.State{Available: true, URL: "u", Running: true, Version: "1",
		Model: "m", ModelPresent: true})
	if got[2] != "model m: on the server, not in memory" {
		t.Errorf("lines = %q", got)
	}
	got = localFacts(ollama.State{Available: true, URL: "u", Running: true, Model: "m"})
	if got[2] != "model m: not on the server" {
		t.Errorf("lines = %q", got)
	}
}
