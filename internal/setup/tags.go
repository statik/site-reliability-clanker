package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// dryRunImage names a public arm64 image, so that a dry run of a launch needs no image of
// the installer.
const dryRunImage = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"

var (
	encodedMessage = regexp.MustCompile(`Encoded authorization failure message: (\S+)`)
	requestTag     = regexp.MustCompile(`aws:RequestTag/([^"\\\s]+)`)
)

// LaunchCheck is the answer of a dry run of ec2:RunInstances in a subnet.
type LaunchCheck struct {
	// Allowed is true when the launch would succeed.
	Allowed bool `json:"allowed"`
	// RequiredTags are the tag keys that a rule names in its refusal. The values of a
	// person's tags are not in it.
	RequiredTags []string `json:"required_tags"`
	// Reason says why the launch would fail, when no rule names a tag.
	Reason string `json:"reason,omitempty"`
}

// CheckLaunch makes a dry run of a launch of the instance type of the core in the subnet,
// with the tags on the instance and its volume. A refusal of the organization comes as an
// encoded message; its decoded text names the tags that a rule requires. No error and no
// result holds a value of the tags.
func (a AWS) CheckLaunch(ctx context.Context, subnet string, tags map[string]string) (
	LaunchCheck, error) {
	var image string
	if err := a.query(ctx, a.Region, &image, "ssm", "get-parameter", "--name", dryRunImage,
		"--query", "Parameter.Value"); err != nil {
		return LaunchCheck{}, fmt.Errorf("find a public image for the dry run: %w", err)
	}
	args := []string{"ec2", "run-instances", "--dry-run", "--image-id", image,
		"--instance-type", coreFlavor, "--subnet-id", subnet}
	if len(tags) > 0 {
		specification, err := tagSpecifications(tags)
		if err != nil {
			return LaunchCheck{}, err
		}
		args = append(args, "--tag-specifications", specification)
	}
	_, err := a.cli(ctx, args...)
	if err == nil {
		return LaunchCheck{}, errors.New("the dry run of ec2 run-instances started an " +
			"instance or gave no answer: look for an instance in " + subnet)
	}
	answer := stderrOf(err)
	switch {
	case strings.Contains(answer, "DryRunOperation"):
		return LaunchCheck{Allowed: true}, nil
	case strings.Contains(answer, "UnauthorizedOperation"):
		return a.refusal(ctx, answer)
	default:
		return LaunchCheck{}, fmt.Errorf("the dry run of a launch failed: %w", err)
	}
}

// refusal decodes the authorization message of a refused dry run and finds the tag keys
// that its rules name.
func (a AWS) refusal(ctx context.Context, answer string) (LaunchCheck, error) {
	check := LaunchCheck{RequiredTags: []string{}}
	match := encodedMessage.FindStringSubmatch(answer)
	if match == nil {
		check.Reason = "the organization refused the launch and gave no reason"
		return check, nil
	}
	var decoded string
	if err := a.query(ctx, a.Region, &decoded, "sts", "decode-authorization-message",
		"--encoded-message", match[1], "--query", "DecodedMessage"); err != nil {
		check.Reason = "the organization refused the launch, and its message could not be " +
			"decoded: " + err.Error()
		return check, nil
	}
	found := map[string]bool{}
	for _, key := range requestTag.FindAllStringSubmatch(decoded, -1) {
		found[key[1]] = true
	}
	check.RequiredTags = slices.Sorted(maps.Keys(found))
	if len(check.RequiredTags) == 0 {
		check.Reason = "the organization refused the launch for a reason that names no tag"
	}
	return check, nil
}

// tagSpecifications returns the tags of the instance and of its volume as the JSON that
// the aws program takes.
func tagSpecifications(tags map[string]string) (string, error) {
	type tag struct {
		Key   string `json:"Key"`
		Value string `json:"Value"`
	}
	list := []tag{}
	for _, key := range slices.Sorted(maps.Keys(tags)) {
		list = append(list, tag{Key: key, Value: tags[key]})
	}
	specification := []map[string]any{
		{"ResourceType": "instance", "Tags": list},
		{"ResourceType": "volume", "Tags": list},
	}
	text, err := json.Marshal(specification)
	if err != nil {
		return "", fmt.Errorf("encode the tags of the dry run: %w", err)
	}
	return string(text), nil
}
