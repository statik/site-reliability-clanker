package main

import (
	"fmt"
	"log/slog"
	"os"
	"slices"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/awstarget"
	"github.com/statik/site-reliability-clanker/internal/broker"
)

// identitiesEnv holds the output `identities` of layer 00-bootstrap on AWS as JSON: the
// role ARN of each identity. An ARN is not a secret.
const identitiesEnv = "CLANKER_AWS_IDENTITIES"

// awsIssuer returns the issuer of the credentials on AWS. Without the roles it returns no
// issuer: the control plane starts before layer 00-bootstrap exists, and then the broker
// exchanges no grant. Roles that are not valid stop the start.
func awsIssuer(log *audit.Log, client func(region string) awstarget.STS) (broker.Issuer, error) {
	text := os.Getenv(identitiesEnv)
	if text == "" {
		slog.Warn("the broker issues no AWS credential: set " + identitiesEnv +
			" to the output `identities` of layer 00-bootstrap")
		return nil, nil
	}
	roles, err := awstarget.ParseRoles(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", identitiesEnv, err)
	}
	region := os.Getenv(regionEnv)
	if region == "" {
		return nil, fmt.Errorf("%s is set, so the broker calls STS: set %s to the region of "+
			"the environment", identitiesEnv, regionEnv)
	}
	identities := make([]string, 0, len(roles))
	for identity := range roles {
		identities = append(identities, identity)
	}
	slices.Sort(identities)
	slog.Info("the broker issues STS sessions", "region", region, "identities", identities)
	return awstarget.Credentials{STS: client(region), Roles: roles,
		Record: func(e audit.Event) error {
			_, err := log.Append(e)
			return err
		}}, nil
}
