package setup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
)

// RemoveSteps are the steps of the removal, in the order of the teardown of #133 and of the
// README of the home stack.
var RemoveSteps = []string{
	"Check this machine and the AWS session",
	"Stop the control plane",
	"Stop the runner tasks",
	"Destroy the layers of the environment",
	"Destroy the home of the control plane",
	"Remove the machine images and the task definitions",
	"Check that nothing is left",
}

// Uninstall removes everything that the installation created, in the order of the
// teardown, and checks at the end that nothing with the name or the tag of the environment
// is left. Each step checks before it reports.
func (i *Installer) Uninstall(ctx context.Context, in InstallInput, report Report) error {
	r, err := i.begin(in, &report)
	if err != nil {
		return err
	}
	if _, err := os.Stat(r.home.vars()); err != nil {
		return fmt.Errorf("the environment %s has no %s: it was not installed from this "+
			"machine, or its inputs are gone", in.Env, r.home.vars())
	}
	err = r.steps(ctx, report, RemoveSteps, []func(context.Context) error{r.removalPreflight,
		r.stopCore, r.stopTasks, r.destroyLayers, r.destroyHome, r.removeImages, r.prove})
	if err != nil {
		return err
	}
	r.record = installRecord{Env: in.Env}
	if err := r.save(); err != nil {
		return err
	}
	r.log("The environment %s is removed: each step was checked. Its inputs stay in %s.",
		in.Env, r.home.vars())
	r.out.Flush()
	return nil
}

func (r *installRun) stateExists() (bool, error) {
	_, err := os.Stat(r.home.state())
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// removalPreflight checks the session and reads the outputs of the home stack from its
// state, which knows more than the record when a run stopped.
func (r *installRun) removalPreflight(ctx context.Context) error {
	if err := r.session(ctx); err != nil {
		return err
	}
	if _, err := LookPath("tofu"); err != nil {
		return errors.New("tofu is not installed: brew install opentofu")
	}
	exists, err := r.stateExists()
	if err != nil || !exists {
		return err
	}
	if err := r.stack.prepare(ctx); err != nil {
		return err
	}
	resources, err := r.stack.resources(ctx)
	if err != nil || len(resources) == 0 {
		return err
	}
	outputs, err := r.stack.outputs(ctx)
	if err != nil {
		return err
	}
	r.record.Outputs = outputs
	return r.save()
}

func (r *installRun) names() (group, cluster, runnerFamily, keycloakFamily string) {
	name := "clanker-" + r.in.Env
	return name + "-core", name, name + "-runner", name + "-keycloak"
}

// stopCore takes the core out of service first, so that it starts no runner task during
// the teardown.
func (r *installRun) stopCore(ctx context.Context) error {
	group, _, _, _ := r.names()
	count := func() (int, error) {
		var instances []int
		err := r.aws.query(ctx, r.aws.Region, &instances, "autoscaling",
			"describe-auto-scaling-groups", "--auto-scaling-group-names", group, "--query",
			"AutoScalingGroups[].length(Instances)")
		if err != nil || len(instances) == 0 {
			return -1, err
		}
		return instances[0], nil
	}
	instances, err := count()
	if err != nil {
		return err
	}
	if instances < 0 {
		r.log("The auto scaling group %s does not exist: the core does not run.", group)
		return nil
	}
	if _, err := r.aws.cli(ctx, "autoscaling", "update-auto-scaling-group",
		"--auto-scaling-group-name", group, "--min-size", "0", "--desired-capacity", "0"); err != nil {
		return fmt.Errorf("stop the core: %w", err)
	}
	stopped, err := r.poll(ctx, 60, func() (bool, error) {
		instances, err := count()
		return instances <= 0, err
	})
	if err != nil {
		return err
	}
	if !stopped {
		return fmt.Errorf("the auto scaling group %s still has an instance", group)
	}
	r.log("The core is stopped: the auto scaling group %s has no instance.", group)
	return nil
}

// stopTasks stops the runner tasks. A cluster with a running task cannot be deleted.
func (r *installRun) stopTasks(ctx context.Context) error {
	_, cluster, family, _ := r.names()
	var active []string
	if err := r.aws.query(ctx, r.aws.Region, &active, "ecs", "describe-clusters", "--clusters",
		cluster, "--query", "clusters[?status=='ACTIVE'].clusterArn"); err != nil {
		return err
	}
	if len(active) == 0 {
		r.log("The cluster %s does not exist: no runner task runs.", cluster)
		return nil
	}
	var tasks []string
	if err := r.aws.query(ctx, r.aws.Region, &tasks, "ecs", "list-tasks", "--cluster",
		cluster, "--family", family, "--query", "taskArns"); err != nil {
		return err
	}
	for _, task := range tasks {
		if _, err := r.aws.cli(ctx, "ecs", "stop-task", "--cluster", cluster, "--task", task,
			"--reason", "removal of the environment"); err != nil {
			return fmt.Errorf("stop the runner task %s: %w", task, err)
		}
	}
	stopped, err := r.poll(ctx, 30, func() (bool, error) {
		var running []string
		err := r.aws.query(ctx, r.aws.Region, &running, "ecs", "list-tasks", "--cluster",
			cluster, "--family", family, "--desired-status", "RUNNING", "--query", "taskArns")
		if err != nil || len(running) > 0 || len(tasks) == 0 {
			return len(running) == 0, err
		}
		var moving []string
		args := append([]string{"ecs", "describe-tasks", "--cluster", cluster, "--tasks"},
			tasks...)
		err = r.aws.query(ctx, r.aws.Region, &moving, append(args, "--query",
			"tasks[?lastStatus!='STOPPED'].taskArn")...)
		return len(moving) == 0, err
	})
	if err != nil {
		return err
	}
	if !stopped {
		return fmt.Errorf("runner tasks in %s did not stop", cluster)
	}
	r.log("No runner task runs in %s. Stopped tasks: %d.", cluster, len(tasks))
	return nil
}

// destroyLayers destroys the layers from 50-app down and 00-bootstrap. Its agent roles
// trust the role of the core, so they go before the home stack.
func (r *installRun) destroyLayers(ctx context.Context) error {
	outputs := r.record.Outputs
	if outputs.CoreRole == "" {
		bucket := "clanker-" + r.in.Env + "-state-" + r.account
		_, err := r.aws.cli(ctx, "s3api", "head-bucket", "--bucket", bucket)
		if err == nil {
			return fmt.Errorf("the state bucket %s of the layers exists, and the home stack "+
				"has no role of the core, which its destroy needs: run `clanker destroy "+
				"--cloud aws` with --broker-principal", bucket)
		}
		if !strings.Contains(stderrOf(err), "(404)") {
			return fmt.Errorf("check the state bucket %s: %w", bucket, err)
		}
		r.log("No layer exists: the home stack has no role of the core, and the state "+
			"bucket %s does not exist.", bucket)
		return nil
	}
	identity, err := r.aws.WhoAmI(ctx)
	if err != nil {
		return err
	}
	operator, err := r.aws.principalARN(ctx, identity)
	if err != nil {
		return fmt.Errorf("find the role of %s: %w", identity.Arn, err)
	}
	variables, err := layerVariables(r.vars, outputs, operator)
	if err != nil {
		return err
	}
	return r.Layers.Destroy(ctx, LayerRequest{Env: r.in.Env, Profile: r.in.Profile,
		Region: r.aws.Region, BrokerPrincipal: outputs.CoreRole,
		ParameterPath: outputs.ParameterPath, KMSKey: outputs.ParameterKey,
		DataDir: r.home.layers(), Variables: variables, Log: r.out})
}

// destroyHome destroys the home stack and checks that its state is empty.
func (r *installRun) destroyHome(ctx context.Context) error {
	exists, err := r.stateExists()
	if err != nil {
		return err
	}
	if !exists {
		r.log("The home stack has no state at %s: it was never applied.", r.home.state())
		return nil
	}
	if err := r.stack.destroy(ctx, r.record.Applied); err != nil {
		return err
	}
	r.record.Applied, r.record.Application = phaseVars{}, ""
	r.record.Runner.Release, r.record.Runner.Digest = "", ""
	r.log("The home stack is destroyed: its state holds no resource.")
	return r.save()
}

// removeImages removes the machine images of the core, recorded or tagged with the
// environment, and the task definitions that the destroy left inactive.
func (r *installRun) removeImages(ctx context.Context) error {
	var tagged []Image
	if err := r.aws.query(ctx, r.aws.Region, &tagged, "ec2", "describe-images", "--owners",
		"self", "--filters", "Name=tag:"+planTagKey+",Values="+r.in.Env, "--query",
		"Images[].{id:ImageId,snapshot_id:BlockDeviceMappings[0].Ebs.SnapshotId,"+
			"name:Tags[?Key=='Name']|[0].Value}"); err != nil {
		return fmt.Errorf("look for the images of %s: %w", r.in.Env, err)
	}
	images := map[string]Image{}
	for _, image := range r.record.Images {
		images[image.ID] = image.Image
	}
	for _, image := range tagged {
		images[image.ID] = image
	}
	for _, id := range slices.Sorted(maps.Keys(images)) {
		if err := r.aws.RemoveImage(ctx, images[id]); err != nil {
			return err
		}
		r.log("Removed the image %s and its snapshot %s.", id, images[id].SnapshotID)
	}
	r.record.Images = nil
	if err := r.save(); err != nil {
		return err
	}
	_, _, runner, keycloak := r.names()
	for _, family := range []string{runner, keycloak} {
		if err := r.deleteTaskDefinitions(ctx, family); err != nil {
			return err
		}
	}
	return nil
}

// taskDefinitions lists the revisions of one family with a status.
func (r *installRun) taskDefinitions(ctx context.Context, family, status string) ([]string,
	error) {
	var arns []string
	if err := r.aws.query(ctx, r.aws.Region, &arns, "ecs", "list-task-definitions",
		"--family-prefix", family, "--status", status, "--query",
		"taskDefinitionArns"); err != nil {
		return nil, err
	}
	exact := []string{}
	for _, arn := range arns {
		name, _, _ := strings.Cut(arn[strings.LastIndex(arn, "/")+1:], ":")
		if name == family {
			exact = append(exact, arn)
		}
	}
	return exact, nil
}

// deleteTaskDefinitions deletes the inactive revisions of a family. A destroy only
// deregisters them.
func (r *installRun) deleteTaskDefinitions(ctx context.Context, family string) error {
	active, err := r.taskDefinitions(ctx, family, "ACTIVE")
	if err != nil {
		return err
	}
	if len(active) > 0 {
		return fmt.Errorf("the task definitions %s are still active after the destroy",
			strings.Join(active, ", "))
	}
	inactive, err := r.taskDefinitions(ctx, family, "INACTIVE")
	if err != nil {
		return err
	}
	for chunk := range slices.Chunk(inactive, describeLimit) {
		args := append([]string{"ecs", "delete-task-definitions", "--task-definitions"},
			chunk...)
		if _, err := r.aws.cli(ctx, args...); err != nil {
			return fmt.Errorf("delete the task definitions of %s: %w", family, err)
		}
	}
	r.log("The family %s has no task definition left: %d were deleted.", family,
		len(inactive))
	return nil
}
