package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/awstarget"
)

func TestBootstrapOnAWSMovesTheStateIntoTheBucketAndWritesTheSetting(t *testing.T) {
	f := newOperatorFake(t)
	if err := bootstrapAWS(context.Background(), f.options(), f.aws); err != nil {
		t.Fatal(err)
	}
	vars := "-var=broker_principal=arn:aws:iam::000000000000:role/test-core " +
		"-var=core_parameter_path=/src/test -var=env_name=test-env -var=region=us-east-1 " +
		"-var=state_location=clanker-test-env-state-000000000000"
	s3 := "-backend-config=bucket=clanker-test-env-state-000000000000 " +
		"-backend-config=encrypt=true -backend-config=key=test-env/00-bootstrap.tfstate " +
		"-backend-config=region=us-east-1 -backend-config=use_lockfile=true"
	want := []string{
		"00-bootstrap|local-backend|init -no-color -input=false -backend-config=path=" +
			f.localState(),
		"00-bootstrap|local-backend|apply -no-color -input=false -auto-approve " + vars,
		"00-bootstrap|local-backend|output -json",
		"00-bootstrap|own-backend|init -no-color -input=false -migrate-state -force-copy " + s3,
		"00-bootstrap|own-backend|init -no-color -input=false " + s3,
		"00-bootstrap|own-backend|state pull",
		"00-bootstrap|own-backend|init -no-color -input=false " + s3,
		"00-bootstrap|own-backend|output -json",
	}
	if got := f.calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("tofu was called with\n%s\nwant\n%s", strings.Join(got, "\n"),
			strings.Join(want, "\n"))
	}
	if !exists(t, f.s3Path("00-bootstrap")) {
		t.Error("the bucket does not hold the state of 00-bootstrap")
	}
	if exists(t, f.localState()) {
		t.Error("the local state file is left after the move into the bucket")
	}
	checkWrittenSetting(t, f)
}

// checkWrittenSetting checks that the fake put-parameter got the setting as a SecureString
// below the parameter path, in the form that the control plane reads.
func checkWrittenSetting(t *testing.T, f *operatorFake) {
	t.Helper()
	wantPut := []string{"ssm", "put-parameter", "--name", "/src/test/CLANKER_AWS_BOOTSTRAP",
		"--type", "SecureString", "--key-id", "alias/test"}
	if len(f.put) < len(wantPut) || !slices.Equal(f.put[:len(wantPut)], wantPut) ||
		!slices.Contains(f.put, "--overwrite") {
		t.Errorf("put-parameter was called with %q", f.put)
	}
	parsed, err := awstarget.ParseBootstrap(f.value)
	if err != nil {
		t.Fatalf("the control plane cannot read the setting %s: %v", f.value, err)
	}
	if parsed.Outputs["evidence_bucket"] != "evidence" || strings.Contains(f.value, "secret") {
		t.Errorf("the setting is %s, want the outputs without the sensitive one", f.value)
	}
}

func TestBootstrapOnAWSRunsWithTheSessionOfTheProfile(t *testing.T) {
	f := newOperatorFake(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "key-of-the-shell")
	if err := bootstrapAWS(context.Background(), f.options(), f.aws); err != nil {
		t.Fatal(err)
	}
	session, err := os.ReadFile(filepath.Join(f.dir, "session"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(session)); got != "test-profile|" {
		t.Errorf("tofu ran with the profile and the key %q, want the profile and no key", got)
	}
	for _, call := range f.awsCalls[:2] {
		if !strings.HasSuffix(call, "--profile test-profile --region us-east-1 --output json") {
			t.Errorf("the aws program was called without the profile: %q", call)
		}
	}
}

func TestBootstrapOnAWSLeavesNothingInTheSourceFiles(t *testing.T) {
	f := newOperatorFake(t)
	stack := filepath.Join(f.source, "platform", "stacks", "aws", "00-bootstrap")
	f.write(filepath.Join(stack, ".terraform", "terraform.tfstate"), `{"backend":"old"}`)
	f.write(filepath.Join(stack, "terraform.tfstate"), appliedState)
	before := listFiles(t, f.source)
	if err := bootstrapAWS(context.Background(), f.options(), f.aws); err != nil {
		t.Fatal(err)
	}
	if after := listFiles(t, f.source); !slices.Equal(before, after) {
		t.Errorf("the source files changed from %q to %q", before, after)
	}
	copied := filepath.Join(f.dataDir, "work", "bootstrap", "test-env", "source", "platform",
		"stacks", "aws", "00-bootstrap")
	for _, name := range []string{".terraform", "terraform.tfstate", "backend_override.tf"} {
		if exists(t, filepath.Join(copied, name)) {
			t.Errorf("the copy of the stack has %s", name)
		}
	}
}

func listFiles(t *testing.T, root string) []string {
	t.Helper()
	files := []string{}
	err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		files = append(files, path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestBootstrapOnAWSAppliesWithTheStateThatTheBucketHolds(t *testing.T) {
	f := newOperatorFake(t)
	f.stateInBucket = true
	f.write(f.s3Path("00-bootstrap"), appliedState)
	if err := bootstrapAWS(context.Background(), f.options(), f.aws); err != nil {
		t.Fatal(err)
	}
	for _, call := range f.calls() {
		if strings.Contains(call, "local-backend") || strings.Contains(call, "-migrate-state") {
			t.Errorf("a bootstrap with the state in the bucket called %q", call)
		}
	}
	if len(f.callsOf("00-bootstrap", "apply")) != 1 {
		t.Errorf("the layer was not applied once: %q", f.calls())
	}
}

func TestBootstrapOnAWSMovesALocalStateFromAnEarlierRunAgain(t *testing.T) {
	f := newOperatorFake(t)
	f.stateInBucket = true
	f.write(f.localState(), appliedState)
	if err := bootstrapAWS(context.Background(), f.options(), f.aws); err != nil {
		t.Fatal(err)
	}
	if len(f.callsOf("00-bootstrap", "init -no-color -input=false -migrate-state")) != 1 {
		t.Errorf("the local state was not moved into the bucket: %q", f.calls())
	}
	for _, call := range f.awsCalls {
		if strings.HasPrefix(call, "s3api head-object") {
			t.Error("the bucket was asked, but the local state is newer than a copy there")
		}
	}
}

func TestBootstrapOnAWSWithoutAKMSKeyWritesNoParameter(t *testing.T) {
	f := newOperatorFake(t)
	o := f.options()
	o.kmsKey = ""
	if err := bootstrapAWS(context.Background(), o, f.aws); err != nil {
		t.Fatal(err)
	}
	if f.put != nil {
		t.Errorf("put-parameter was called with %q", f.put)
	}
}

func TestBootstrapOnAWSChecksTheFlagsBeforeAnyCall(t *testing.T) {
	cases := map[string]func(*options){
		"no broker principal": func(o *options) { o.brokerPrincipal = "" },
		"a user as broker principal": func(o *options) {
			o.brokerPrincipal = "arn:aws:iam::000000000000:user/x"
		},
		"no parameter path":            func(o *options) { o.parameterPath = "" },
		"a parameter path with a /":    func(o *options) { o.parameterPath = "/src/" },
		"no profile":                   func(o *options) { o.profile = "" },
		"no region":                    func(o *options) { o.region = "" },
		"an environment name with _":   func(o *options) { o.env = "test_env" },
		"a region that does not exist": func(o *options) { o.region = "mars" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newOperatorFake(t)
			o := f.options()
			change(&o)
			if err := bootstrapAWS(context.Background(), o, f.aws); err == nil {
				t.Fatal("no error")
			}
			if len(f.awsCalls) != 0 || len(f.calls()) != 0 {
				t.Fatalf("calls before the flags were checked: aws %q, tofu %q", f.awsCalls,
					f.calls())
			}
		})
	}
}

// withLayerStates puts states into the fake bucket: 30-data and 10-network have resources,
// 40-platform was applied and destroyed, and 50-app and 20-cluster were never applied.
func withLayerStates(f *operatorFake) {
	f.write(f.s3Path("00-bootstrap"), appliedState)
	f.write(f.s3Path("30-data"), appliedState)
	f.write(f.s3Path("10-network"), appliedState)
	f.write(f.s3Path("40-platform"), `{"version":4,"resources":[]}`)
}

func TestDestroyOnAWSDestroysTheLayersWithResourcesAfterCheckingEach(t *testing.T) {
	f := newOperatorFake(t)
	withLayerStates(f)
	if err := destroyAWS(context.Background(), f.options(), f.aws); err != nil {
		t.Fatal(err)
	}
	destroyed, pulls, lastPull, firstDestroy := destroyOrder(f.calls())
	if want := []string{"30-data", "10-network", "00-bootstrap"}; !slices.Equal(destroyed, want) {
		t.Errorf("destroyed %q, want %q", destroyed, want)
	}
	if pulls != 6 || lastPull > firstDestroy {
		t.Errorf("%d layers were checked, the last at call %d and the first destroy at %d: "+
			"want all 6 checked before any destroy", pulls, lastPull, firstDestroy)
	}
	if destroys := f.callsOf("30-data", "destroy"); !strings.HasSuffix(destroys[0],
		"-var=env_name=test-env -var=region=us-east-1 "+
			"-var=state_location=clanker-test-env-state-000000000000") {
		t.Errorf("the destroy of 30-data has not the variables of the job: %q", destroys[0])
	}
	checkBootstrapMovedOut(t, f)
}

// checkBootstrapMovedOut checks that the state of 00-bootstrap moved once from the bucket to
// the local file, and that the file is gone after the destroy.
func checkBootstrapMovedOut(t *testing.T, f *operatorFake) {
	t.Helper()
	moves := f.callsOf("00-bootstrap", "init -no-color -input=false -migrate-state -force-copy "+
		"-backend-config=path="+f.localState())
	if len(moves) != 1 || !strings.Contains(moves[0], "|local-backend|") {
		t.Errorf("the state of 00-bootstrap was not moved out of the bucket once: %q", moves)
	}
	if exists(t, f.localState()) {
		t.Error("the local state file of 00-bootstrap is left after its destroy")
	}
}

// destroyOrder returns the layers that the calls destroyed, in order, the number of state
// reads, the index of the last read and the index of the first destroy.
func destroyOrder(calls []string) ([]string, int, int, int) {
	destroyed, pulls, lastPull, firstDestroy := []string{}, 0, -1, -1
	for i, call := range calls {
		parts := strings.SplitN(call, "|", 3)
		if strings.HasPrefix(parts[2], "state pull") {
			pulls, lastPull = pulls+1, i
		}
		if strings.HasPrefix(parts[2], "destroy") {
			destroyed = append(destroyed, parts[0])
			if firstDestroy < 0 {
				firstDestroy = i
			}
		}
	}
	return destroyed, pulls, lastPull, firstDestroy
}

func TestDestroyOnAWSDestroysNothingWhenALayerCannotBeRead(t *testing.T) {
	f := newOperatorFake(t)
	withLayerStates(f)
	f.write(f.s3Path("20-cluster"), "unreadable")
	err := destroyAWS(context.Background(), f.options(), f.aws)
	if err == nil || !strings.Contains(err.Error(), "check layer 20-cluster") {
		t.Fatalf("error = %v, want one that names 20-cluster", err)
	}
	for _, call := range f.calls() {
		if strings.Contains(call, "|destroy") || strings.Contains(call, "-migrate-state") {
			t.Errorf("a failed check was followed by %q", call)
		}
	}
}

func TestDestroyOnAWSDestroysNothingWhenTheBootstrapStateCannotBeRead(t *testing.T) {
	f := newOperatorFake(t)
	withLayerStates(f)
	f.write(f.s3Path("00-bootstrap"), "unreadable")
	err := destroyAWS(context.Background(), f.options(), f.aws)
	if err == nil || !strings.Contains(err.Error(), "check layer 00-bootstrap") {
		t.Fatalf("error = %v, want one that names 00-bootstrap", err)
	}
	if len(f.callsOf("30-data", "")) != 0 {
		t.Errorf("a layer was used after the failed check: %q", f.calls())
	}
}

func TestDestroyOnAWSUsesALocalStateOfBootstrapWithoutABucket(t *testing.T) {
	f := newOperatorFake(t)
	f.write(f.localState(), stateWithoutBucket)
	if err := destroyAWS(context.Background(), f.options(), f.aws); err != nil {
		t.Fatal(err)
	}
	for _, layer := range upperLayersTopDown() {
		if calls := f.callsOf(layer, ""); len(calls) != 0 {
			t.Errorf("layer %s was asked although no state bucket exists: %q", layer, calls)
		}
	}
	moves := f.callsOf("00-bootstrap", "init -no-color -input=false -migrate-state")
	if len(moves) != 0 {
		t.Errorf("the local state was replaced by a move: %q", moves)
	}
	if len(f.callsOf("00-bootstrap", "destroy")) != 1 || exists(t, f.localState()) {
		t.Errorf("00-bootstrap was not destroyed from its local state: %q", f.calls())
	}
}

func TestTheCheckSaysWhetherItReadTheLayersAboveBootstrap(t *testing.T) {
	for local, want := range map[string]bool{stateWithoutBucket: false, appliedState: true} {
		f := newOperatorFake(t)
		f.write(f.localState(), local)
		target, err := openAWSTarget(context.Background(), f.options(), f.aws)
		if err != nil {
			t.Fatal(err)
		}
		w, err := newWorkCopy(target.r, f.options().source, "test-env")
		if err != nil {
			t.Fatal(err)
		}
		found, err := target.check(context.Background(), w)
		if err != nil || found.upperChecked != want {
			t.Errorf("local state %s: upper layers checked = %v, error = %v, want %v", local,
				found.upperChecked, err, want)
		}
	}
}

func TestTheResultOfADestroySaysWhichLayersWereNotChecked(t *testing.T) {
	inferred := survey{}.result("prod")
	if !strings.Contains(inferred, "were not checked") || strings.Contains(inferred, "each layer") {
		t.Errorf("a destroy that read only 00-bootstrap ends with %q", inferred)
	}
	checked := survey{upperChecked: true}.result("prod")
	if !strings.Contains(checked, "each layer was checked") {
		t.Errorf("a destroy that read every layer ends with %q", checked)
	}
}

func TestDestroyOnAWSChecksTheLayersWhenTheLocalStateHasTheBucket(t *testing.T) {
	f := newOperatorFake(t)
	f.write(f.localState(), appliedState)
	f.write(f.s3Path("10-network"), appliedState)
	f.write(f.s3Path("00-bootstrap"), "unreadable")
	if err := destroyAWS(context.Background(), f.options(), f.aws); err != nil {
		t.Fatal(err)
	}
	if len(f.callsOf("10-network", "destroy")) != 1 {
		t.Errorf("10-network was not destroyed: %q", f.calls())
	}
	if len(f.callsOf("00-bootstrap", "init -no-color -input=false -migrate-state")) != 0 {
		t.Error("the copy in the bucket replaced the newer local state")
	}
}

func TestDestroyOnAWSChecksTheBrokerPrincipalBeforeAnyCall(t *testing.T) {
	f := newOperatorFake(t)
	o := f.options()
	o.brokerPrincipal = "core"
	if err := destroyAWS(context.Background(), o, f.aws); err == nil {
		t.Fatal("no error for a broker principal that is not a role ARN")
	}
	if len(f.awsCalls) != 0 || len(f.calls()) != 0 {
		t.Fatalf("calls before the flags were checked: aws %q, tofu %q", f.awsCalls, f.calls())
	}
}

func TestTheBootstrapStackHasTheStateBucketModule(t *testing.T) {
	main, err := os.ReadFile(filepath.Join("..", "..", "platform", "stacks", "aws",
		"00-bootstrap", "main.tf"))
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimSuffix(strings.TrimPrefix(stateBucketModule, "module."), ".")
	if !strings.Contains(string(main), `module "`+name+`" {`) {
		t.Fatalf("layer 00-bootstrap has no module %q: the destroy looks for it in the state",
			name)
	}
}

func TestDestroyOfAnotherCloudIsRefused(t *testing.T) {
	err := destroy(context.Background(), options{cloud: "gcp"})
	if err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("error = %v, want a refusal", err)
	}
}

func TestBootstrapOnAWSStopsWhenTheOutputsAreNotAValidSetting(t *testing.T) {
	f := newOperatorFake(t)
	f.write(filepath.Join(f.dir, "outputs.json"),
		`{"evidence_bucket": {"sensitive": false, "type": "string", "value": "evidence"}}`)
	err := bootstrapAWS(context.Background(), f.options(), f.aws)
	if err == nil || !strings.Contains(err.Error(), "CLANKER_AWS_BOOTSTRAP") {
		t.Fatalf("error = %v, want a refusal of the setting", err)
	}
	if f.put != nil {
		t.Errorf("a setting that is not valid was written: %q", f.put)
	}
}
