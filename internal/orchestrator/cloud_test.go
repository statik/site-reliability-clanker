package orchestrator

import (
	"context"
	"io/fs"
	"maps"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
	"github.com/statik/site-reliability-clanker/internal/policy"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// sessionIssuer is a target that gives an AWS session for each grant. It records the
// grants, and the time that each one had left when the broker exchanged it.
type sessionIssuer struct {
	mu        sync.Mutex
	grants    []broker.Grant
	remaining []time.Duration
}

func (s *sessionIssuer) Issue(_ context.Context, g broker.Grant, remaining time.Duration) (
	broker.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants = append(s.grants, g)
	s.remaining = append(s.remaining, remaining)
	return broker.Credential{AWS: &jobspec.AWSSession{AccessKeyID: "ASIAEXAMPLE" + g.Layer,
		SecretAccessKey: "example-secret", SessionToken: "example-token",
		Expires: fixedClock{}.Now().Add(15 * time.Minute)}, Mechanism: "example STS session"}, nil
}

func (s *sessionIssuer) Withdraw(context.Context, broker.Grant) error { return nil }

func (s *sessionIssuer) issued() []broker.Grant {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]broker.Grant{}, s.grants...)
}

func (s *sessionIssuer) remainders() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration{}, s.remaining...)
}

var (
	exampleBackend = map[string]string{"bucket": "clanker-example-state-111122223333",
		"encrypt": "true", "region": "eu-west-1", "use_lockfile": "true"}
	sandbox = EnvConfig{Name: "aws-sandbox", Cloud: "aws", Region: "eu-west-1",
		Backend: exampleBackend}
	localEnv = EnvConfig{Name: "local", Cloud: "local"}
)

// sharedVariables are the variables that the control plane gives every job.
var sharedVariables = map[string]string{"TF_VAR_admin_token": "example-admin-token"}

// cloudOrchestrator has one environment, a forge with the runbooks of this repository, a
// broker with the issuer, and runners that answer each plan job with a plan that changes
// nothing. The layers in applied have outputs. issuer can be nil: then the broker has no
// issuer.
func cloudOrchestrator(t *testing.T, env EnvConfig, issuer broker.Issuer,
	applied ...string) (*Orchestrator, *fakeRunners) {
	t.Helper()
	db := storetest.Open(t)
	clock := fixedClock{}
	f := forge.New(db)
	if err := f.Seed(RunbooksRepo, packtest.Runbooks(t), "Seed runbooks"); err != nil {
		t.Fatal(err)
	}
	code := fstest.MapFS{"README.md": {Data: []byte("Platform\n")}}
	if err := f.Seed(PlatformRepo, map[string]fs.FS{".": code}, "Seed platform"); err != nil {
		t.Fatal(err)
	}
	key, err := broker.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := broker.New(db, clock, key, issuer,
		func() (policy.Approvals, error) { return ApprovalSettings(f) })
	if err != nil {
		t.Fatal(err)
	}
	for _, layer := range applied {
		if err := store.SetLayerOutputs(db, env.Name, layer,
			map[string]any{"from": layer}, "then"); err != nil {
			t.Fatal(err)
		}
	}
	// The orchestrator gets its own maps, so that a test sees a change that a job makes.
	env.Backend = maps.Clone(env.Backend)
	plan := []byte("a plan that changes nothing")
	runners := &fakeRunners{result: jobspec.Result{Plan: plan, PlanSHA256: jobspec.SHA256(plan),
		PlanJSON: []byte(`{"resource_changes": []}`)}}
	return &Orchestrator{DB: db, Audit: audit.New(db, clock), Forge: f, Broker: b,
		Clock: clock, Runners: runners, App: packtest.Example(t),
		Envs:         map[string]EnvConfig{env.Name: env},
		JobVariables: maps.Clone(sharedVariables)}, runners
}

// planNew creates a work item for the layers and plans it.
func planNew(t *testing.T, o *Orchestrator, env string, layers []string) (WorkItem, error) {
	t.Helper()
	wi, err := o.Create(NewWorkItem{Title: "Deploy " + env, Kind: KindBlankSlate, Env: env,
		Layers: layers, Trigger: "test", Actor: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return o.PlanWorkItem(context.Background(), wi.ID)
}

func TestTheFirstPlanOnACloudIsLayer10WithAPlannerSessionTheBackendAndTheInputs(t *testing.T) {
	issuer := &sessionIssuer{}
	o, runners := cloudOrchestrator(t, sandbox, issuer, "00-bootstrap")
	wi, err := planNew(t, o, sandbox.Name, sandbox.BlankSlateLayers())
	if err != nil {
		t.Fatal(err)
	}
	if want := Layers[1:]; !reflect.DeepEqual(wi.Layers, want) {
		t.Errorf("layers = %v, want %v", wi.Layers, want)
	}
	if len(runners.jobs) != 1 || runners.jobs[0].Layer != "10-network" {
		t.Fatalf("jobs = %+v; want one plan of 10-network, the others wait for it", runners.jobs)
	}
	spec := runners.jobs[0]
	if spec.AWS == nil || spec.AWS.AccessKeyID != "ASIAEXAMPLE10-network" {
		t.Errorf("AWS session = %v, want the session of the planner grant", spec.AWS)
	}
	wantBackend := maps.Clone(exampleBackend)
	wantBackend["key"] = "aws-sandbox/10-network.tfstate"
	if !reflect.DeepEqual(spec.Backend, wantBackend) {
		t.Errorf("backend = %v, want %v", spec.Backend, wantBackend)
	}
	wantVariables := map[string]string{"TF_VAR_admin_token": "example-admin-token",
		"TF_VAR_env_name": "aws-sandbox", "TF_VAR_region": "eu-west-1",
		"TF_VAR_state_location": "clanker-example-state-111122223333"}
	if !reflect.DeepEqual(spec.Variables, wantVariables) {
		t.Errorf("variables = %v, want %v", spec.Variables, wantVariables)
	}
	grants := issuer.issued()
	if len(grants) != 1 || grants[0].Identity != "planner" || grants[0].Kind != broker.KindPlan ||
		grants[0].Layer != "10-network" {
		t.Errorf("grants = %+v, want one planner grant for 10-network", grants)
	}
	// The job got its own copies: the settings of the environment stay the same.
	if !reflect.DeepEqual(o.Envs[sandbox.Name].Backend, exampleBackend) ||
		!reflect.DeepEqual(o.JobVariables, sharedVariables) {
		t.Errorf("a job changed the shared settings: backend %v, variables %v",
			o.Envs[sandbox.Name].Backend, o.JobVariables)
	}
}

func TestEveryPlanOnACloudGetsAPlannerSessionAndTheKeyOfItsLayer(t *testing.T) {
	issuer := &sessionIssuer{}
	o, runners := cloudOrchestrator(t, sandbox, issuer, Layers[:5]...)
	if _, err := planNew(t, o, sandbox.Name, sandbox.BlankSlateLayers()); err != nil {
		t.Fatal(err)
	}
	if len(runners.jobs) != 5 || len(issuer.issued()) != 5 {
		t.Fatalf("%d jobs and %d grants, want 5 of each", len(runners.jobs), len(issuer.issued()))
	}
	for i, spec := range runners.jobs {
		layer := Layers[i+1]
		if spec.Layer != layer || spec.AWS == nil || spec.AWS.AccessKeyID != "ASIAEXAMPLE"+layer ||
			spec.Backend["key"] != "aws-sandbox/"+layer+".tfstate" {
			t.Errorf("job %d: layer %s, session %v, key %q", i, spec.Layer, spec.AWS,
				spec.Backend["key"])
		}
	}
}

func TestAPlanOnTheLocalTargetHasNoBackendNoInputsAndNoGrantBeforeTheCluster(t *testing.T) {
	issuer := &sessionIssuer{}
	o, runners := cloudOrchestrator(t, localEnv, issuer, "00-bootstrap")
	if _, err := planNew(t, o, localEnv.Name, []string{"10-network"}); err != nil {
		t.Fatal(err)
	}
	if len(runners.jobs) != 1 {
		t.Fatalf("jobs = %+v, want one", runners.jobs)
	}
	spec := runners.jobs[0]
	if spec.Backend != nil || spec.AWS != nil || spec.KubeConfig != "" ||
		!reflect.DeepEqual(spec.Variables, sharedVariables) {
		t.Errorf("backend %v, session %v, variables %v; want none, none and the shared ones",
			spec.Backend, spec.AWS, spec.Variables)
	}
	if grants := issuer.issued(); len(grants) != 0 || count(t, o, "grants") != 0 {
		t.Errorf("grants = %+v, want none before the cluster exists", grants)
	}
}

func TestAPlanOnACloudWithNoIssuerGetsNoGrant(t *testing.T) {
	o, runners := cloudOrchestrator(t, sandbox, nil, "00-bootstrap")
	if _, err := planNew(t, o, sandbox.Name, []string{"10-network"}); err != nil {
		t.Fatal(err)
	}
	if len(runners.jobs) != 1 || runners.jobs[0].AWS != nil || count(t, o, "grants") != 0 {
		t.Errorf("jobs = %+v, %d grants; want one job and no grant", runners.jobs,
			count(t, o, "grants"))
	}
}

func TestACloudEnvironmentWithNoBackendPlansNothing(t *testing.T) {
	issuer := &sessionIssuer{}
	noBackend := EnvConfig{Name: "aws-sandbox", Cloud: "aws"}
	o, runners := cloudOrchestrator(t, noBackend, issuer, "00-bootstrap")
	wi, err := planNew(t, o, noBackend.Name, []string{"10-network"})
	want := "10-network of environment aws-sandbox on aws: the control plane has no state " +
		"backend for the environment"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want %q", err, want)
	}
	if wi.State != StateFailed || len(runners.jobs) != 0 || count(t, o, "grants") != 0 {
		t.Errorf("state %s, %d jobs, %d grants; want Failed and nothing started", wi.State,
			len(runners.jobs), count(t, o, "grants"))
	}
}

func TestTheFirstDeploymentOnACloudStartsAboveTheBootstrapLayer(t *testing.T) {
	if got := localEnv.BlankSlateLayers(); !reflect.DeepEqual(got, Layers) {
		t.Errorf("local: %v, want %v", got, Layers)
	}
	got := sandbox.BlankSlateLayers()
	if !reflect.DeepEqual(got, Layers[1:]) {
		t.Errorf("aws: %v, want %v", got, Layers[1:])
	}
	got[0] = "changed"
	if Layers[1] != "10-network" {
		t.Fatal("the layers of a work item share their array with Layers")
	}
}

func TestTheApplyWindowIsOneHourOnAWSAndTheReserveElsewhere(t *testing.T) {
	cases := []struct {
		cloud string
		want  time.Duration
	}{{"aws", time.Hour}, {"local", policy.WindowReserve}, {"gcp", policy.WindowReserve},
		{"", policy.WindowReserve}}
	for _, c := range cases {
		if got := (EnvConfig{Cloud: c.cloud}).ApplyWindow(); got != c.want {
			t.Errorf("cloud %q: apply window %s, want %s", c.cloud, got, c.want)
		}
	}
	if policy.WindowReserve != 30*time.Minute {
		t.Errorf("the apply window of the local target changed to %s", policy.WindowReserve)
	}
}

func TestAnApplyOnAWSCanUseTheWindowOfEachLayerAndLocally45Minutes(t *testing.T) {
	o := &Orchestrator{Envs: map[string]EnvConfig{sandbox.Name: sandbox,
		localEnv.Name: localEnv}}
	cases := []struct {
		wi   WorkItem
		want time.Duration
	}{
		{WorkItem{Env: localEnv.Name, Layers: []string{"50-app"}}, 45 * time.Minute},
		{WorkItem{Env: localEnv.Name, Layers: Layers}, 45 * time.Minute},
		{WorkItem{Env: localEnv.Name}, 45 * time.Minute},
		{WorkItem{Env: sandbox.Name, Layers: sandbox.BlankSlateLayers()},
			5*time.Hour + 15*time.Minute},
		{WorkItem{Env: sandbox.Name, Layers: []string{"20-cluster"}},
			time.Hour + 15*time.Minute},
	}
	for _, c := range cases {
		if got := o.applyTimeout(c.wi); got != c.want {
			t.Errorf("%s %v: timeout %s, want %s", c.wi.Env, c.wi.Layers, got, c.want)
		}
	}
}

// approvedApply approves a work item of tier T2 with one saved plan for the layer that
// adds a resource, and waits for its apply.
func approvedApply(t *testing.T, o *Orchestrator, env, layer string) WorkItem {
	t.Helper()
	commit, err := o.Forge.Head(PlatformRepo, "main")
	if err != nil {
		t.Fatal(err)
	}
	plan := []byte("the approved plan of " + layer)
	if _, err := o.DB.Exec(`INSERT INTO users (name, display, role, email) VALUES
		('alice', 'Alice Ng', 'approver', 'alice@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.DB.Exec(`INSERT INTO work_items (id, title, kind, env, layers, state, tier,
		commit_sha, created_at, updated_at) VALUES ('WI-0001', 'Change a layer', 'manual', $1,
		$2, $3, 'T2', $4, 'then', 'then')`, env, `["`+layer+`"]`, StateAwaitingApproval,
		commit); err != nil {
		t.Fatal(err)
	}
	if _, err := o.DB.Exec(`INSERT INTO plans (work_item, layer, commit_sha, plan_sha256, plan,
		plan_json, facts, created_at) VALUES ('WI-0001', $1, $2, $3, $4, '{}', '{"add": 1}',
		'then')`, layer, commit, jobspec.SHA256(plan), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := approve(t, o, "WI-0001", "alice"); err != nil {
		t.Fatal(err)
	}
	o.WaitForApplies()
	wi, err := o.Get("WI-0001")
	if err != nil {
		t.Fatal(err)
	}
	return wi
}

func TestAnApplyGrantAndItsSessionLastTheApplyWindowOfTheTarget(t *testing.T) {
	cases := []struct {
		env   EnvConfig
		layer string
		want  time.Duration
	}{{sandbox, "10-network", time.Hour}, {localEnv, "50-app", 30 * time.Minute}}
	for _, c := range cases {
		issuer := &sessionIssuer{}
		o, _ := cloudOrchestrator(t, c.env, issuer, "00-bootstrap")
		if wi := approvedApply(t, o, c.env.Name, c.layer); wi.State != StateDone {
			t.Fatalf("%s: state %s, want %s", c.env.Cloud, wi.State, StateDone)
		}
		grants, remaining := issuer.issued(), issuer.remainders()
		if len(grants) != 1 || grants[0].Kind != broker.KindApply {
			t.Fatalf("%s: want one apply grant, got %+v", c.env.Cloud, grants)
		}
		lifetime := store.ParseTime(grants[0].Expires).Sub(store.ParseTime(grants[0].NotBefore))
		if lifetime != c.want || remaining[0] != c.want {
			t.Errorf("%s: grant of %s with %s left at the exchange, want %s", c.env.Cloud,
				lifetime, remaining[0], c.want)
		}
	}
}
