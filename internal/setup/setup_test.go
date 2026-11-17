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
		ReleaseBase: "http://127.0.0.1:1", DashboardURL: "http://127.0.0.1:1", CLI: cli.run})
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
	if status, _ := request(t, handler, token, http.MethodPost, "/api/key", `{"key":"`+key+`"}`); status != 200 {
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
	for _, key := range []string{"short", `"sk-or-v1-0123456789abcdef0123456789"`, "two words that are long enough"} {
		body, _ := json.Marshal(map[string]string{"key": key})
		if status, _ := request(t, handler, server.Token(), http.MethodPost, "/api/key", string(body)); status != 400 {
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

func account(t *testing.T, cli *fakeCLI) AWS {
	t.Helper()
	return AWS{Profile: "sandbox", Region: "us-east-1", Env: "prod-1", Run: cli.run, WorkDir: t.TempDir()}
}

func TestPutKeyKeepsTheKeyOffTheCommandLine(t *testing.T) {
	const key = "sk-or-v1-0123456789abcdef0123456789"
	var fileBody string
	var fileMode os.FileMode
	var filePath string
	cli := &fakeCLI{onCall: func(args []string) {
		for _, arg := range args {
			if strings.HasPrefix(arg, "file://") {
				filePath = strings.TrimPrefix(arg, "file://")
				data, _ := os.ReadFile(filePath)
				info, _ := os.Stat(filePath)
				fileBody, fileMode = string(data), info.Mode().Perm()
			}
		}
	}}
	if err := account(t, cli).PutKey(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(cli.calls[0], " "), key) {
		t.Fatal("the key is on the command line")
	}
	if !strings.Contains(fileBody, key) || !strings.Contains(fileBody, "SecureString") {
		t.Fatalf("the input file is wrong: %s", fileBody)
	}
	if fileMode != 0o600 {
		t.Fatalf("the input file has mode %o, want 600", fileMode)
	}
	if _, err := os.Stat(filePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the input file with the key was not removed")
	}
}

func TestReviewReturnsTheResourcesOfTheChangeSet(t *testing.T) {
	cli := &fakeCLI{answers: map[string]string{"cloudformation describe-change-set": `{"Changes":[
		{"ResourceChange":{"Action":"Add","LogicalResourceId":"Machine","ResourceType":"AWS::EC2::Instance"}},
		{"ResourceChange":{"Action":"Add","LogicalResourceId":"Role","ResourceType":"AWS::IAM::Role"}}]}`}}
	binary := Binary{URL: "https://example.com/clanker-linux-arm64", SHA256: strings.Repeat("a", 64)}
	changes, err := account(t, cli).Review(context.Background(), binary, SizeOf("medium"), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].Type != "AWS::EC2::Instance" || changes[1].ID != "Role" {
		t.Fatalf("changes = %+v", changes)
	}
	var create []string
	for _, call := range cli.calls {
		if call[1] == "create-change-set" {
			create = call
		}
	}
	joined := strings.Join(create, " ")
	for _, want := range []string{"--change-set-type CREATE", "clanker-prod-1-control",
		"ParameterKey=BinarySha256,ParameterValue=" + binary.SHA256, "--profile sandbox",
		"ParameterKey=HasModelKey,ParameterValue=true", "ParameterKey=Cpu,ParameterValue=1024",
		"ParameterKey=Memory,ParameterValue=2048"} {
		if !strings.Contains(joined, want) {
			t.Errorf("create-change-set lacks %q: %s", want, joined)
		}
	}
	if cli.called("cloudformation execute-change-set") {
		t.Fatal("the review executed the change set")
	}
}

func TestDestroyDeletesTheStackAndTheKey(t *testing.T) {
	cli := &fakeCLI{errors: map[string]error{
		"ssm delete-parameter": errors.New("An error occurred (ParameterNotFound)")}}
	lines := []string{}
	err := account(t, cli).Destroy(context.Background(), func(line string) { lines = append(lines, line) })
	if err != nil {
		t.Fatalf("a key that is already gone must not fail the removal: %v", err)
	}
	for _, key := range []string{"cloudformation delete-stack", "cloudformation wait", "ssm delete-parameter"} {
		if !cli.called(key) {
			t.Errorf("%s was not called", key)
		}
	}
	if len(lines) == 0 {
		t.Fatal("no progress was reported")
	}
}

func TestDestroyStopsWhenTheStackIsNotDeleted(t *testing.T) {
	cli := &fakeCLI{errors: map[string]error{"cloudformation wait": errors.New("DELETE_FAILED")}}
	err := account(t, cli).Destroy(context.Background(), func(string) {})
	if err == nil || cli.called("ssm delete-parameter") {
		t.Fatalf("error = %v; the key was deleted although the stack is still there", err)
	}
}

func TestHomeOfAStackThatDoesNotExistIsEmpty(t *testing.T) {
	cli := &fakeCLI{errors: map[string]error{
		"cloudformation describe-stacks": errors.New("Stack with id x does not exist")}}
	home, err := account(t, cli).Home(context.Background())
	if err != nil || home != (Home{}) {
		t.Fatalf("home = %+v, err = %v", home, err)
	}
}

func TestHomeReadsTheClusterAndTheService(t *testing.T) {
	cli := &fakeCLI{answers: map[string]string{"cloudformation describe-stacks": `{"Stacks":[{"Outputs":[
		{"OutputKey":"Cluster","OutputValue":"clanker-prod-1-control"},
		{"OutputKey":"Service","OutputValue":"control-plane"},
		{"OutputKey":"RoleArn","OutputValue":"arn:aws:iam::000000000000:role/x"}]}]}`}}
	home, err := account(t, cli).Home(context.Background())
	if err != nil || home.Cluster != "clanker-prod-1-control" || home.Service != "control-plane" {
		t.Fatalf("home = %+v, err = %v", home, err)
	}
}

const taskArn = "arn:aws:ecs:us-east-1:000000000000:task/clanker-prod-1-control/0123456789abcdef"

func describeTasks(status, agent, runtime string) string {
	return `{"tasks":[{"taskArn":"` + taskArn + `","lastStatus":"` + status + `","containers":[
		{"runtimeId":"` + runtime + `","managedAgents":[{"name":"ExecuteCommandAgent","lastStatus":"` + agent + `"}]}]}]}`
}

func TestRunningTaskGivesTheTargetOfTheTunnel(t *testing.T) {
	cli := &fakeCLI{answers: map[string]string{
		"ecs list-tasks":     `{"taskArns":["` + taskArn + `"]}`,
		"ecs describe-tasks": describeTasks("RUNNING", "RUNNING", "0123456789abcdef-0265927825"),
	}}
	home := Home{Cluster: "clanker-prod-1-control", Service: "control-plane"}
	task, err := account(t, cli).RunningTask(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	want := "ecs:clanker-prod-1-control_0123456789abcdef_0123456789abcdef-0265927825"
	if task.Target() != want {
		t.Fatalf("target = %q, want %q", task.Target(), want)
	}
}

func TestRunningTaskWaitsForTheExecAgent(t *testing.T) {
	home := Home{Cluster: "clanker-prod-1-control", Service: "control-plane"}
	cases := map[string]map[string]string{
		"no task":            {"ecs list-tasks": `{"taskArns":[]}`},
		"task still starts":  {"ecs list-tasks": `{"taskArns":["` + taskArn + `"]}`, "ecs describe-tasks": describeTasks("PENDING", "PENDING", "")},
		"agent not ready":    {"ecs list-tasks": `{"taskArns":["` + taskArn + `"]}`, "ecs describe-tasks": describeTasks("RUNNING", "PENDING", "abc-1")},
		"runtime id missing": {"ecs list-tasks": `{"taskArns":["` + taskArn + `"]}`, "ecs describe-tasks": describeTasks("RUNNING", "RUNNING", "")},
	}
	for name, answers := range cases {
		_, err := account(t, &fakeCLI{answers: answers}).RunningTask(context.Background(), home)
		if !errors.Is(err, ErrNoTask) {
			t.Errorf("%s: error = %v, want ErrNoTask", name, err)
		}
	}
}

func TestReleaseBinaryReadsTheChecksum(t *testing.T) {
	sum := strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.2.3/checksums.txt" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("c", 64) + "  clanker-darwin-arm64\n" + sum + "  clanker-linux-arm64\n"))
	}))
	defer server.Close()
	binary, err := ReleaseBinary(context.Background(), server.Client(), server.URL, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if binary.SHA256 != sum || binary.URL != server.URL+"/v1.2.3/clanker-linux-arm64" {
		t.Fatalf("binary = %+v", binary)
	}
	if _, err := ReleaseBinary(context.Background(), server.Client(), server.URL, "v0.0.0"); err == nil {
		t.Fatal("no error for a release that does not exist")
	}
	if _, err := ReleaseBinary(context.Background(), server.Client(), server.URL, "dev"); err == nil ||
		!strings.Contains(err.Error(), "development build") {
		t.Fatalf("error for a development build = %v", err)
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

func TestCostEstimateFollowsTheSize(t *testing.T) {
	total := func(lines []CostLine) float64 {
		sum := 0.0
		for _, line := range lines {
			sum += line.Monthly
		}
		return sum
	}
	small, medium := total(costEstimate(SizeOf("small"))), total(costEstimate(SizeOf("medium")))
	if small < 15 || small > 22 {
		t.Fatalf("small estimate = %.2f USD, want about 18", small)
	}
	if medium <= small {
		t.Fatalf("medium (%.2f) must cost more than small (%.2f)", medium, small)
	}
	if SizeOf("enormous") != Sizes[0] {
		t.Fatal("a size outside the list was accepted")
	}
}
