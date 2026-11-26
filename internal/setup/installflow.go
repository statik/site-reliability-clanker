package setup

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/pack"
)

var (
	networkIDPattern = regexp.MustCompile(`^vpc-[0-9a-f]+$`)
	subnetIDPattern  = regexp.MustCompile(`^subnet-[0-9a-f]+$`)
	groupIDPattern   = regexp.MustCompile(`^sg-[0-9a-f]+$`)
)

// installRun is one run of the installer or of the removal.
type installRun struct {
	*Installer
	in      InstallInput
	vars    HomeVars
	home    awsHome
	aws     AWS
	record  installRecord
	out     *lineWriter
	redact  redactor
	stack   homeStack
	program program
	// release is the version of the checkout; zone is the availability zone of the first
	// subnet, where ops writes the snapshot.
	release string
	zone    string
	account string
	// app is the application pack of the checkout.
	app pack.Pack
}

func (i *Installer) begin(in InstallInput, report *Report) (*installRun, error) {
	if report.Step == nil {
		report.Step = func(int, string) {}
	}
	if report.Line == nil {
		report.Line = func(string) {}
	}
	home, err := i.home(in.Env)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(home.dir, 0o700); err != nil {
		return nil, fmt.Errorf("create the directory of the environment %s: %w", home.dir, err)
	}
	if err := os.Chmod(home.dir, 0o700); err != nil {
		return nil, fmt.Errorf("make %s readable only by you: %w", home.dir, err)
	}
	saved, _, err := ReadHomeVars(home.vars())
	if err != nil {
		return nil, err
	}
	record, err := readRecord(home.record())
	if err != nil {
		return nil, err
	}
	vars := merge(in, saved)
	r := &installRun{Installer: i, in: in, vars: vars, home: home, record: record,
		redact: secretsOf(vars)}
	r.record.Env = in.Env
	r.out = &lineWriter{send: report.Line, redact: r.redact}
	run := i.CLI
	if run == nil {
		run = realCLI
	}
	r.aws = AWS{Profile: in.Profile, Region: vars.Region, Env: in.Env, Run: run}
	r.stack = homeStack{home: home, profile: in.Profile, region: vars.Region, out: r.out,
		redact: r.redact}
	r.program = program{out: r.out, redact: r.redact}
	return r, nil
}

func (r *installRun) log(format string, args ...any) {
	fmt.Fprintf(r.out, format+"\n", args...)
}

func (r *installRun) save() error { return writeRecord(r.home.record(), r.record) }

// steps runs the steps in order and stops at the first that fails. A step that a run
// before finished checks that its result still exists and then does nothing again.
func (r *installRun) steps(ctx context.Context, report Report, titles []string,
	steps []func(context.Context) error) error {
	defer r.out.Flush()
	for index, step := range steps {
		report.Step(index, titles[index])
		r.log("== Step %d of %d: %s", index+1, len(steps), titles[index])
		if err := step(ctx); err != nil {
			r.out.Flush()
			return r.redact.err(fmt.Errorf("step %d of %d (%s) failed: %w. Run it again to "+
				"continue from this step", index+1, len(steps), titles[index], err))
		}
	}
	return nil
}

// Install creates the home of the control plane on AWS and starts the control plane. Each
// step can run again: a run after a failure continues where the last one stopped.
func (i *Installer) Install(ctx context.Context, in InstallInput, report Report) (
	InstallResult, error) {
	r, err := i.begin(in, &report)
	if err != nil {
		return InstallResult{}, err
	}
	err = r.steps(ctx, report, InstallSteps, []func(context.Context) error{r.preflight,
		r.checkInputs, r.phaseOne, r.pushRunner, r.pushApplication, r.buildCore, r.bootstrap,
		r.phaseTwo, r.checkHealth})
	if err != nil {
		return InstallResult{}, err
	}
	r.record.Installed = true
	if err := r.save(); err != nil {
		return InstallResult{}, err
	}
	result := r.result()
	r.log("The control plane runs at %s.", result.DashboardURL)
	r.log("Sign in as %s. The passwords of the two users of Keycloak: %s", result.AdminEmail,
		result.Passwords)
	r.log("It costs about %.2f USD each week until you remove it.", result.WeeklyUSD)
	r.out.Flush()
	return result, nil
}

func (r *installRun) result() InstallResult {
	outputs := r.record.Outputs
	return InstallResult{DashboardURL: outputs.CoreURL, KeycloakURL: outputs.IDPURL,
		AdminEmail: outputs.AdminEmail, ApproverEmail: outputs.ApproverEmail,
		Passwords: fmt.Sprintf("cd %s && TF_DATA_DIR=%s tofu output -raw admin_password "+
			"(and approver_password)", r.home.stack(), r.home.data()),
		Directory: r.home.dir, WeeklyUSD: weeklyPrice()}
}

// session checks the names, the session of the profile and the account.
func (r *installRun) session(ctx context.Context) error {
	if err := r.aws.Validate(); err != nil {
		return err
	}
	identity, err := r.aws.WhoAmI(ctx)
	if errors.Is(err, ErrNeedsLogin) {
		return fmt.Errorf("%w. Sign in with `aws sso login --profile %s`, or with the tool "+
			"that gives the profile its credentials, and run it again", err, r.aws.Profile)
	}
	if err != nil {
		return err
	}
	if !accountPattern.MatchString(identity.Account) {
		return fmt.Errorf("the profile %s gave the account %q, which is not an AWS account ID",
			r.aws.Profile, identity.Account)
	}
	r.account = identity.Account
	r.log("Signed in to the account %s as %s, in %s.", identity.Account, identity.Arn,
		r.aws.Region)
	return nil
}

var accountPattern = regexp.MustCompile(`^[0-9]{12}$`)

// preflight checks the session, the tools and the checkout.
func (r *installRun) preflight(ctx context.Context) error {
	if err := r.session(ctx); err != nil {
		return err
	}
	if missing := Missing(InstallChecks()); len(missing) > 0 {
		return fmt.Errorf("this machine lacks %s: `clanker setup` shows how to install each",
			strings.Join(missing, ", "))
	}
	source, err := openCheckout(r.in.Source)
	if err != nil {
		return err
	}
	if r.release, err = source.release(ctx, r.program); err != nil {
		return err
	}
	if r.app, err = source.pack(); err != nil {
		return err
	}
	r.vars.Application = r.app.Name
	if r.Version != "dev" && r.Version != "" && r.Version != r.release {
		return fmt.Errorf("the checkout %s is at %s, and this program is %s: the home stack "+
			"of this program must match the control plane that the checkout builds. Check "+
			"out %s, or run the program of %s", source.dir, r.release, r.Version, r.Version,
			r.release)
	}
	r.log("The checkout %s is at %s. The runner, the core and the application are built "+
		"from it.", source.dir, r.release)
	return nil
}

// checkInputs checks the network, the zone, the tags and the kernel, and writes
// home.tfvars.
func (r *installRun) checkInputs(ctx context.Context) error {
	if err := r.checkNetwork(ctx); err != nil {
		return err
	}
	zone, err := r.aws.PublicZone(ctx, r.vars.ZoneID)
	if err != nil {
		return err
	}
	if !zone.Delegated {
		return fmt.Errorf("the zone %s (%s) cannot hold the names: %s", zone.Name, zone.ID,
			zone.Reason)
	}
	r.log("The names go into the zone %s: src-%s.%s and src-%s-idp.%s.", zone.Name, r.in.Env,
		zone.Name, r.in.Env, zone.Name)
	if r.in.Nanos == "" {
		r.in.Nanos = DefaultNanos
	}
	if !nanosVersionPattern.MatchString(r.in.Nanos) {
		return fmt.Errorf("the Nanos version %q is not valid: use the version of a release, "+
			"for example %s", r.in.Nanos, DefaultNanos)
	}
	if err := r.checkTags(ctx); err != nil {
		return err
	}
	if err := WriteHomeVars(r.home.vars(), r.vars); err != nil {
		return err
	}
	r.log("The inputs are in %s, which only you can read.", r.home.vars())
	return nil
}

// checkNetwork checks that the subnets are private subnets of the network in two zones or
// more, and that the security group of the router is in the network.
func (r *installRun) checkNetwork(ctx context.Context) error {
	v := r.vars
	if !networkIDPattern.MatchString(v.Network) || len(v.Subnets) < zonesNeeded ||
		!groupIDPattern.MatchString(v.RouterGroup) {
		return fmt.Errorf("choose the network: a VPC, %d private subnets or more and the "+
			"security group of its Tailscale subnet router. `clanker discover --profile %s` "+
			"proposes them", zonesNeeded, r.aws.Profile)
	}
	for _, subnet := range v.Subnets {
		if !subnetIDPattern.MatchString(subnet) {
			return fmt.Errorf("%q is not the ID of a subnet", subnet)
		}
	}
	var subnets []struct {
		ID      string `json:"id"`
		Network string `json:"vpc"`
		Zone    string `json:"zone"`
	}
	args := append([]string{"ec2", "describe-subnets", "--subnet-ids"}, v.Subnets...)
	args = append(args, "--query", "Subnets[].{id:SubnetId,vpc:VpcId,zone:AvailabilityZone}")
	if err := r.aws.query(ctx, r.aws.Region, &subnets, args...); err != nil {
		return fmt.Errorf("read the subnets: %w", err)
	}
	zones := map[string]bool{}
	for _, subnet := range subnets {
		if subnet.Network != v.Network {
			return fmt.Errorf("the subnet %s is in %s, not in %s", subnet.ID, subnet.Network,
				v.Network)
		}
		zones[subnet.Zone] = true
		if subnet.ID == v.Subnets[0] {
			r.zone = subnet.Zone
		}
	}
	if len(subnets) != len(v.Subnets) || len(zones) < zonesNeeded {
		return fmt.Errorf("the subnets are in %d availability zones: the database needs %d",
			len(zones), zonesNeeded)
	}
	var groups []string
	if err := r.aws.query(ctx, r.aws.Region, &groups, "ec2", "describe-security-groups",
		"--group-ids", v.RouterGroup, "--query", "SecurityGroups[].VpcId"); err != nil {
		return fmt.Errorf("read the security group of the router: %w", err)
	}
	if len(groups) != 1 || groups[0] != v.Network {
		return fmt.Errorf("the security group %s of the router is not in %s", v.RouterGroup,
			v.Network)
	}
	r.log("The network %s with the subnets %s in %d zones. The load balancer admits the "+
		"router of %s.", v.Network, strings.Join(v.Subnets, ", "), len(zones), v.RouterGroup)
	return nil
}

// checkTags checks the required tags with a dry run of a launch. A rule that names a tag
// that the input lacks stops the run. A refusal for another reason does not: the dry run
// uses a public image, which a rule of the organization can refuse by itself.
func (r *installRun) checkTags(ctx context.Context) error {
	for key, value := range r.vars.Tags {
		if strings.TrimSpace(key) == "" || value == "" {
			return fmt.Errorf("the required tag %q has no value: give one, or remove the tag", key)
		}
	}
	r.log("The required tags: %s. Their values are not shown.",
		strings.Join(slices.Sorted(maps.Keys(r.vars.Tags)), ", "))
	check, err := r.aws.CheckLaunch(ctx, r.vars.Subnets[0], r.vars.Tags)
	switch {
	case err != nil:
		r.log("The dry run of a launch did not answer, so the tags are not checked: %v", err)
	case check.Allowed:
		r.log("A dry run of a launch of the core with these tags is allowed.")
	default:
		missing := []string{}
		for _, key := range check.RequiredTags {
			if _, given := r.vars.Tags[key]; !given {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("the organization requires the tags %s on a launch: add them "+
				"with their values", strings.Join(missing, ", "))
		}
		r.log("A dry run of a launch was refused, and the tags are not the reason: %s. The "+
			"installation goes on; the start of the core in phase 2 can fail for it.",
			check.Reason)
	}
	return nil
}

// phaseOne applies the home stack with the inputs of phase 2 of the last apply that
// passed, so that a run after an installation keeps the core. After a phase 2 that failed,
// the record has no inputs of phase 2, and this apply removes what that phase 2 made: a
// core that did not start, for example with a kernel that does not boot.
func (r *installRun) phaseOne(ctx context.Context) error {
	if err := r.stack.prepare(ctx); err != nil {
		return err
	}
	if err := r.stack.apply(ctx, r.record.Applied); err != nil {
		return err
	}
	return r.readOutputs(ctx)
}

func (r *installRun) readOutputs(ctx context.Context) error {
	outputs, err := r.stack.outputs(ctx)
	if err != nil {
		return err
	}
	if outputs.CoreRole == "" || outputs.ParameterKey == "" || outputs.RunnerRepository == "" {
		return errors.New("the home stack has no core role, key or runner repository in its " +
			"outputs")
	}
	r.record.Outputs = outputs
	return r.save()
}

func (r *installRun) bootstrap(ctx context.Context) error {
	outputs := r.record.Outputs
	return r.Layers.Bootstrap(ctx, LayerRequest{Env: r.in.Env, Profile: r.in.Profile,
		Region: r.aws.Region, BrokerPrincipal: outputs.CoreRole,
		ParameterPath: outputs.ParameterPath, KMSKey: outputs.ParameterKey,
		DataDir: r.home.layers(), Log: r.out})
}

// phaseTwo applies the home stack with the runner image and the image of the core. The
// apply ends when the core answers /healthz behind the load balancer.
func (r *installRun) phaseTwo(ctx context.Context) error {
	image, found := r.currentImage()
	if !found || r.record.Runner.Digest == "" {
		return errors.New("the runner image or the image of the core is missing: run the " +
			"installation from its first step")
	}
	phase := phaseVars{RunnerDigest: r.record.Runner.Digest, CoreImage: image.ID}
	if err := r.stack.apply(ctx, phase); err != nil {
		return err
	}
	r.record.Applied = phase
	if err := r.save(); err != nil {
		return err
	}
	return r.readOutputs(ctx)
}
