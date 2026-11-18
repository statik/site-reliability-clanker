package main

import (
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/awstarget"
)

const exampleIdentities = `{"planner": "arn:aws:iam::111122223333:role/clanker/example-planner"}`

// noSTS fails the test when the start asks for a client that it must not need.
func noSTS(t *testing.T) func(string) awstarget.STS {
	return func(string) awstarget.STS {
		t.Fatal("the start made an STS client")
		return nil
	}
}

func TestWithoutTheRolesTheCoreOnAWSStartsWithNoIssuer(t *testing.T) {
	t.Setenv(identitiesEnv, "")
	t.Setenv(regionEnv, "")
	issuer, err := awsIssuer(nil, noSTS(t))
	if err != nil || issuer != nil {
		t.Fatalf("issuer = %v, error = %v, want neither", issuer, err)
	}
}

func TestRolesThatAreNotValidOrHaveNoRegionStopTheStart(t *testing.T) {
	cases := []struct{ identities, region, want string }{
		{"not JSON", "eu-west-1", identitiesEnv + ": the roles of the identities are not"},
		{`{"planner": "example"}`, "eu-west-1", "not the ARN of an IAM role"},
		{exampleIdentities, "", "set " + regionEnv},
	}
	for _, c := range cases {
		t.Setenv(identitiesEnv, c.identities)
		t.Setenv(regionEnv, c.region)
		issuer, err := awsIssuer(nil, noSTS(t))
		if err == nil || !strings.Contains(err.Error(), c.want) || issuer != nil {
			t.Errorf("%q in %q: issuer = %v, error = %v, want %q", c.identities, c.region,
				issuer, err, c.want)
		}
	}
}

func TestWithTheRolesTheIssuerCallsSTSInTheRegion(t *testing.T) {
	t.Setenv(identitiesEnv, exampleIdentities)
	t.Setenv(regionEnv, "eu-west-1")
	region := ""
	issuer, err := awsIssuer(nil, func(r string) awstarget.STS {
		region = r
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	credentials, isAWS := issuer.(awstarget.Credentials)
	if !isAWS || region != "eu-west-1" || len(credentials.Roles) != 1 {
		t.Fatalf("issuer = %#v, region = %q", issuer, region)
	}
}
