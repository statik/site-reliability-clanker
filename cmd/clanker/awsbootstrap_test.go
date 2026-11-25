package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/awstarget"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/sim"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// exampleBootstrap is what `tofu output -json` prints in layer 00-bootstrap of an example
// account.
const exampleBootstrap = `{
  "clanker_ttl": {"sensitive": false, "type": "string", "value": "168"},
  "evidence_bucket": {"sensitive": false, "type": "string",
    "value": "clanker-example-evidence-111122223333"},
  "identities": {"sensitive": false, "type": ["map", "string"],
    "value": {"planner": "arn:aws:iam::111122223333:role/clanker/example-planner"}},
  "permission_guard": {"sensitive": false, "type": "string",
    "value": "arn:aws:iam::111122223333:policy/example-boundary"},
  "state_backend": {"sensitive": false, "type": ["map", "string"],
    "value": {"bucket": "clanker-example-state-111122223333", "encrypt": "true",
      "region": "eu-west-1", "use_lockfile": "true"}},
  "workload_role_path": {"sensitive": false, "type": "string", "value": "/clanker-workload/"}
}`

// noSTS fails the test when the start asks for a client that it must not need.
func noSTS(t *testing.T) func(string) awstarget.STS {
	return func(string) awstarget.STS {
		t.Fatal("the start made an STS client")
		return nil
	}
}

var sandbox = orchestrator.EnvConfig{Name: "aws-sandbox", Cloud: "aws"}

func TestWithoutTheBootstrapTheCoreOnAWSStartsWithNoIssuerAndNoBackend(t *testing.T) {
	t.Setenv(bootstrapEnv, "")
	t.Setenv(regionEnv, "")
	// The start reads no database here: the components have none.
	env, issuer, err := awsEnvironment(&components{}, sandbox, noSTS(t))
	if err != nil || issuer != nil || !reflect.DeepEqual(env, sandbox) {
		t.Fatalf("env = %+v, issuer = %v, error = %v; want the same env and neither", env,
			issuer, err)
	}
}

func TestABootstrapThatIsNotValidOrHasNoRegionStopsTheStart(t *testing.T) {
	cases := []struct{ bootstrap, region, want string }{
		{"not JSON", "eu-west-1",
			bootstrapEnv + ": the outputs of layer 00-bootstrap are not what"},
		{`{"identities": {"sensitive": false, "type": "string", "value": "x"}}`, "eu-west-1",
			bootstrapEnv + ": the output clanker_ttl of layer 00-bootstrap is missing"},
		{exampleBootstrap, "", "set " + regionEnv},
	}
	for _, c := range cases {
		t.Setenv(bootstrapEnv, c.bootstrap)
		t.Setenv(regionEnv, c.region)
		// The components have no database, so the start must refuse before it records.
		_, issuer, err := awsEnvironment(&components{}, sandbox, noSTS(t))
		if err == nil || !strings.Contains(err.Error(), c.want) || issuer != nil {
			t.Errorf("%q in %q: issuer = %v, error = %v, want %q", c.bootstrap, c.region,
				issuer, err, c.want)
		}
	}
}

func TestTheBootstrapGivesTheEnvironmentItsBackendAndTheBrokerItsRoles(t *testing.T) {
	t.Setenv(bootstrapEnv, exampleBootstrap)
	t.Setenv(regionEnv, "eu-west-1")
	db := storetest.Open(t)
	clock, err := sim.NewClock(db)
	if err != nil {
		t.Fatal(err)
	}
	c := &components{db: db, clock: clock, log: audit.New(db, clock)}
	region := ""
	env, issuer, err := awsEnvironment(c, sandbox, func(r string) awstarget.STS {
		region = r
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantBackend := map[string]string{"bucket": "clanker-example-state-111122223333",
		"encrypt": "true", "region": "eu-west-1", "use_lockfile": "true"}
	if !reflect.DeepEqual(env.Backend, wantBackend) || env.Region != "eu-west-1" ||
		env.Name != sandbox.Name || env.Cloud != sandbox.Cloud {
		t.Errorf("env = %+v", env)
	}
	credentials, isAWS := issuer.(awstarget.Credentials)
	if !isAWS || region != "eu-west-1" || len(credentials.Roles) != 1 ||
		credentials.Roles["planner"] != "arn:aws:iam::111122223333:role/clanker/example-planner" {
		t.Fatalf("issuer = %#v, region = %q", issuer, region)
	}
	outputs, err := store.LayerOutputs(db, "aws-sandbox", "00-bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := awstarget.ParseBootstrap(exampleBootstrap)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(outputs, bootstrap.Outputs) {
		t.Errorf("stored outputs = %v, want %v", outputs, bootstrap.Outputs)
	}
}
