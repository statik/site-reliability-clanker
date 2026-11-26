package setup

import (
	"context"
	"fmt"
	"strings"
)

// leftoverQuery is one list of the account that must be empty after the removal.
type leftoverQuery struct {
	what string
	args []string
}

// leftoverQueries are the lists of the README of the home stack: the parts that the tag of
// the environment does not find, and the parts outside the state.
func (r *installRun) leftoverQueries() []leftoverQuery {
	env := r.in.Env
	name := "clanker-" + env
	tag := "Name=tag:" + planTagKey + ",Values=" + env
	queries := []leftoverQuery{
		{"IAM roles", []string{"iam", "list-roles", "--query",
			"Roles[?starts_with(RoleName, '" + name + "-')].RoleName"}},
		{"IAM policies", []string{"iam", "list-policies", "--scope", "Local", "--query",
			"Policies[?starts_with(PolicyName, '" + name + "-')].PolicyName"}},
		{"instance profiles", []string{"iam", "list-instance-profiles", "--query",
			"InstanceProfiles[?starts_with(InstanceProfileName, '" + name +
				"-')].InstanceProfileName"}},
		{"log groups", []string{"logs", "describe-log-groups", "--log-group-name-prefix",
			"/clanker/" + env + "/", "--query", "logGroups[].logGroupName"}},
		{"parameters", []string{"ssm", "get-parameters-by-path", "--path",
			parameterPath(env), "--recursive", "--query", "Parameters[].Name"}},
		{"repositories", []string{"ecr", "describe-repositories", "--query",
			"repositories[?starts_with(repositoryName, '" + name + "/')].repositoryName"}},
		{"key aliases", []string{"kms", "list-aliases", "--query",
			"Aliases[?AliasName=='alias/" + name + "-settings'].AliasName"}},
		{"machine images", []string{"ec2", "describe-images", "--owners", "self",
			"--filters", tag, "--query", "Images[].ImageId"}},
		{"snapshots", []string{"ec2", "describe-snapshots", "--owner-ids", "self",
			"--filters", tag, "--query", "Snapshots[].SnapshotId"}},
		{"active task definitions", []string{"ecs", "list-task-definitions",
			"--family-prefix", name + "-", "--query", "taskDefinitionArns"}},
		{"inactive task definitions", []string{"ecs", "list-task-definitions",
			"--family-prefix", name + "-", "--status", "INACTIVE", "--query",
			"taskDefinitionArns"}},
	}
	if r.vars.ZoneID != "" {
		queries = append(queries, leftoverQuery{"records of the zone", []string{"route53",
			"list-resource-record-sets", "--hosted-zone-id", r.vars.ZoneID, "--query",
			"ResourceRecordSets[?contains(Name, 'src-" + env + ".') || contains(Name, 'src-" +
				env + "-idp.')].Name"}})
	}
	return queries
}

// prove lists what is left with the name or the tag of the environment. A key that waits
// for its deletion and a task definition that is being deleted are not left.
func (r *installRun) prove(ctx context.Context) error {
	left := []string{}
	for _, query := range r.leftoverQueries() {
		var found []string
		if err := r.aws.query(ctx, r.aws.Region, &found, query.args...); err != nil {
			return fmt.Errorf("list the %s: %w", query.what, err)
		}
		for _, item := range found {
			left = append(left, query.what+": "+item)
		}
	}
	tagged, err := r.taggedLeft(ctx)
	if err != nil {
		return err
	}
	left = append(left, tagged...)
	for _, bucket := range []string{"state", "evidence"} {
		name := "clanker-" + r.in.Env + "-" + bucket + "-" + r.account
		_, err := r.aws.cli(ctx, "s3api", "head-bucket", "--bucket", name)
		if err == nil {
			left = append(left, "bucket: "+name)
		} else if !strings.Contains(stderrOf(err), "(404)") {
			return fmt.Errorf("check the bucket %s: %w", name, err)
		}
	}
	if len(left) > 0 {
		return fmt.Errorf("these are left: %s. The tagging API can lag behind a delete by "+
			"some minutes: run the removal again to check again", strings.Join(left, "; "))
	}
	r.log("Nothing is left: no resource with the tag %s=%s, and no role, log group, "+
		"parameter, repository, key alias, image, snapshot, task definition, record or "+
		"bucket with the name of the environment.", planTagKey, r.in.Env)
	return nil
}

// taggedLeft lists the resources that the tagging API finds with the tag of the
// environment.
func (r *installRun) taggedLeft(ctx context.Context) ([]string, error) {
	var arns []string
	if err := r.aws.query(ctx, r.aws.Region, &arns, "resourcegroupstaggingapi",
		"get-resources", "--tag-filters", "Key="+planTagKey+",Values="+r.in.Env, "--query",
		"ResourceTagMappingList[].ResourceARN"); err != nil {
		return nil, fmt.Errorf("list the resources with the tag %s=%s: %w", planTagKey,
			r.in.Env, err)
	}
	left := []string{}
	for _, arn := range arns {
		gone, err := r.goingAway(ctx, arn)
		if err != nil {
			return nil, err
		}
		if !gone {
			left = append(left, "tagged: "+arn)
		}
	}
	return left, nil
}

// goingAway reports whether a tagged resource waits for its deletion: a KMS key in its
// waiting period, or a task definition that ECS deletes.
func (r *installRun) goingAway(ctx context.Context, arn string) (bool, error) {
	var state string
	switch {
	case strings.Contains(arn, ":kms:") && strings.Contains(arn, ":key/"):
		if err := r.aws.query(ctx, r.aws.Region, &state, "kms", "describe-key", "--key-id",
			arn, "--query", "KeyMetadata.KeyState"); err != nil {
			return false, fmt.Errorf("read the state of the key %s: %w", arn, err)
		}
		return state == "PendingDeletion", nil
	case strings.Contains(arn, ":task-definition/"):
		err := r.aws.query(ctx, r.aws.Region, &state, "ecs", "describe-task-definition",
			"--task-definition", arn, "--query", "taskDefinition.status")
		if err != nil && strings.Contains(stderrOf(err), "ClientException") {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("read the state of %s: %w", arn, err)
		}
		return state == "DELETE_IN_PROGRESS", nil
	}
	return false, nil
}
