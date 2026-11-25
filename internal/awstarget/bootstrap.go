package awstarget

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// bootstrapOutputs are the outputs of layer 00-bootstrap on AWS, as
// platform/stacks/aws/00-bootstrap/outputs.tf declares them.
var bootstrapOutputs = []string{"clanker_ttl", "evidence_bucket", "identities",
	"permission_guard", "state_backend", "workload_role_path"}

// backendSettings are the keys of the output state_backend. The control plane adds the
// key of each layer itself.
var backendSettings = []string{"bucket", "encrypt", "region", "use_lockfile"}

// Bootstrap is what the control plane needs from layer 00-bootstrap on AWS. An operator
// applies that layer from a laptop with `clanker bootstrap`, so its outputs come to the
// control plane as a setting and not from an apply job.
type Bootstrap struct {
	// Outputs are the values of the outputs by name, as an apply job reports them.
	Outputs map[string]any
	// Roles gives the role ARN of each identity: the output identities.
	Roles map[string]string
	// Backend is the output state_backend: the settings of the S3 backend of the other
	// layers, without the key of a layer.
	Backend map[string]string
	// Region is the region of the environment: the region of its state bucket.
	Region string
}

// output is one output as `tofu output -json` prints it.
type output struct {
	Sensitive *bool           `json:"sensitive"`
	Type      json.RawMessage `json:"type"`
	Value     json.RawMessage `json:"value"`
}

// strictDecode decodes text into v and refuses a field that v does not have.
func strictDecode(text []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(text))
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
}

// ParseBootstrap reads what `tofu output -json` prints for layer 00-bootstrap on AWS. It
// refuses an output that the layer does not have, an output that is missing, a sensitive
// output and a state_backend or identities value that is not valid.
func ParseBootstrap(text string) (Bootstrap, error) {
	raw := map[string]output{}
	if err := strictDecode([]byte(text), &raw); err != nil {
		return Bootstrap{}, fmt.Errorf("the outputs of layer 00-bootstrap are not what `tofu "+
			"output -json` prints: %w", err)
	}
	b := Bootstrap{Outputs: map[string]any{}}
	for name := range raw {
		if !slices.Contains(bootstrapOutputs, name) {
			return Bootstrap{}, fmt.Errorf("layer 00-bootstrap has no output %q: the outputs "+
				"are %s", name, strings.Join(bootstrapOutputs, ", "))
		}
	}
	for _, name := range bootstrapOutputs {
		value, err := outputValue(raw, name)
		if err != nil {
			return Bootstrap{}, err
		}
		b.Outputs[name] = value
	}
	var err error
	if b.Backend, err = parseBackend(raw["state_backend"].Value); err != nil {
		return Bootstrap{}, err
	}
	b.Region = b.Backend["region"]
	if err := json.Unmarshal(raw["identities"].Value, &b.Roles); err != nil {
		return Bootstrap{}, fmt.Errorf("the output identities of layer 00-bootstrap is not a "+
			"map of text values: %w", err)
	}
	if err := checkRoles(b.Roles); err != nil {
		return Bootstrap{}, fmt.Errorf("the output identities of layer 00-bootstrap: %w", err)
	}
	return b, nil
}

// outputValue returns the value of one output, which must be there and not be sensitive.
func outputValue(raw map[string]output, name string) (any, error) {
	o, found := raw[name]
	if !found {
		return nil, fmt.Errorf("the output %s of layer 00-bootstrap is missing: give every "+
			"output that `tofu output -json` prints for the layer", name)
	}
	if o.Sensitive == nil || o.Value == nil {
		return nil, fmt.Errorf("the output %s of layer 00-bootstrap has no `sensitive` or no "+
			"`value`: give it as `tofu output -json` prints it", name)
	}
	if *o.Sensitive {
		return nil, fmt.Errorf("the output %s of layer 00-bootstrap is sensitive: the setting "+
			"holds no secret, and the layer has no sensitive output", name)
	}
	var value any
	if err := json.Unmarshal(o.Value, &value); err != nil {
		return nil, fmt.Errorf("the value of the output %s of layer 00-bootstrap: %w", name, err)
	}
	return value, nil
}

// parseBackend reads the output state_backend: exactly the settings in backendSettings,
// each with a value.
func parseBackend(value json.RawMessage) (map[string]string, error) {
	backend := map[string]string{}
	if err := json.Unmarshal(value, &backend); err != nil {
		return nil, fmt.Errorf("the output state_backend of layer 00-bootstrap is not a map of "+
			"text values: %w", err)
	}
	for name := range backend {
		if !slices.Contains(backendSettings, name) {
			return nil, fmt.Errorf("the output state_backend of layer 00-bootstrap has the "+
				"setting %q, which the control plane does not give to a layer: the settings "+
				"are %s", name, strings.Join(backendSettings, ", "))
		}
	}
	for _, name := range backendSettings {
		if backend[name] == "" {
			return nil, fmt.Errorf("the output state_backend of layer 00-bootstrap has no "+
				"value for %s", name)
		}
	}
	return backend, nil
}

// checkRoles refuses roles that name no identity or that are not role ARNs.
func checkRoles(roles map[string]string) error {
	if len(roles) == 0 {
		return errors.New("the roles of the identities name no identity")
	}
	for identity, arn := range roles {
		if !roleARN.MatchString(arn) {
			return fmt.Errorf("the role of the identity %s is %q, which is not the ARN of an "+
				"IAM role", identity, arn)
		}
	}
	return nil
}
