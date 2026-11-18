package dispatch_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/dispatch"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
	"github.com/statik/site-reliability-clanker/internal/runner"
)

// launcher starts a real runner client in this process, as the local launcher starts one in
// another process. It keeps the tokens that it saw.
type launcher struct {
	url    string
	dir    string
	fail   error
	silent bool

	mu     sync.Mutex
	tokens []string
}

func (l *launcher) Launch(ctx context.Context, token string) error {
	l.mu.Lock()
	l.tokens = append(l.tokens, token)
	l.mu.Unlock()
	if l.fail != nil {
		return l.fail
	}
	if l.silent {
		return nil
	}
	r, err := runner.New(l.dir)
	if err != nil {
		return err
	}
	return (&runner.Client{CoreURL: l.url, Token: token, Runner: r}).Serve(ctx)
}

func (l *launcher) lastToken() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.tokens[len(l.tokens)-1]
}

func scripts(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipped := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipped)
	for name, body := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)),
			Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func setup(t *testing.T) (*dispatch.Dispatcher, *launcher, *httptest.Server) {
	t.Helper()
	l := &launcher{dir: t.TempDir()}
	d := dispatch.New(l)
	server := httptest.NewServer(d.Handler())
	t.Cleanup(server.Close)
	l.url = server.URL
	return d, l, server
}

func runbook(t *testing.T, name string, log *bytes.Buffer) jobspec.Job {
	t.Helper()
	return jobspec.Job{Spec: jobspec.Spec{Kind: jobspec.KindRunbook, Env: "local",
		CheckoutID: "runbooks/abc", Runbook: name, Params: map[string]string{"who": "carol"}},
		Checkout: scripts(t, map[string]string{
			"jobs/hello.sh": "#!/bin/bash\necho \"hello $PARAM_WHO\"\n",
			"jobs/fail.sh":  "#!/bin/bash\necho before the failure\nexit 3\n",
			"jobs/report.sh": "#!/bin/bash\necho \"file=/backups/$PARAM_WHO.dump\" >>\"$OUTPUT_FILE\"\n" +
				"echo \"objects=31\" >>\"$OUTPUT_FILE\"\n",
		}), Log: log}
}

func TestTheOutputsOfARunbookComeBackFromTheRunner(t *testing.T) {
	d, _, _ := setup(t)
	result, err := d.Run(context.Background(), runbook(t, "report", &bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	// The runner sends text. The core gives each output its declared type.
	want := map[string]any{"file": "/backups/carol.dump", "objects": "31"}
	if !reflect.DeepEqual(result.Outputs, want) {
		t.Errorf("outputs = %#v, want %#v", result.Outputs, want)
	}
}

func TestARunnerRunsTheJobAndTheLogComesBack(t *testing.T) {
	d, l, server := setup(t)
	var log bytes.Buffer
	result, err := d.Run(context.Background(), runbook(t, "hello", &log))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != jobspec.StatusOK || !strings.HasPrefix(result.ToolDigest, "tools-sha256:") {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(log.String(), "hello carol") {
		t.Fatalf("log = %q", log.String())
	}
	// When the job has its result, its token opens nothing.
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/runner/v1/job", nil)
	request.Header.Set("Authorization", "Bearer "+l.lastToken())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the token of a finished job got status %d, want 401", response.StatusCode)
	}
}

func TestAFailedJobIsAnErrorWithItsLog(t *testing.T) {
	d, _, _ := setup(t)
	var log bytes.Buffer
	result, err := d.Run(context.Background(), runbook(t, "fail", &log))
	if err == nil || !strings.Contains(err.Error(), "runbook fail") {
		t.Fatalf("error = %v, want the failure of the runbook", err)
	}
	if result.Status != jobspec.StatusFailed || !strings.Contains(log.String(), "before the failure") {
		t.Fatalf("status %q, log %q", result.Status, log.String())
	}
	if errors.Is(err, jobspec.ErrStalePlan) {
		t.Fatal("a failed runbook counts as a stale plan")
	}
}

func TestAnApplyWithAnotherHashIsRefusedByTheRunner(t *testing.T) {
	d, _, _ := setup(t)
	var log bytes.Buffer
	job := jobspec.Job{Spec: jobspec.Spec{Kind: jobspec.KindApply, Env: "local", Cloud: "local",
		Layer: "50-app", CheckoutID: "WI-0001/abc", PlanSHA256: jobspec.SHA256([]byte("approved"))},
		Checkout: scripts(t, map[string]string{"platform/stacks/local/50-app/main.tf": ""}),
		Plan:     []byte("another plan"), Log: &log}
	_, err := d.Run(context.Background(), job)
	if err == nil || !strings.Contains(err.Error(), "not the approved hash") {
		t.Fatalf("error = %v, want a refusal", err)
	}
}

func TestRequestsWithoutTheTokenOfARunningJobAreRefused(t *testing.T) {
	_, _, server := setup(t)
	for _, header := range []string{"", "Bearer ", "Bearer wrong", "Basic abc"} {
		for _, path := range []string{"job", "job/checkout", "job/plan"} {
			request, _ := http.NewRequest(http.MethodGet, server.URL+"/runner/v1/"+path, nil)
			if header != "" {
				request.Header.Set("Authorization", header)
			}
			// The user header of the dashboard means nothing here.
			request.Header.Set("X-Clanker-User", "alice")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized {
				t.Errorf("%q on %s: status %d, want 401", header, path, response.StatusCode)
			}
		}
	}
}

func TestALauncherThatFailsFailsTheJob(t *testing.T) {
	d, l, _ := setup(t)
	l.fail = errors.New("no machine")
	_, err := d.Run(context.Background(), runbook(t, "hello", &bytes.Buffer{}))
	if err == nil || !strings.Contains(err.Error(), "gave no result") || !strings.Contains(err.Error(), "no machine") {
		t.Fatalf("error = %v", err)
	}
}

func TestARunnerThatSaysNothingFailsTheJob(t *testing.T) {
	d, l, _ := setup(t)
	l.silent = true
	d.Silence = 1500 * time.Millisecond
	started := time.Now()
	_, err := d.Run(context.Background(), runbook(t, "hello", &bytes.Buffer{}))
	if err == nil || !strings.Contains(err.Error(), "said nothing") {
		t.Fatalf("error = %v", err)
	}
	if waited := time.Since(started); waited > 6*time.Second {
		t.Fatalf("the job waited %s for a silent runner", waited)
	}
}

func TestAStoppedContextStopsTheWait(t *testing.T) {
	d, l, _ := setup(t)
	l.silent = true
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := d.Run(ctx, runbook(t, "hello", &bytes.Buffer{})); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the deadline", err)
	}
}

func TestASecondResultIsRefused(t *testing.T) {
	d, l, server := setup(t)
	l.silent = true
	done := make(chan error, 1)
	go func() {
		_, err := d.Run(context.Background(), runbook(t, "hello", &bytes.Buffer{}))
		done <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		l.mu.Lock()
		launched := len(l.tokens) > 0
		l.mu.Unlock()
		if launched || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	post := func() int {
		request, _ := http.NewRequest(http.MethodPost, server.URL+"/runner/v1/job/result",
			strings.NewReader(`{"status":"stale","error":"the state moved"}`))
		request.Header.Set("Authorization", "Bearer "+l.lastToken())
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if status := post(); status != http.StatusNoContent {
		t.Fatalf("first result: status %d", status)
	}
	if err := <-done; !errors.Is(err, jobspec.ErrStalePlan) {
		t.Fatalf("error = %v, want a stale plan", err)
	}
	if status := post(); status != http.StatusUnauthorized && status != http.StatusConflict {
		t.Fatalf("second result: status %d, want a refusal", status)
	}
}

// envTofu stands in for tofu. For `init` and `plan` it appends the AWS variables of its
// environment to $FAKE_TOFU_ENV. It prints nothing. The other variables stay out of the
// file, so that a failed test does not print the environment of the machine. Only `init`
// works, so a plan job stops after these two steps. The runner also asks tofu for its
// version, which is not a step of the job.
const envTofu = `#!/bin/sh
case "$1" in
init | plan) env | grep '^AWS_' >>"$FAKE_TOFU_ENV" ;;
esac
[ "$1" = init ]
`

// planWithTofuEnv runs a plan job with envTofu and returns the environment that the tofu
// processes saw, the job log with the error of the job, and the directory of the runner.
func planWithTofuEnv(t *testing.T, session *jobspec.AWSSession) (string, string, string) {
	t.Helper()
	bin, seen := t.TempDir(), filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(filepath.Join(bin, "tofu"), []byte(envTofu), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_TOFU_ENV", seen)
	// The runner has keys of its own, as a runner on an instance with a profile has.
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIARUNNEROWN")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	d, l, _ := setup(t)
	var log bytes.Buffer
	_, err := d.Run(context.Background(), jobspec.Job{Spec: jobspec.Spec{Kind: jobspec.KindPlan,
		Env: "example", Cloud: "aws", Layer: "10-network", CheckoutID: "WI-0142/abc",
		AWS: session}, Log: &log, Checkout: scripts(t, map[string]string{
		"platform/stacks/aws/10-network/main.tf": "# empty\n"})})
	if err == nil || !strings.Contains(err.Error(), "tofu plan") {
		t.Fatalf("error = %v, want the failure of the plan step of the stand-in", err)
	}
	environment, readErr := os.ReadFile(seen)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(environment), log.String() + err.Error(), l.dir
}

func TestTheAWSKeysOfAJobAreOnlyInTheEnvironmentOfItsTofuProcesses(t *testing.T) {
	session := &jobspec.AWSSession{AccessKeyID: "ASIAEXAMPLE",
		SecretAccessKey: "example-secret-access-key", SessionToken: "example-session-token",
		Expires: time.Date(2026, 11, 21, 2, 30, 0, 0, time.UTC)}
	environment, log, dir := planWithTofuEnv(t, session)
	for _, line := range []string{"AWS_ACCESS_KEY_ID=ASIAEXAMPLE",
		"AWS_SECRET_ACCESS_KEY=example-secret-access-key",
		"AWS_SESSION_TOKEN=example-session-token"} {
		if strings.Count(environment, line+"\n") != 2 {
			t.Errorf("init and plan must each see %s", line)
		}
	}
	if strings.Contains(environment, "AKIARUNNEROWN") {
		t.Error("tofu saw the key of the runner, not only the key of the job")
	}
	secrets := []string{session.SecretAccessKey, session.SessionToken}
	for _, secret := range secrets {
		if strings.Contains(log, secret) {
			t.Errorf("the job log or the error holds %q: %s", secret, log)
		}
	}
	if os.Getenv("AWS_SESSION_TOKEN") != "" || os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		t.Error("the keys are in the environment of the runner itself")
	}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		text, err := os.ReadFile(path)
		for _, secret := range secrets {
			if bytes.Contains(text, []byte(secret)) {
				t.Errorf("the file %s of the runner holds %q", path, secret)
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAJobWithoutAWSKeysLeavesTheEnvironmentOfTofuAsItIs(t *testing.T) {
	environment, _, _ := planWithTofuEnv(t, nil)
	if strings.Count(environment, "AWS_ACCESS_KEY_ID=AKIARUNNEROWN\n") != 2 {
		t.Error("tofu did not see the key of the runner in both steps")
	}
	if strings.Count(environment, "AWS_SESSION_TOKEN=\n") != 2 {
		t.Error("tofu got a session token that no job has")
	}
}
