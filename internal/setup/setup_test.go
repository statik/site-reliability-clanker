package setup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCLI records the calls and answers from a table keyed by the first two words.
type fakeCLI struct {
	calls   [][]string
	answers map[string]string
	errors  map[string]error
	onCall  func(args []string)
}

func (f *fakeCLI) run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	if f.onCall != nil {
		f.onCall(args)
	}
	key := args[0] + " " + args[1]
	if err := f.errors[key]; err != nil {
		return nil, err
	}
	return []byte(f.answers[key]), nil
}

func (f *fakeCLI) called(key string) bool {
	for _, call := range f.calls {
		if call[0]+" "+call[1] == key {
			return true
		}
	}
	return false
}

func newServer(t *testing.T, cli *fakeCLI) (*Server, http.Handler) {
	t.Helper()
	server, err := New(context.Background(), Config{DataDir: t.TempDir(), Version: "v9.9.9",
		DashboardURL: "http://127.0.0.1:1", CLI: cli.run})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := server.Handler("127.0.0.1:7069")
	if err != nil {
		t.Fatal(err)
	}
	return server, handler
}

func request(t *testing.T, handler http.Handler, token, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, "http://127.0.0.1:7069"+path, strings.NewReader(body))
	req.Header.Set("X-Setup-Token", token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	var decoded map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
	return recorder.Code, decoded
}

func TestAPIRefusesARequestWithoutTheToken(t *testing.T) {
	server, handler := newServer(t, &fakeCLI{})
	for _, token := range []string{"", "wrong", server.Token() + "x"} {
		status, body := request(t, handler, token, http.MethodGet, "/api/state", "")
		if status != http.StatusUnauthorized {
			t.Fatalf("token %q: status %d, want 401", token, status)
		}
		if !strings.Contains(body["error"].(string), "clanker setup") {
			t.Fatalf("token %q: the error does not say what to do: %v", token, body["error"])
		}
	}
	if status, _ := request(t, handler, server.Token(), http.MethodGet, "/api/state", ""); status != 200 {
		t.Fatalf("right token: status %d, want 200", status)
	}
}

func TestHandlerRefusesAnotherHostName(t *testing.T) {
	server, handler := newServer(t, &fakeCLI{})
	for host, want := range map[string]int{"127.0.0.1:7069": 200, "localhost:7069": 200,
		"evil.example:7069": 403, "127.0.0.1:9999": 403} {
		req := httptest.NewRequest(http.MethodGet, "http://placeholder/api/state", nil)
		req.Host = host
		req.Header.Set("X-Setup-Token", server.Token())
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != want {
			t.Errorf("host %s: status %d, want %d", host, recorder.Code, want)
		}
	}
}

func TestPagesAreServedWithoutTheToken(t *testing.T) {
	_, handler := newServer(t, &fakeCLI{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7069/", nil))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "app.js") {
		t.Fatalf("the wizard page was not served: %d", recorder.Code)
	}
}

func TestTargetIsValidated(t *testing.T) {
	server, handler := newServer(t, &fakeCLI{})
	cases := map[string]int{
		`{"target":"gcp","env":"prod-1"}`:    400,
		`{"target":"aws","env":"X"}`:         400,
		`{"target":"aws","env":"bad name"}`:  400,
		`{"target":"aws","env":"prod-1"}`:    200,
		`{"target":"local","env":"ignored"}`: 200,
		`not json`:                           400,
	}
	for body, want := range cases {
		if status, _ := request(t, handler, server.Token(), http.MethodPost, "/api/target", body); status != want {
			t.Errorf("%s: status %d, want %d", body, status, want)
		}
	}
	if server.state.Env != "local" && server.state.Env != "prod-1" {
		t.Fatalf("env = %q", server.state.Env)
	}
}

func TestStateIsSavedWithoutTheKey(t *testing.T) {
	server, handler := newServer(t, &fakeCLI{})
	token := server.Token()
	const key = "sk-or-v1-0123456789abcdef0123456789"
	request(t, handler, token, http.MethodPost, "/api/target", `{"target":"aws","env":"prod-1"}`)
	if status, _ := request(t, handler, token, http.MethodPost, "/api/model",
		`{"choice":"live","key":"`+key+`"}`); status != 200 {
		t.Fatalf("key: status %d", status)
	}
	saved, err := os.ReadFile(filepath.Join(server.cfg.DataDir, "setup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), key) {
		t.Fatal("the saved state contains the model key")
	}
	if !strings.Contains(string(saved), `"key_choice": "live"`) {
		t.Fatalf("the saved state does not record the choice: %s", saved)
	}
	_, state := request(t, handler, token, http.MethodGet, "/api/state", "")
	if raw, _ := json.Marshal(state); strings.Contains(string(raw), key) {
		t.Fatal("the state answer contains the model key")
	}
}

func TestKeyThatIsNotAKeyIsRefused(t *testing.T) {
	server, handler := newServer(t, &fakeCLI{})
	for _, key := range []string{"", "short", `"sk-or-v1-0123456789abcdef0123456789"`, "two words that are long enough"} {
		body, _ := json.Marshal(map[string]string{"choice": "live", "key": key})
		if status, _ := request(t, handler, server.Token(), http.MethodPost, "/api/model", string(body)); status != 400 {
			t.Errorf("key %q: status %d, want 400", key, status)
		}
	}
}

func TestSignInAsksForLoginWhenTheSessionExpired(t *testing.T) {
	cli := &fakeCLI{errors: map[string]error{
		"sts get-caller-identity": errors.New("Error when retrieving token from sso: Token has expired")}}
	server, handler := newServer(t, cli)
	request(t, handler, server.Token(), http.MethodPost, "/api/target", `{"target":"aws","env":"prod-1"}`)
	status, _ := request(t, handler, server.Token(), http.MethodPost, "/api/aws/signin",
		`{"profile":"sandbox","region":"us-east-1"}`)
	if status != http.StatusConflict {
		t.Fatalf("status %d, want 409", status)
	}
	if server.state.Account != "" {
		t.Fatal("the account was saved although the sign-in failed")
	}
}

func TestSignInRefusesNamesThatAreNotNames(t *testing.T) {
	cli := &fakeCLI{}
	server, handler := newServer(t, cli)
	request(t, handler, server.Token(), http.MethodPost, "/api/target", `{"target":"aws","env":"prod-1"}`)
	for _, body := range []string{`{"profile":"a b","region":"us-east-1"}`,
		`{"profile":"ok","region":"us-east-1; rm"}`, `{"profile":"--debug x","region":"us-east-1"}`} {
		if status, _ := request(t, handler, server.Token(), http.MethodPost, "/api/aws/signin", body); status != 400 {
			t.Errorf("%s: status %d, want 400", body, status)
		}
	}
	if len(cli.calls) != 0 {
		t.Fatalf("the CLI ran for a name that is not valid: %v", cli.calls)
	}
}

func TestDeployNeedsAReview(t *testing.T) {
	server, handler := newServer(t, &fakeCLI{})
	request(t, handler, server.Token(), http.MethodPost, "/api/target", `{"target":"local"}`)
	status, body := request(t, handler, server.Token(), http.MethodPost, "/api/deploy", "")
	if status != 400 || !strings.Contains(body["error"].(string), "review") {
		t.Fatalf("status %d, error %v", status, body["error"])
	}
}

func TestChecksReportWhatIsMissingAndHowToFixIt(t *testing.T) {
	oldLook, oldDocker := LookPath, DockerAnswers
	defer func() { LookPath, DockerAnswers = oldLook, oldDocker }()
	LookPath = func(name string) (string, error) {
		if name == "kind" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	DockerAnswers = func() bool { return false }
	checks, err := Checks(TargetLocal)
	if err != nil {
		t.Fatal(err)
	}
	missing := Missing(checks)
	if len(missing) != 2 || missing[0] != "kind" || missing[1] != "Docker daemon" {
		t.Fatalf("missing = %v", missing)
	}
	for _, check := range checks {
		if !check.OK && check.Fix == "" {
			t.Errorf("%s is missing and has no fix", check.Name)
		}
	}
	awsChecks, _ := Checks(TargetAWS)
	if len(Missing(awsChecks)) != 0 {
		t.Fatalf("the AWS target must not need kind or Docker: %v", Missing(awsChecks))
	}
	if _, err := Checks("mars"); err == nil {
		t.Fatal("no error for a target that does not exist")
	}
}

func TestStateSaysWhenTheAppShowsTheWizard(t *testing.T) {
	for _, inApp := range []bool{false, true} {
		server, err := New(context.Background(), Config{DataDir: t.TempDir(),
			DashboardURL: "http://127.0.0.1:1", InApp: inApp})
		if err != nil {
			t.Fatal(err)
		}
		handler, err := server.Handler("127.0.0.1:7069")
		if err != nil {
			t.Fatal(err)
		}
		_, state := request(t, handler, server.Token(), http.MethodGet, "/api/state", "")
		if state["in_app"] != inApp {
			t.Errorf("in_app is %v, want %v", state["in_app"], inApp)
		}
	}
}
