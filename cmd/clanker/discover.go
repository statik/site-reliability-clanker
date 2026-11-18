package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/statik/site-reliability-clanker/internal/setup"
)

// runDiscover surveys an AWS account with the session of the person and prints what the
// wizard would propose, as JSON. It changes nothing in the account.
func runDiscover(ctx context.Context, o options) error {
	if o.profile == "" {
		return errors.New("name the AWS CLI profile of the account: clanker discover " +
			"--profile NAME. `aws configure list-profiles` lists the profiles")
	}
	found, err := setup.DiscoverAccount(ctx, o.profile, o.region)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(found)
}
