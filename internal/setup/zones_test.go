package setup

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func zoneCLI() *fakeCLI {
	cli := &fakeCLI{answers: map[string]string{
		"route53 list-hosted-zones": `[{"id":"/hostedzone/ZLIVE","name":"example.org."},
			{"id":"/hostedzone/ZORPHAN","name":"example.org."}]`,
	}}
	servers := map[string]string{"ZLIVE": `["ns-1.example.net.","NS-2.example.net"]`,
		"ZORPHAN": `["ns-9.example.net"]`}
	cli.onCall = func(args []string) {
		cli.answers["route53 get-hosted-zone"] = servers[argumentAfter(args, "--id")]
	}
	return cli
}

func TestZonesOffersOnlyTheZoneThatTheParentDelegates(t *testing.T) {
	lookup := LookupNS
	t.Cleanup(func() { LookupNS = lookup })
	LookupNS = func(_ context.Context, name string) ([]*net.NS, error) {
		if name != "example.org" {
			return nil, errors.New("no such host")
		}
		return []*net.NS{{Host: "ns-2.example.net."}, {Host: "ns-1.example.net."}}, nil
	}
	zones, err := AWS{Profile: "p", Region: "us-east-1", Run: zoneCLI().run}.Zones(
		context.Background())
	if err != nil {
		t.Fatal(err)
	}
	delegated := map[string]bool{}
	for _, zone := range zones {
		delegated[zone.ID] = zone.Delegated
		if !zone.Delegated && !strings.Contains(zone.Reason, "other name servers") {
			t.Errorf("zone %s: reason %q", zone.ID, zone.Reason)
		}
	}
	if !delegated["ZLIVE"] || delegated["ZORPHAN"] || len(zones) != 2 {
		t.Errorf("zones = %+v", zones)
	}
}

func TestCheckLaunchGivesAReasonWhenTheMessageCannotBeDecoded(t *testing.T) {
	cli := &fakeCLI{answers: map[string]string{"ssm get-parameter": `"ami-0123"`}}
	cli.errors = map[string]error{
		"ec2 run-instances": &CLIError{Command: "aws ec2 run-instances", Err: errors.New("exit 254"),
			Stderr: "(UnauthorizedOperation) Encoded authorization failure message: " +
				strings.Repeat("x", 1000)},
		"sts decode-authorization-message": errors.New("AccessDenied"),
	}
	check, err := AWS{Profile: "p", Region: "us-east-1", Run: cli.run}.CheckLaunch(
		context.Background(), "subnet-1", nil)
	if err != nil || check.Allowed || !strings.Contains(check.Reason, "could not be decoded") {
		t.Errorf("check = %+v, %v", check, err)
	}
}
