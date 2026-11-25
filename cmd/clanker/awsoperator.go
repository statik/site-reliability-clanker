package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/awstarget"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/runner"
	"github.com/statik/site-reliability-clanker/internal/setup"
)

// standardParameterLimit is the largest value of a parameter in the standard tier of
// Parameter Store, in bytes.
const standardParameterLimit = 4096

// stateBucketModule starts the address of each resource of the state bucket in the state
// of layer 00-bootstrap (module "state_bucket" in platform/stacks/aws/00-bootstrap).
const stateBucketModule = "module.state_bucket."

var (
	rolePattern          = regexp.MustCompile(`^arn:aws[a-z-]*:iam::[0-9]{12}:role/.+$`)
	parameterPathPattern = regexp.MustCompile(`^(/[A-Za-z0-9_.-]+)+$`)
	accountPattern       = regexp.MustCompile(`^[0-9]{12}$`)
)

// runAWS runs the aws program and returns what it printed. The error has the end of what it
// printed on standard error.
func runAWS(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "aws", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(stderr.String())
		if len(text) > 600 {
			text = text[len(text)-600:]
		}
		return nil, fmt.Errorf("aws %s: %w: %s", strings.Join(args[:min(2, len(args))], " "),
			err, text)
	}
	return stdout.Bytes(), nil
}

// awsTarget is an environment on AWS as the commands of the operator see it: the session
// of a profile of the AWS CLI, and the bucket that holds the state of the layers.
type awsTarget struct {
	o      options
	run    setup.CLI
	r      *runner.Runner
	bucket string
}

// openAWSTarget checks the session of the profile and finds the bucket of the environment.
func openAWSTarget(ctx context.Context, o options, run setup.CLI) (awsTarget, error) {
	if o.profile == "" || o.region == "" {
		return awsTarget{}, errors.New("name the AWS CLI profile and the region of the " +
			"environment: --profile NAME --region NAME")
	}
	session := setup.AWS{Profile: o.profile, Region: o.region, Env: o.env, Run: run}
	if err := session.Validate(); err != nil {
		return awsTarget{}, err
	}
	identity, err := session.WhoAmI(ctx)
	if errors.Is(err, setup.ErrNeedsLogin) {
		return awsTarget{}, fmt.Errorf("%w. Sign in with `aws sso login --profile %s`, or "+
			"with the tool that gives the profile its credentials, and run the command again",
			err, o.profile)
	}
	if err != nil {
		return awsTarget{}, err
	}
	if !accountPattern.MatchString(identity.Account) {
		return awsTarget{}, fmt.Errorf("the profile %s gave the account %q, which is not an "+
			"AWS account ID", o.profile, identity.Account)
	}
	r, err := runner.New(o.dataDir)
	if err != nil {
		return awsTarget{}, err
	}
	// OpenTofu and the programs that it starts, such as `aws eks get-token`, use the session
	// of the profile. Keys in the environment of the shell would win over the profile, so
	// the jobs get empty ones.
	r.ExtraEnv = []string{"AWS_PROFILE=" + o.profile, "AWS_REGION=" + o.region,
		"AWS_ACCESS_KEY_ID=", "AWS_SECRET_ACCESS_KEY=", "AWS_SESSION_TOKEN="}
	return awsTarget{o: o, run: run, r: r, bucket: stateBucket(o.env, identity.Account)}, nil
}

// stateBucket is the bucket that layer 00-bootstrap creates for the state of the other
// layers. Its name follows the evidence bucket of the layer, clanker-<env>-evidence-<account>.
func stateBucket(env, account string) string {
	return "clanker-" + env + "-state-" + account
}

// cli runs the aws program with the profile and the region of the environment.
func (t awsTarget) cli(ctx context.Context, args ...string) ([]byte, error) {
	return t.run(ctx, append(args, "--profile", t.o.profile, "--region", t.o.region,
		"--output", "json")...)
}

// vars are the inputs that a job gives to each layer on AWS (platform/CONTRACT.md).
func (t awsTarget) vars() map[string]string {
	return map[string]string{"env_name": t.o.env, "state_location": t.bucket,
		"region": t.o.region}
}

// layerJob returns a job for a layer above 00-bootstrap, with its state in the bucket.
func (t awsTarget) layerJob(layer string, log io.Writer) runner.Job {
	return runner.Job{Env: t.o.env, Cloud: "aws", Layer: layer,
		Checkout: runner.Checkout{Dir: t.o.sourceDir},
		Backend:  runner.S3Backend(t.bucket, t.o.region, t.o.env, layer), Vars: t.vars(),
		Log: log}
}

// bootstrapJobs returns the two jobs of layer 00-bootstrap in the copy: with its state in
// the bucket, and with its state in the state directory.
func (t awsTarget) bootstrapJobs(w workCopy, log io.Writer) (runner.Job, runner.Job) {
	vars := t.vars()
	vars["broker_principal"] = t.o.brokerPrincipal
	if t.o.parameterPath != "" {
		vars["core_parameter_path"] = t.o.parameterPath
	}
	inBucket := runner.Job{Env: t.o.env, Cloud: "aws", Layer: bootstrapLayer,
		Checkout: runner.Checkout{Dir: w.source}, DataDir: w.dataDir, Vars: vars, Log: log,
		Backend: runner.S3Backend(t.bucket, t.o.region, t.o.env, bootstrapLayer)}
	local := inBucket
	local.Backend, local.LocalState = nil, true
	return inBucket, local
}

// localState reports whether the state of layer 00-bootstrap is in the state directory:
// before it moved into the bucket, or after it moved back for a destroy. The file is then
// newer than a copy in the bucket.
func (t awsTarget) localState() (bool, error) {
	_, err := os.Stat(t.r.StatePath(t.o.env, bootstrapLayer))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check the state file of %s: %w", bootstrapLayer, err)
	}
	return true, nil
}

// removeLocalState removes the state file of layer 00-bootstrap and its backup.
func (t awsTarget) removeLocalState() error {
	path := t.r.StatePath(t.o.env, bootstrapLayer)
	for _, file := range []string{path, path + ".backup"} {
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove the state file %s: %w", file, err)
		}
	}
	return nil
}

// stateInBucket reports whether the bucket holds the state of layer 00-bootstrap. A
// bucket that does not exist holds none.
func (t awsTarget) stateInBucket(ctx context.Context) (bool, error) {
	key := t.o.env + "/" + bootstrapLayer + ".tfstate"
	_, err := t.cli(ctx, "s3api", "head-object", "--bucket", t.bucket, "--key", key)
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "(404)") {
		return false, nil
	}
	return false, fmt.Errorf("check whether the bucket %s holds the state of %s: %w",
		t.bucket, bootstrapLayer, err)
}

// quiet returns the job with no log, for a check whose result the command prints itself.
func quiet(j runner.Job) runner.Job {
	j.Log = nil
	return j
}

// checkBrokerPrincipal refuses a value of --broker-principal that is not an IAM role ARN.
func checkBrokerPrincipal(arn string) error {
	if !rolePattern.MatchString(arn) {
		return fmt.Errorf("--broker-principal %q is not valid: give the ARN of the IAM role "+
			"that the control plane runs as, from the outputs of its home stack", arn)
	}
	return nil
}

func checkBootstrapFlags(o options) error {
	if err := checkBrokerPrincipal(o.brokerPrincipal); err != nil {
		return err
	}
	if !parameterPathPattern.MatchString(o.parameterPath) {
		return fmt.Errorf("--parameter-path %q is not valid: give the Parameter Store path of "+
			"the settings of the control plane, for example /src/prod, with no / at the end",
			o.parameterPath)
	}
	return nil
}

// bootstrapAWS applies layer 00-bootstrap with the session of the profile, moves its state
// into the bucket that it creates, and prints the setting CLANKER_AWS_BOOTSTRAP. With a
// KMS key it also writes the setting to Parameter Store.
func bootstrapAWS(ctx context.Context, o options, run setup.CLI) error {
	if err := checkBootstrapFlags(o); err != nil {
		return err
	}
	t, err := openAWSTarget(ctx, o, run)
	if err != nil {
		return err
	}
	w, err := newWorkCopy(t.r, o.source, o.env)
	if err != nil {
		return err
	}
	inBucket, local := t.bootstrapJobs(w, os.Stdout)
	moved, err := t.applyBootstrap(ctx, w, inBucket, local)
	if err != nil {
		return err
	}
	setting, err := t.settingFromBucket(ctx, inBucket)
	if err != nil {
		return err
	}
	if moved {
		if err := t.removeLocalState(); err != nil {
			return err
		}
	}
	return t.writeSetting(ctx, w, setting)
}

// settingFromBucket reads the state of layer 00-bootstrap from the bucket, which must hold
// its resources, and returns its outputs as the setting CLANKER_AWS_BOOTSTRAP.
func (t awsTarget) settingFromBucket(ctx context.Context, inBucket runner.Job) ([]byte, error) {
	resources, err := t.r.Resources(ctx, quiet(inBucket))
	if err != nil {
		return nil, fmt.Errorf("read the state of %s from the bucket %s: %w", bootstrapLayer,
			t.bucket, err)
	}
	if len(resources) == 0 {
		return nil, fmt.Errorf("the bucket %s holds no resource of %s after the apply",
			t.bucket, bootstrapLayer)
	}
	setting, err := t.r.PrintedOutputs(ctx, quiet(inBucket))
	if err != nil {
		return nil, err
	}
	// The control plane reads the setting with the same check at each start.
	if _, err := awstarget.ParseBootstrap(string(setting)); err != nil {
		return nil, fmt.Errorf("the outputs of %s are not a valid %s: %w", bootstrapLayer,
			bootstrapEnv, err)
	}
	fmt.Printf("\nBootstrap of %s is complete. The bucket %s holds the state of %s: %d "+
		"resources.\n", t.o.env, t.bucket, bootstrapLayer, len(resources))
	return setting, nil
}

// applyBootstrap applies layer 00-bootstrap. When the bucket holds its state, it applies
// with that state. Otherwise it applies with a local state, as on the first run when the
// bucket does not exist yet, and then moves the state into the bucket. It reports whether
// it moved the state.
func (t awsTarget) applyBootstrap(ctx context.Context, w workCopy, inBucket,
	local runner.Job) (bool, error) {
	isLocal, err := t.localState()
	if err != nil {
		return false, err
	}
	if !isLocal {
		held, err := t.stateInBucket(ctx)
		if err != nil {
			return false, err
		}
		if held {
			fmt.Printf("The bucket %s holds the state of %s/%s. Applying the layer with it.\n",
				t.bucket, t.o.env, bootstrapLayer)
			_, err := t.r.ApplyDirect(ctx, inBucket)
			return false, err
		}
	}
	fmt.Printf("Applying layer %s of %s with the session of the profile %s. Its state stays "+
		"in %s until the bucket %s holds it.\n", bootstrapLayer, t.o.env, t.o.profile,
		t.r.StatePath(t.o.env, bootstrapLayer), t.bucket)
	if err := w.setLocalBackend("aws", true); err != nil {
		return false, err
	}
	if _, err := t.r.ApplyDirect(ctx, local); err != nil {
		return false, err
	}
	if err := w.setLocalBackend("aws", false); err != nil {
		return false, err
	}
	fmt.Printf("\nMoving the state of %s into the bucket %s.\n", bootstrapLayer, t.bucket)
	return true, t.r.MoveState(ctx, inBucket)
}

// writeSetting prints the setting CLANKER_AWS_BOOTSTRAP and, with a KMS key, writes it
// below the parameter path of the control plane as a SecureString.
func (t awsTarget) writeSetting(ctx context.Context, w workCopy, setting []byte) error {
	name := t.o.parameterPath + "/" + bootstrapEnv
	fmt.Printf("\nThe setting %s of the control plane. It holds no secret:\n%s\n",
		bootstrapEnv, setting)
	if t.o.kmsKey == "" {
		fmt.Printf("\nIt was not written to Parameter Store. With --kms-key KEY the command "+
			"writes it as %s.\n", name)
		return nil
	}
	if len(setting) > standardParameterLimit {
		return fmt.Errorf("the setting %s has %d bytes, more than the %d of a parameter in "+
			"the standard tier: it was not written", bootstrapEnv, len(setting),
			standardParameterLimit)
	}
	file := filepath.Join(filepath.Dir(w.dataDir), "setting.json")
	if err := os.WriteFile(file, setting, 0o600); err != nil {
		return fmt.Errorf("write the setting %s to %s: %w", bootstrapEnv, file, err)
	}
	defer os.Remove(file)
	if _, err := t.cli(ctx, "ssm", "put-parameter", "--name", name, "--type", "SecureString",
		"--key-id", t.o.kmsKey, "--value", "file://"+file, "--overwrite"); err != nil {
		return fmt.Errorf("write the setting %s to Parameter Store as %s: %w", bootstrapEnv,
			name, err)
	}
	fmt.Printf("\nWritten to Parameter Store as %s, a SecureString with the KMS key %s.\n",
		name, t.o.kmsKey)
	return nil
}

// upperLayersTopDown returns the layers above 00-bootstrap, the top one first.
func upperLayersTopDown() []string {
	layers := slices.Clone(orchestrator.Layers[1:])
	slices.Reverse(layers)
	return layers
}

// survey is what the check of an environment found before anything is destroyed.
type survey struct {
	// resources is the number of managed resources in the state of each layer.
	resources map[string]int
	// localBootstrap says that the state of 00-bootstrap is in the state directory.
	localBootstrap bool
	// upperChecked says that the state of each layer above 00-bootstrap was read. It is
	// false when the local state of 00-bootstrap has no state bucket, so no such state exists.
	upperChecked bool
}

// destroyAWS destroys each layer of an AWS environment that has resources, from 50-app down
// to 10-network, then moves the state of 00-bootstrap out of its bucket and destroys it.
// It reads the state of every layer first, and destroys nothing when one cannot be read.
func destroyAWS(ctx context.Context, o options, run setup.CLI) error {
	if err := checkBrokerPrincipal(o.brokerPrincipal); err != nil {
		return err
	}
	t, err := openAWSTarget(ctx, o, run)
	if err != nil {
		return err
	}
	w, err := newWorkCopy(t.r, o.source, o.env)
	if err != nil {
		return err
	}
	found, err := t.check(ctx, w)
	if err != nil {
		return err
	}
	if err := t.destroyUpperLayers(ctx, found); err != nil {
		return err
	}
	if err := t.destroyBootstrap(ctx, w, found); err != nil {
		return err
	}
	fmt.Printf("\n%s\n", found.result(o.env))
	return nil
}

// result is the last line of a destroy that passed. It names the layers that were not
// checked.
func (s survey) result(env string) string {
	if !s.upperChecked {
		return fmt.Sprintf("Environment %s is destroyed: layer %s was checked and each of its "+
			"resources was destroyed. The layers above it were not checked: their state has no "+
			"bucket, because the state of %s has no state bucket.", env, bootstrapLayer,
			bootstrapLayer)
	}
	return fmt.Sprintf("Environment %s is destroyed: each layer was checked, and each layer "+
		"with resources was destroyed.", env)
}

// check reads the state of each layer and prints how many resources it has. It fails with
// the name of the first layer whose state it cannot read.
func (t awsTarget) check(ctx context.Context, w workCopy) (survey, error) {
	local, err := t.localState()
	if err != nil {
		return survey{}, err
	}
	found := survey{resources: map[string]int{}, localBootstrap: local}
	inBucket, inFile := t.bootstrapJobs(w, nil)
	job, where := inBucket, "in the bucket "+t.bucket
	if local {
		if err := w.setLocalBackend("aws", true); err != nil {
			return survey{}, err
		}
		job, where = inFile, "in "+t.r.StatePath(t.o.env, bootstrapLayer)
	}
	addresses, err := t.r.Resources(ctx, job)
	if err != nil {
		return survey{}, fmt.Errorf("check layer %s of %s %s: %w. Nothing was destroyed",
			bootstrapLayer, t.o.env, where, err)
	}
	found.resources[bootstrapLayer] = len(addresses)
	fmt.Printf("Resources in the state of each layer of %s:\n", t.o.env)
	if local && !slices.ContainsFunc(addresses, isStateBucket) {
		fmt.Printf("  10-network to 50-app: no state, because the state of %s has no state "+
			"bucket\n", bootstrapLayer)
	} else {
		if err := t.checkUpperLayers(ctx, found); err != nil {
			return survey{}, err
		}
		found.upperChecked = true
	}
	fmt.Printf("  %-13s %d (state %s)\n", bootstrapLayer, len(addresses), where)
	return found, nil
}

func isStateBucket(address string) bool { return strings.HasPrefix(address, stateBucketModule) }

// checkUpperLayers reads the state of each layer above 00-bootstrap from the bucket.
func (t awsTarget) checkUpperLayers(ctx context.Context, found survey) error {
	for _, layer := range upperLayersTopDown() {
		addresses, err := t.r.Resources(ctx, t.layerJob(layer, nil))
		if err != nil {
			return fmt.Errorf("check layer %s of %s in the bucket %s: %w. Nothing was destroyed",
				layer, t.o.env, t.bucket, err)
		}
		found.resources[layer] = len(addresses)
		fmt.Printf("  %-13s %d\n", layer, len(addresses))
	}
	return nil
}

// destroyUpperLayers destroys each layer above 00-bootstrap that has resources, the top
// layer first.
func (t awsTarget) destroyUpperLayers(ctx context.Context, found survey) error {
	for _, layer := range upperLayersTopDown() {
		if found.resources[layer] == 0 {
			continue
		}
		fmt.Printf("\nDestroying %s/%s\n", t.o.env, layer)
		if err := t.r.Destroy(ctx, t.layerJob(layer, os.Stdout)); err != nil {
			return err
		}
	}
	return nil
}

// destroyBootstrap destroys layer 00-bootstrap from a local state. A state in the bucket
// moves out first: the destroy removes the bucket.
func (t awsTarget) destroyBootstrap(ctx context.Context, w workCopy, found survey) error {
	if found.resources[bootstrapLayer] == 0 {
		fmt.Printf("\nLayer %s of %s has no resources.\n", bootstrapLayer, t.o.env)
		return t.removeLocalState()
	}
	_, local := t.bootstrapJobs(w, os.Stdout)
	if !found.localBootstrap {
		if err := w.setLocalBackend("aws", true); err != nil {
			return err
		}
		fmt.Printf("\nMoving the state of %s from the bucket %s to %s.\n", bootstrapLayer,
			t.bucket, t.r.StatePath(t.o.env, bootstrapLayer))
		if err := t.r.MoveState(ctx, local); err != nil {
			return err
		}
	}
	fmt.Printf("\nDestroying %s/%s\n", t.o.env, bootstrapLayer)
	if err := t.r.Destroy(ctx, local); err != nil {
		return err
	}
	return t.removeLocalState()
}
