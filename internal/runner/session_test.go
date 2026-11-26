package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/jobspec"
)

var exampleSession = &jobspec.AWSSession{AccessKeyID: "ASIAEXAMPLEJOB",
	SecretAccessKey: "example-job-secret", SessionToken: "example-job-token",
	Expires: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)}

// showSession prints the keys and the kubeconfig that a program of a job sees.
const showSession = `echo "key=[${AWS_ACCESS_KEY_ID-unset}]` +
	` secret=[${AWS_SECRET_ACCESS_KEY-unset}] token=[${AWS_SESSION_TOKEN-unset}]"
echo "kubeconfig=[$(cat "${KUBECONFIG}")]"
`

// The keys of the runner itself must not reach a job that has a session of its own.
func setRunnerKeys(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "from-the-runner")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "from-the-runner")
	t.Setenv("AWS_SESSION_TOKEN", "from-the-runner")
}

func runbookWithSession(t *testing.T, session *jobspec.AWSSession) string {
	t.Helper()
	checkout := Checkout{ID: "runbooks/def", Archive: archive(t,
		entry{"jobs/job.sh", "#!/bin/bash\nset -euo pipefail\n" + showSession, 0o755})}
	var log bytes.Buffer
	_, err := newRunner(t).Runbook(context.Background(), RunbookJob{Env: "aws-sandbox",
		Checkout: checkout, Name: "job", KubeConfig: `{"kind": "Config"}`, AWS: session,
		Namespace: "harbor", Log: &log})
	if err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	return log.String()
}

func TestARunbookGetsTheKeysOfItsSessionBesideItsKubeconfig(t *testing.T) {
	setRunnerKeys(t)
	log := runbookWithSession(t, exampleSession)
	want := `key=[ASIAEXAMPLEJOB] secret=[example-job-secret] token=[example-job-token]` +
		"\nkubeconfig=[{\"kind\": \"Config\"}]"
	if !strings.Contains(log, want) {
		t.Errorf("log = %q, want %q", log, want)
	}
}

func TestARunbookWithoutASessionGetsNoOtherKeys(t *testing.T) {
	setRunnerKeys(t)
	log := runbookWithSession(t, nil)
	if !strings.Contains(log, "key=[from-the-runner]") {
		t.Errorf("log = %q: a run without a session must keep the environment of the runner",
			log)
	}
}

func TestKubectlGetsTheKeysOfTheSession(t *testing.T) {
	setRunnerKeys(t)
	bin := t.TempDir()
	fake := "#!/bin/bash\nset -euo pipefail\nKUBECONFIG=\"$2\"\nshift 2\necho \"args=[$*]\"\n" +
		showSession
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := newRunner(t)
	out, err := r.Kubectl(context.Background(), `{"kind": "Config"}`, exampleSession, "get",
		"pods")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"args=[get pods]",
		"key=[ASIAEXAMPLEJOB] secret=[example-job-secret] token=[example-job-token]",
		`kubeconfig=[{"kind": "Config"}]`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output = %q, want %q", out, want)
		}
	}
	out, err = r.Kubectl(context.Background(), `{"kind": "Config"}`, nil, "get", "pods")
	if err != nil || !strings.Contains(string(out), "key=[from-the-runner]") {
		t.Errorf("without a session: %q, %v", out, err)
	}
	left, err := os.ReadDir(filepath.Join(r.WorkRoot, "credentials"))
	if err != nil || len(left) != 0 {
		t.Errorf("credential files left: %v, %v", left, err)
	}
}
