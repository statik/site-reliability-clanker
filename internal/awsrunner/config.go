package awsrunner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// Config says where the tasks of the runners run. The setting CLANKER_AWS_RUNNER holds it
// as JSON. ParseConfig reads it.
type Config struct {
	// Cluster is the name or the ARN of the ECS cluster.
	Cluster string `json:"cluster"`
	// TaskDefinition is the family, family:revision or ARN of the task definition of the
	// runner.
	TaskDefinition string `json:"task_definition"`
	// Container is the name of the runner container in the task definition. It gets the
	// address of the core and the token of the job.
	Container string `json:"container"`
	// Subnets are the IDs of the private subnets of the tasks.
	Subnets []string `json:"subnets"`
	// SecurityGroups are the IDs of the security groups of the tasks.
	SecurityGroups []string `json:"security_groups"`
}

var (
	subnetID        = regexp.MustCompile(`^subnet-[0-9a-f]+$`)
	securityGroupID = regexp.MustCompile(`^sg-[0-9a-f]+$`)
)

// ParseConfig reads the setting of the runners as a JSON object with the fields cluster,
// task_definition, container, subnets and security_groups. It refuses another field and an
// empty value.
func ParseConfig(text string) (Config, error) {
	decoder := json.NewDecoder(bytes.NewReader([]byte(text)))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("the runner setting is not a JSON object with the fields "+
			"cluster, task_definition, container, subnets and security_groups: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("the runner setting has more text after its JSON object")
	}
	texts := []struct{ field, value string }{{"cluster", config.Cluster},
		{"task_definition", config.TaskDefinition}, {"container", config.Container}}
	for _, text := range texts {
		if text.value == "" {
			return Config{}, fmt.Errorf("the runner setting has no %s", text.field)
		}
	}
	if err := checkIDs("subnets", "subnet", config.Subnets, subnetID); err != nil {
		return Config{}, err
	}
	if err := checkIDs("security_groups", "security group", config.SecurityGroups,
		securityGroupID); err != nil {
		return Config{}, err
	}
	return config, nil
}

// checkIDs refuses an empty list, and an ID that does not have the shape of its kind.
func checkIDs(field, kind string, ids []string, shape *regexp.Regexp) error {
	if len(ids) == 0 {
		return fmt.Errorf("the runner setting names no %s in %s", kind, field)
	}
	for index, id := range ids {
		if !shape.MatchString(id) {
			return fmt.Errorf("item %d of %s in the runner setting is %q, which is not the ID "+
				"of a %s", index+1, field, id, kind)
		}
	}
	return nil
}
