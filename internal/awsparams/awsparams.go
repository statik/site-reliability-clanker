// Package awsparams reads the settings of the control plane from AWS Systems Manager
// Parameter Store, so that the machine image carries no secret.
package awsparams

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// allowedPrefixes are the names of the settings that a parameter can give. A parameter
// with another name is an error, so that nobody sets a variable such as PATH from the store.
var allowedPrefixes = []string{"CLANKER_", "OPENROUTER_"}

// Store is the one call of Parameter Store that this package uses.
type Store interface {
	GetParametersByPath(ctx context.Context, in *ssm.GetParametersByPathInput,
		options ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
}

// NewStore returns Parameter Store in the region, with the credentials of the role of the
// instance.
func NewStore(region string) Store {
	return ssm.New(ssm.Options{Region: region,
		Credentials: aws.NewCredentialsCache(ec2rolecreds.New())})
}

// Load returns the settings below the path: for the parameter /src/prod/CLANKER_ADMIN_EMAIL
// and the path /src/prod, the setting CLANKER_ADMIN_EMAIL. It decrypts secure parameters.
func Load(ctx context.Context, store Store, path string) (map[string]string, error) {
	if !strings.HasPrefix(path, "/") || strings.Trim(path, "/") == "" {
		return nil, fmt.Errorf("the parameter path %q is not valid: use a path such as /src/prod", path)
	}
	path = strings.TrimRight(path, "/")
	settings := map[string]string{}
	var next *string
	for {
		page, err := store.GetParametersByPath(ctx, &ssm.GetParametersByPathInput{
			Path: aws.String(path), WithDecryption: aws.Bool(true), NextToken: next})
		if err != nil {
			return nil, fmt.Errorf("read the parameters below %s: %w. The role of the instance "+
				"needs ssm:GetParametersByPath on that path, and kms:Decrypt for its key", path, err)
		}
		if err := add(settings, path, page.Parameters); err != nil {
			return nil, err
		}
		if next = page.NextToken; next == nil {
			break
		}
	}
	if len(settings) == 0 {
		return nil, fmt.Errorf("there is no parameter below %s", path)
	}
	return settings, nil
}

// add puts the parameters of one page into the settings.
func add(settings map[string]string, path string, parameters []types.Parameter) error {
	for _, parameter := range parameters {
		name := strings.TrimPrefix(aws.ToString(parameter.Name), path+"/")
		if !allowed(name) {
			return fmt.Errorf("the parameter %s is not a setting of the control plane: "+
				"a name starts with %s", aws.ToString(parameter.Name),
				strings.Join(allowedPrefixes, " or "))
		}
		settings[name] = aws.ToString(parameter.Value)
	}
	return nil
}

func allowed(name string) bool {
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) && !strings.Contains(name, "/") {
			return true
		}
	}
	return false
}
