package setup

import (
	"context"
	"errors"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The private values of the stand-in account. No output of the installer may show them.
const (
	fixtureTagValue = "owner@example.com"
	fixtureModelKey = "sk-or-v1-fixture-model-key-0000000000"
)

// fakeLayers stands in for bootstrap and destroy of the layers. It records what the
// installer asked, and which markers of the stand-ins existed at that time.
type fakeLayers struct {
	state     string
	boots     []LayerRequest
	destroys  []LayerRequest
	failBoot  bool
	atDestroy []string
}

func (f *fakeLayers) Bootstrap(_ context.Context, request LayerRequest) error {
	f.boots = append(f.boots, request)
	if f.failBoot {
		f.failBoot = false
		return errors.New("the fake bootstrap fails once")
	}
	return nil
}

func (f *fakeLayers) Destroy(_ context.Context, request LayerRequest) error {
	f.destroys = append(f.destroys, request)
	for _, marker := range []string{"core-stopped", "tasks-stopped", "home-applied"} {
		if _, err := os.Stat(filepath.Join(f.state, marker)); err == nil {
			f.atDestroy = append(f.atDestroy, marker)
		}
	}
	return nil
}

// installStage is a machine with the stand-ins of web/scripts/fixtures/aws-account first in
// PATH, no AWS credential, an empty home and a dashboard that answers.
type installStage struct {
	t       *testing.T
	root    string
	state   string
	layers  *fakeLayers
	lines   []string
	steps   []string
	repo    string
	awsLog  string
	healthz *httptest.Server
}

func newInstallStage(t *testing.T) *installStage {
	t.Helper()
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	s := &installStage{t: t, root: t.TempDir(), state: t.TempDir(), repo: repo}
	s.awsLog = filepath.Join(s.state, "aws.log")
	s.layers = &fakeLayers{state: s.state}
	fixtures := filepath.Join(repo, "web", "scripts", "fixtures", "aws-account")
	t.Setenv("PATH", fixtures+string(os.PathListSeparator)+"/usr/bin"+
		string(os.PathListSeparator)+"/bin")
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"AWS_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN", "AWS_REGION", "AWS_DEFAULT_REGION", "TF_DATA_DIR",
		"SRC_FIXTURE_FAIL"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	t.Setenv("SRC_FIXTURE_STATE", s.state)
	t.Setenv("SRC_FIXTURE_LOG", s.awsLog)
	s.healthz = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.healthz.Close)
	t.Setenv("SRC_FIXTURE_CORE_URL", s.healthz.URL)
	t.Setenv("SRC_FIXTURE_NAME_SERVERS", "ns-1.fixture.example.net")
	lookup := LookupNS
	LookupNS = func(_ context.Context, name string) ([]*net.NS, error) {
		if name != "example.com" {
			return nil, errors.New("no such host")
		}
		return []*net.NS{{Host: "ns-1.fixture.example.net."}}, nil
	}
	t.Cleanup(func() { LookupNS = lookup })
	return s
}

func (s *installStage) installer() *Installer {
	return &Installer{Layers: s.layers, Root: s.root, Version: "dev", Wait: time.Millisecond}
}

func (s *installStage) input() InstallInput {
	return InstallInput{Env: "fixture", Profile: "fixture-operator", Region: "us-west-2",
		Network: "vpc-0f1c7e01", Subnets: []string{"subnet-0a", "subnet-0b"},
		RouterGroup: "sg-0f1c7e02", ZoneID: "Z0F1C7E1",
		Tags:     map[string]string{"example:owner": fixtureTagValue},
		ModelKey: fixtureModelKey, Source: s.repo}
}

func (s *installStage) report() Report {
	return Report{Step: func(_ int, title string) { s.steps = append(s.steps, title) },
		Line: func(text string) { s.lines = append(s.lines, text) }}
}

func (s *installStage) install(in InstallInput) (InstallResult, error) {
	s.steps = nil
	return s.installer().Install(context.Background(), in, s.report())
}

func (s *installStage) uninstall() error {
	s.steps = nil
	in := InstallInput{Env: "fixture", Profile: "fixture-operator"}
	return s.installer().Uninstall(context.Background(), in, s.report())
}

// calls returns the calls of the stand-ins tofu, docker, ops and make that start with text.
func (s *installStage) calls(text string) []string {
	s.t.Helper()
	data, err := os.ReadFile(filepath.Join(s.state, "calls"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		s.t.Fatal(err)
	}
	found := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, text) {
			found = append(found, line)
		}
	}
	return found
}

func (s *installStage) file(path string) string {
	s.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		s.t.Fatal(err)
	}
	return string(data)
}

func (s *installStage) dir() string { return filepath.Join(s.root, "fixture") }

// checkPrivate fails when a private value is in the output, an error or the record.
func (s *installStage) checkPrivate(err error) {
	s.t.Helper()
	texts := append(slices.Clone(s.lines), s.file(filepath.Join(s.dir(), "install.json")))
	if err != nil {
		texts = append(texts, err.Error())
	}
	for _, text := range texts {
		for _, private := range []string{fixtureTagValue, fixtureModelKey,
			"fixture-admin-password", "fixture-registry-token"} {
			if strings.Contains(text, private) {
				s.t.Errorf("a private value is in %q", text)
			}
		}
	}
}

func TestInstallRunsEveryStepAndShowsNoPrivateValue(t *testing.T) {
	s := newInstallStage(t)
	result, err := s.install(s.input())
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(s.lines, "\n"))
	}
	if !slices.Equal(s.steps, InstallSteps) {
		t.Errorf("steps = %q", s.steps)
	}
	s.checkPrivate(nil)
	if !slices.ContainsFunc(s.lines, func(line string) bool {
		return strings.Contains(line, `"example:owner" = "(hidden)"`)
	}) {
		t.Error("the plan output of the tags is not in the log with its value hidden")
	}
	if result.DashboardURL != s.healthz.URL || result.AdminEmail != "admin@src-fixture-idp.example.com" ||
		!strings.Contains(result.Passwords, "TF_DATA_DIR="+filepath.Join(s.dir(), "tfdata")) {
		t.Errorf("result = %+v", result)
	}
	for _, path := range []string{"home.tfvars", "install.json"} {
		info, err := os.Stat(filepath.Join(s.dir(), path))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, %v; want 0600", path, info.Mode(), err)
		}
	}
	vars, _, err := ReadHomeVars(filepath.Join(s.dir(), "home.tfvars"))
	if err != nil || vars.Tags["example:owner"] != fixtureTagValue || vars.ModelKey != fixtureModelKey {
		t.Errorf("home.tfvars = %+v, %v", vars, err)
	}
	checkPhases(t, s)
	checkImagesWerePushed(t, s)
}

// checkPhases checks the two applies of the home stack and the bootstrap between them.
func checkPhases(t *testing.T, s *installStage) {
	t.Helper()
	applies := s.calls("tofu stack apply")
	if len(applies) != 2 || strings.Contains(applies[0], "-var=") ||
		!strings.Contains(applies[1], "-var=runner_image_digest=sha256:") ||
		!strings.Contains(applies[1], "-var=core_image_id=ami-0f1c7e") {
		t.Errorf("applies of the home stack = %q", applies)
	}
	if len(s.layers.boots) != 1 {
		t.Fatalf("bootstraps = %+v", s.layers.boots)
	}
	boot := s.layers.boots[0]
	if boot.BrokerPrincipal != "arn:aws:iam::111122223333:role/clanker-fixture-core" ||
		boot.ParameterPath != "/src/fixture" || !strings.HasPrefix(boot.KMSKey, "arn:aws:kms:") ||
		boot.DataDir != filepath.Join(s.dir(), "layers") {
		t.Errorf("bootstrap = %+v", boot)
	}
	record, err := readRecord(filepath.Join(s.dir(), "install.json"))
	if err != nil || !record.Installed || record.Applied.CoreImage == "" ||
		len(record.Images) != 1 || record.Images[0].Nanos != DefaultNanos {
		t.Errorf("record = %+v, %v", record, err)
	}
}

// checkImagesWerePushed checks the runner image and the four releases of the application.
func checkImagesWerePushed(t *testing.T, s *installStage) {
	t.Helper()
	registry := "111122223333.dkr.ecr.us-west-2.amazonaws.com/clanker-fixture"
	if pushes := s.calls("docker push " + registry + "/runner:"); len(pushes) != 1 {
		t.Errorf("runner pushes = %q", pushes)
	}
	builds := s.calls("docker build --platform linux/arm64")
	pushes := s.calls("docker push " + registry + "/paperboat:")
	if len(builds) != 4 || len(pushes) != 4 {
		t.Errorf("application builds %q, pushes %q", builds, pushes)
	}
	if core := s.calls("docker buildx build --platform linux/arm64"); len(core) != 2 ||
		!strings.Contains(core[1], "--target core") {
		t.Errorf("builds of the runner and the core = %q", core)
	}
}

func TestInstallGoesOnFromTheStepThatFailed(t *testing.T) {
	s := newInstallStage(t)
	t.Setenv("SRC_FIXTURE_FAIL", "ops image create")
	_, err := s.install(s.input())
	if err == nil || !strings.Contains(err.Error(), "step 6 of 9") {
		t.Fatalf("the first run ended with %v, want a failure at step 6", err)
	}
	s.checkPrivate(err)
	s.layers.failBoot = true
	if _, err := s.install(s.input()); err == nil || !strings.Contains(err.Error(), "step 7") {
		t.Fatalf("the second run ended with %v, want a failure at step 7", err)
	}
	if _, err := s.install(s.input()); err != nil {
		t.Fatalf("the third run: %v", err)
	}
	if bundles := s.calls("make bundle"); len(bundles) != 1 {
		t.Errorf("the runner image was built %d times, want once", len(bundles))
	}
	if paperboat := s.calls("docker build --platform"); len(paperboat) != 4 {
		t.Errorf("the application was built %d times, want once for each of 4", len(paperboat))
	}
	if images := s.calls("ops image create"); len(images) != 2 {
		t.Errorf("ops ran %d times, want the failed run and one more", len(images))
	}
	for _, apply := range s.calls("tofu stack apply")[:3] {
		if strings.Contains(apply, "-var=") {
			t.Errorf("an apply of phase 1 before phase 2 had inputs of phase 2: %s", apply)
		}
	}
}

func TestInstallAfterAFailedPhaseTwoRemovesItsCoreFirst(t *testing.T) {
	s := newInstallStage(t)
	t.Setenv("SRC_FIXTURE_FAIL", "tofu stack apply -no-color -input=false -auto-approve "+
		"-var-file="+filepath.Join(s.dir(), "home.tfvars")+" -var=runner_image_digest=")
	if _, err := s.install(s.input()); err == nil || !strings.Contains(err.Error(), "step 8") {
		t.Fatalf("the first run ended with %v, want a failure of phase 2", err)
	}
	if _, err := s.install(s.input()); err != nil {
		t.Fatal(err)
	}
	applies := s.calls("tofu stack apply")
	if len(applies) != 4 || strings.Contains(applies[2], "-var=") ||
		!strings.Contains(applies[3], "-var=core_image_id=") {
		t.Errorf("after a failed phase 2 the run must apply phase 1 without the core, then "+
			"phase 2: %q", applies)
	}
}

func TestInstallAgainKeepsPhaseTwo(t *testing.T) {
	s := newInstallStage(t)
	if _, err := s.install(s.input()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.install(s.input()); err != nil {
		t.Fatal(err)
	}
	applies := s.calls("tofu stack apply")
	if len(applies) != 4 || !strings.Contains(applies[2], "-var=core_image_id=") {
		t.Errorf("a run after an installation must apply phase 1 with the image: %q", applies)
	}
	if images := s.calls("ops image create"); len(images) != 1 {
		t.Errorf("ops ran %d times, want once", len(images))
	}
}

func TestInstallBuildsAnotherKernelUnderTheSameName(t *testing.T) {
	s := newInstallStage(t)
	if _, err := s.install(s.input()); err != nil {
		t.Fatal(err)
	}
	in := s.input()
	in.Nanos = "0.1.55"
	if _, err := s.install(in); err != nil {
		t.Fatal(err)
	}
	record, err := readRecord(filepath.Join(s.dir(), "install.json"))
	if err != nil || len(record.Images) != 1 || record.Images[0].Nanos != "0.1.55" ||
		!strings.HasSuffix(record.Images[0].ID, "2") {
		t.Errorf("record = %+v, %v; want only the second image", record.Images, err)
	}
	if !strings.Contains(s.file(filepath.Join(s.state, "count-image")), "2") ||
		strings.Contains(s.file(filepath.Join(s.state, "images")), "00000001") {
		t.Error("the image of the first kernel was not removed")
	}
}

func TestInstallRefusesAZoneThatTheParentDoesNotDelegate(t *testing.T) {
	s := newInstallStage(t)
	in := s.input()
	in.ZoneID = "Z0F1C7E2"
	_, err := s.install(in)
	if err == nil || !strings.Contains(err.Error(), "other name servers") {
		t.Fatalf("err = %v, want the refusal of an orphaned zone", err)
	}
	if applies := s.calls("tofu"); len(applies) != 0 {
		t.Errorf("tofu ran: %q", applies)
	}
}

func TestInstallNeedsTheTagsThatTheOrganizationRequires(t *testing.T) {
	s := newInstallStage(t)
	in := s.input()
	in.Tags = map[string]string{"example:team": "platform-team"}
	_, err := s.install(in)
	if err == nil || !strings.Contains(err.Error(), "requires the tags example:owner") {
		t.Fatalf("err = %v, want the tag that the decoded message names", err)
	}
}

func TestInstallTakesTheValuesOfHomeTfvars(t *testing.T) {
	s := newInstallStage(t)
	if _, err := s.install(s.input()); err != nil {
		t.Fatal(err)
	}
	in := InstallInput{Env: "fixture", Profile: "fixture-operator", Source: s.repo,
		Tags: map[string]string{"example:owner": ""}}
	if _, err := s.install(in); err != nil {
		t.Fatalf("a run with the values of home.tfvars: %v", err)
	}
	vars, _, err := ReadHomeVars(filepath.Join(s.dir(), "home.tfvars"))
	if err != nil || vars.Tags["example:owner"] != fixtureTagValue || vars.Region != "us-west-2" ||
		vars.ModelKey != fixtureModelKey {
		t.Errorf("home.tfvars after the run = %+v, %v", vars, err)
	}
}

func TestInstallWithBedrockChecksEachModelAndKeepsNoKey(t *testing.T) {
	s := newInstallStage(t)
	if _, err := s.install(s.input()); err != nil {
		t.Fatal(err)
	}
	in := s.input()
	in.ModelKey, in.ModelProvider = "", ProviderBedrock
	in.BedrockModels = map[string]string{"reviewer": "global.anthropic.claude-sonnet-5-5"}
	if _, err := s.install(in); err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(s.lines, "\n"))
	}
	vars, _, err := ReadHomeVars(filepath.Join(s.dir(), "home.tfvars"))
	want := map[string]string{"triage": "us.anthropic.claude-haiku-5-5",
		"explainer": "us.anthropic.claude-haiku-5-5", "author": "us.anthropic.claude-sonnet-5-5",
		"investigator": "us.anthropic.claude-sonnet-5-5",
		"reviewer":     "global.anthropic.claude-sonnet-5-5"}
	if err != nil || vars.ModelKey != "" || !maps.Equal(vars.BedrockModels, want) {
		t.Errorf("home.tfvars = key %q, models %v, %v", vars.ModelKey, vars.BedrockModels, err)
	}
	log := s.file(s.awsLog)
	for _, id := range []string{"global.anthropic.claude-sonnet-5-5",
		"us.anthropic.claude-haiku-5-5", "us.anthropic.claude-sonnet-5-5"} {
		for _, call := range []string{"bedrock get-inference-profile " +
			"--inference-profile-identifier " + id, "bedrock-runtime converse --model-id " + id} {
			if strings.Count(log, call+" ") != 1 {
				t.Errorf("the installer did not call aws %s once: %s", call, log)
			}
		}
		if !slices.Contains(s.lines, "Amazon Bedrock in us-west-2 answered with "+id+
			": 3 tokens.") {
			t.Errorf("the log does not say that %s answered", id)
		}
	}
	if !slices.ContainsFunc(s.lines, func(line string) bool {
		return strings.Contains(line, "Bedrock bills each token to this account")
	}) {
		t.Error("the log does not say who bills the tokens")
	}
	s.checkPrivate(nil)
}

func TestInstallRefusesABedrockModelThatTheRegionDoesNotHave(t *testing.T) {
	s := newInstallStage(t)
	in := s.input()
	in.ModelKey, in.ModelProvider = "", ProviderBedrock
	in.BedrockModels = map[string]string{"author": "us.anthropic.claude-absent-1"}
	_, err := s.install(in)
	if err == nil || !strings.Contains(err.Error(), "step 2 of 9") ||
		!strings.Contains(err.Error(), "us.anthropic.claude-absent-1") ||
		!strings.Contains(err.Error(), "bedrock:GetInferenceProfile") {
		t.Fatalf("err = %v, want the profile at step 2", err)
	}
	if applies := s.calls("tofu stack apply"); len(applies) != 0 {
		t.Errorf("the home stack was applied: %q", applies)
	}
	in.BedrockModels = map[string]string{"writer": "us.anthropic.claude-sonnet-5-5"}
	if _, err := s.install(in); err == nil || !strings.Contains(err.Error(), `no role "writer"`) {
		t.Errorf("err = %v, want the unknown role", err)
	}
}

func TestInstallFailsWhenThisMachineCannotReachTheDashboard(t *testing.T) {
	s := newInstallStage(t)
	s.healthz.Close()
	_, err := s.install(s.input())
	if err == nil || !strings.Contains(err.Error(), "reports the core healthy, but this "+
		"machine cannot reach") {
		t.Fatalf("err = %v, want the check from this machine to fail", err)
	}
	record, readErr := readRecord(filepath.Join(s.dir(), "install.json"))
	if readErr != nil || record.Installed {
		t.Errorf("the record says installed: %+v, %v", record, readErr)
	}
}

func TestUninstallRemovesEverythingInTheOrderOfTheTeardown(t *testing.T) {
	s := newInstallStage(t)
	if _, err := s.install(s.input()); err != nil {
		t.Fatal(err)
	}
	if err := s.uninstall(); err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(s.lines, "\n"))
	}
	if !slices.Equal(s.steps, RemoveSteps) {
		t.Errorf("steps = %q", s.steps)
	}
	if !slices.Equal(s.layers.atDestroy, []string{"core-stopped", "tasks-stopped",
		"home-applied"}) {
		t.Errorf("at the destroy of the layers the stand-ins had %q, want the core and the "+
			"tasks stopped and the home stack there", s.layers.atDestroy)
	}
	destroy := s.layers.destroys[0]
	if destroy.Variables["image_registry"] == "" ||
		destroy.Variables["instance_tags"] != `{"example:owner":"`+fixtureTagValue+`"}` ||
		destroy.Variables["operator_principal_arn"] != "arn:aws:iam::111122223333:role/Operator" ||
		destroy.Variables["existing_private_subnet_ids"] != `["subnet-0a","subnet-0b"]` {
		t.Errorf("the destroy of the layers got %v", destroy.Variables)
	}
	for _, name := range []string{"images", "snapshots", "taskdefs"} {
		if left := strings.TrimSpace(s.file(filepath.Join(s.state, name))); left != "" {
			t.Errorf("%s left: %s", name, left)
		}
	}
	record, err := readRecord(filepath.Join(s.dir(), "install.json"))
	if err != nil || record.Installed || len(record.Images) > 0 || record.Outputs.CoreRole != "" {
		t.Errorf("record after the removal = %+v, %v", record, err)
	}
	s.checkPrivate(nil)
}

func TestUninstallReportsWhatIsLeft(t *testing.T) {
	s := newInstallStage(t)
	if _, err := s.install(s.input()); err != nil {
		t.Fatal(err)
	}
	// Without its state the home stack is not destroyed, and its resources stay.
	if err := os.Remove(filepath.Join(s.dir(), "home.tfstate")); err != nil {
		t.Fatal(err)
	}
	err := s.uninstall()
	if err == nil || !strings.Contains(err.Error(), "step 7 of 7") ||
		!strings.Contains(err.Error(), "these are left") {
		t.Fatalf("err = %v, want the check of step 7 to find what is left", err)
	}
	if slices.ContainsFunc(s.lines, func(line string) bool {
		return strings.Contains(line, "is removed")
	}) {
		t.Error("the removal reported success")
	}
}

func TestUninstallWithoutAnInstallationRefuses(t *testing.T) {
	s := newInstallStage(t)
	if err := s.uninstall(); err == nil || !strings.Contains(err.Error(), "home.tfvars") {
		t.Fatalf("err = %v, want the missing inputs", err)
	}
}
