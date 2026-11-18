package setup

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func private(id, zone string) Subnet {
	return Subnet{ID: id, CIDR: "10.0.0.0/24", Zone: zone, Outbound: outboundNAT}
}

// fitNetwork is a network with private subnets in two zones.
func fitNetwork(id, cidr string, instances int, router *Router) Network {
	return Network{ID: id, Name: id, CIDR: cidr, Instances: instances, Router: router,
		Subnets: []Subnet{private(id+"-a", "zone-a"), private(id+"-a2", "zone-a"),
			private(id+"-b", "zone-b"),
			{ID: id + "-public", Zone: "zone-a", Outbound: outboundInternet}}}
}

func TestPlacePrefersANetworkThatATailscaleRouterServes(t *testing.T) {
	served := &Router{Service: "router", Routes: []string{"10.2.0.0/16"}, Covers: true}
	placement := Place([]Network{
		fitNetwork("quiet", "10.1.0.0/16", 0, nil),
		fitNetwork("served", "10.2.0.0/16", 40, served),
	})
	if placement.Mode != PlaceExisting || placement.NetworkID != "served" ||
		placement.Access != AccessTailscale {
		t.Fatalf("placement = %+v", placement)
	}
	if !slices.Equal(placement.Subnets, []string{"served-a", "served-b"}) {
		t.Fatalf("subnets = %v, want one private subnet in each of two zones", placement.Subnets)
	}
}

func TestPlaceTakesTheNetworkWithTheFewestInstances(t *testing.T) {
	partly := &Router{Service: "router", Routes: []string{"10.9.0.0/24"}, Covers: false}
	placement := Place([]Network{
		fitNetwork("busy", "10.1.0.0/16", 30, partly),
		fitNetwork("quiet", "10.2.0.0/16", 2, nil),
	})
	if placement.NetworkID != "quiet" || placement.Access != AccessNone {
		t.Fatalf("placement = %+v", placement)
	}
	if !strings.Contains(strings.Join(placement.Reasons, " "), "private path is necessary") {
		t.Fatalf("reasons = %v, want the missing private path", placement.Reasons)
	}
}

func TestPlaceProposesANewNetworkWhenNoneFits(t *testing.T) {
	oneZone := Network{ID: "one-zone", CIDR: "10.64.0.0/16",
		Subnets: []Subnet{private("s1", "zone-a")}}
	fallback := Network{ID: "default", CIDR: "172.31.0.0/16", Default: true,
		Subnets: []Subnet{private("s2", "zone-a"), private("s3", "zone-b")}}
	placement := Place([]Network{oneZone, fallback})
	if placement.Mode != PlaceNew || placement.NetworkID != "" {
		t.Fatalf("placement = %+v", placement)
	}
	if placement.CIDR != "10.65.0.0/22" {
		t.Fatalf("CIDR = %q, want the first range that overlaps no network", placement.CIDR)
	}
	if len(placement.Rejected) != 2 ||
		!strings.Contains(placement.Rejected[0].Reason, "1 availability") ||
		!strings.Contains(placement.Rejected[1].Reason, "default network") {
		t.Fatalf("rejected = %+v", placement.Rejected)
	}
}

func TestPlaceInAnEmptyRegionProposesANewNetwork(t *testing.T) {
	placement := Place(nil)
	if placement.Mode != PlaceNew || placement.CIDR != "10.64.0.0/22" {
		t.Fatalf("placement = %+v", placement)
	}
}

func TestPlaceSaysSoWhenNoAddressRangeIsFree(t *testing.T) {
	placement := Place([]Network{{ID: "all", CIDR: "10.0.0.0/8"}})
	if placement.Mode != PlaceNew || placement.CIDR != "" ||
		!strings.Contains(strings.Join(placement.Reasons, " "), "choose one by hand") {
		t.Fatalf("placement = %+v", placement)
	}
}

func TestASubnetWithoutARouteTableUsesTheMainTable(t *testing.T) {
	subnets := []awsSubnet{{ID: "own", VPC: "v"}, {ID: "inherits", VPC: "v"},
		{ID: "other-network", VPC: "w"}}
	tables := []awsRouteTable{
		{VPC: "v", Main: true, Gateway: "igw-1"},
		{VPC: "v", Subnets: []string{"own"}, NAT: "nat-1"},
		{VPC: "w", Main: true, Gateway: "local"},
	}
	got := outboundOf(subnets, tables)
	want := map[string]string{"own": outboundNAT, "inherits": outboundInternet,
		"other-network": outboundNone}
	for id, outbound := range want {
		if got[id] != outbound {
			t.Errorf("subnet %s: outbound = %q, want %q", id, got[id], outbound)
		}
	}
}

func TestARouterCoversANetworkOnlyWithItsWholeRange(t *testing.T) {
	cases := []struct {
		routes []string
		want   bool
	}{
		{[]string{"10.2.0.0/16"}, true},
		{[]string{"2600:1f14::/56", "10.0.0.0/8"}, true},
		{[]string{"10.2.0.0/17"}, false},
		{[]string{"10.3.0.0/16"}, false},
		{[]string{"not a range"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := covers(c.routes, "10.2.0.0/16"); got != c.want {
			t.Errorf("routes %v: covers = %v, want %v", c.routes, got, c.want)
		}
	}
}

// accountCLI answers as an account with one network that a Tailscale router serves.
func accountCLI() *fakeCLI {
	return &fakeCLI{answers: map[string]string{
		"sts get-caller-identity": `{"Account":"111122223333",
			"Arn":"arn:aws:sts::111122223333:assumed-role/Operator/someone"}`,
		"ec2 describe-vpcs": `[{"id":"vpc-1","cidr":"10.2.0.0/16","default":false,
			"tags":[{"Key":"Name","Value":"apps"},{"Key":"owner","Value":"platform"}]}]`,
		"ec2 describe-subnets": `[
			{"id":"subnet-a","vpc":"vpc-1","cidr":"10.2.0.0/24","zone":"zone-a","tags":null},
			{"id":"subnet-b","vpc":"vpc-1","cidr":"10.2.1.0/24","zone":"zone-b",
			 "tags":[{"Key":"Name","Value":"private-b"}]},
			{"id":"subnet-p","vpc":"vpc-1","cidr":"10.2.2.0/24","zone":"zone-a","tags":null}]`,
		"ec2 describe-route-tables": `[
			{"vpc":"vpc-1","main":true,"subnets":[null],"nat":null,"gateway":"igw-1"},
			{"vpc":"vpc-1","main":false,"subnets":["subnet-a","subnet-b"],"nat":"nat-1","gateway":null}]`,
		"ec2 describe-instances": `["vpc-1","vpc-1","vpc-other"]`,
		"ecs list-clusters":      `["arn:aws:ecs:r:111122223333:cluster/routers"]`,
		"ecs list-services":      `["arn:router","arn:web"]`,
		"ecs describe-services": `[
			{"name":"router","task":"task-router","running":1,"subnets":["subnet-a"]},
			{"name":"stopped","task":"task-x","running":0,"subnets":["subnet-a"]}]`,
		"ecs describe-task-definition": `[{"image":"tailscale/tailscale:stable",
			"routes":"10.2.0.0/16, fd00::/56"}]`,
		"iam get-role": `"arn:aws:iam::111122223333:role/Operator"`,
		"iam simulate-principal-policy": `[{"action":"iam:CreateRole","decision":"allowed"},
			{"action":"iam:PassRole","decision":"implicitDeny"}]`,
	}}
}

func argumentAfter(call []string, flag string) string {
	if at := slices.Index(call, flag); at >= 0 && at+1 < len(call) {
		return call[at+1]
	}
	return ""
}

// surveyed returns the one network of accountCLI and the calls that found it.
func surveyed(t *testing.T) (Network, [][]string) {
	t.Helper()
	cli := accountCLI()
	account := AWS{Profile: "team/operator", Run: cli.run}
	networks, err := account.Networks(context.Background(), "eu-west-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(networks) != 1 {
		t.Fatalf("networks = %+v", networks)
	}
	return networks[0], cli.calls
}

func TestNetworksJoinsTagsSubnetsRoutesAndInstances(t *testing.T) {
	apps, _ := surveyed(t)
	if apps.Name != "apps" || apps.Tags["owner"] != "platform" || apps.Instances != 2 {
		t.Fatalf("network = %+v", apps)
	}
	outbound := map[string]string{}
	for _, subnet := range apps.Subnets {
		outbound[subnet.ID] = subnet.Outbound
	}
	want := map[string]string{"subnet-a": outboundNAT, "subnet-b": outboundNAT,
		"subnet-p": outboundInternet}
	if !maps.Equal(outbound, want) {
		t.Fatalf("outbound = %v, want %v", outbound, want)
	}
}

func TestNetworksFindsTheTailscaleRouterOfARunningService(t *testing.T) {
	apps, calls := surveyed(t)
	want := Router{Service: "router", Routes: []string{"10.2.0.0/16", "fd00::/56"}, Covers: true}
	if apps.Router == nil || !reflect.DeepEqual(*apps.Router, want) {
		t.Fatalf("router = %+v, want %+v", apps.Router, want)
	}
	tasks := 0
	for _, call := range calls {
		if call[1] == "describe-task-definition" {
			tasks++
		}
	}
	if tasks != 1 {
		t.Fatalf("%d task definitions were read: the task of the stopped service must not be", tasks)
	}
}

func TestNetworksCallsTheRegionWithTheProfile(t *testing.T) {
	_, calls := surveyed(t)
	for _, call := range calls {
		if argumentAfter(call, "--region") != "eu-west-1" ||
			argumentAfter(call, "--profile") != "team/operator" {
			t.Fatalf("call %v does not name the region and the profile", call)
		}
	}
}

func TestRightsSimulatesThePoliciesOfTheRoleOfTheSession(t *testing.T) {
	cli := accountCLI()
	account := AWS{Profile: "p", Region: "us-east-1", Run: cli.run}
	rights, err := account.Rights(context.Background(),
		Identity{Arn: "arn:aws:sts::111122223333:assumed-role/Operator/someone"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Right{{Action: "iam:CreateRole", Allowed: true}, {Action: "iam:PassRole"}}
	if !slices.Equal(rights, want) {
		t.Fatalf("rights = %+v", rights)
	}
	var role, source string
	for _, call := range cli.calls {
		role += argumentAfter(call, "--role-name")
		source += argumentAfter(call, "--policy-source-arn")
	}
	if role != "Operator" || source != "arn:aws:iam::111122223333:role/Operator" {
		t.Fatalf("role = %q, simulated ARN = %q", role, source)
	}
}

func TestRightsOfAUserNeedNoRoleLookup(t *testing.T) {
	cli := accountCLI()
	_, err := AWS{Profile: "p", Region: "us-east-1", Run: cli.run}.Rights(context.Background(),
		Identity{Arn: "arn:aws:iam::111122223333:user/someone"})
	if err != nil {
		t.Fatal(err)
	}
	if cli.called("iam get-role") {
		t.Fatal("the role of a user was looked up")
	}
}

// countsForLengthQueries makes the fake answer a query that counts with a number. The
// same two calls count what a region holds and list its networks and instances.
func countsForLengthQueries(cli *fakeCLI) func(args []string) {
	networks := cli.answers["ec2 describe-vpcs"]
	return func(args []string) {
		if strings.HasPrefix(argumentAfter(args, "--query"), "length(") {
			cli.answers["ec2 describe-instances"] = `5`
			cli.answers["ec2 describe-vpcs"] = `1`
			return
		}
		cli.answers["ec2 describe-instances"] = `["vpc-1"]`
		cli.answers["ec2 describe-vpcs"] = networks
	}
}

func TestDiscoverKeepsTheNetworksWhenTheRightsAreNotKnown(t *testing.T) {
	cli := accountCLI()
	cli.answers["ec2 describe-regions"] = `["eu-west-1"]`
	cli.errors = map[string]error{"iam simulate-principal-policy": errors.New("AccessDenied")}
	cli.onCall = countsForLengthQueries(cli)
	found, err := AWS{Profile: "p", Run: cli.run}.Discover(context.Background(), "us-east-1", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []RegionUse{{Name: "eu-west-1", Instances: 5, Networks: 1}}
	if found.Region != "eu-west-1" || !slices.Equal(found.Regions, want) {
		t.Fatalf("regions = %+v, region = %q", found.Regions, found.Region)
	}
	if found.Placement.NetworkID != "vpc-1" || found.Placement.Access != AccessTailscale {
		t.Fatalf("placement = %+v", found.Placement)
	}
	if found.Rights != nil || !strings.Contains(found.RightsError, "AccessDenied") {
		t.Fatalf("rights = %v, error = %q", found.Rights, found.RightsError)
	}
}

func TestDiscoverStopsWhenTheSessionExpired(t *testing.T) {
	cli := &fakeCLI{errors: map[string]error{
		"sts get-caller-identity": errors.New("the SSO session has expired")}}
	_, err := AWS{Profile: "p", Run: cli.run}.Discover(context.Background(), "us-east-1", "")
	if !errors.Is(err, ErrNeedsLogin) {
		t.Fatalf("error = %v, want ErrNeedsLogin", err)
	}
}

func TestDiscoverRefusesNamesThatAreNotNames(t *testing.T) {
	cli := &fakeCLI{}
	cases := []struct{ profile, region string }{
		{"--endpoint-url=http://evil", ""}, {"", ""}, {"p", "us-east-1; rm"}, {"-p", "us-east-1"}}
	for _, c := range cases {
		account := AWS{Profile: c.profile, Run: cli.run}
		_, err := account.Discover(context.Background(), "us-east-1", c.region)
		if err == nil {
			t.Errorf("profile %q, region %q: no error", c.profile, c.region)
		}
	}
	if len(cli.calls) != 0 {
		t.Fatalf("the CLI was called: %v", cli.calls)
	}
}

func TestAProfileNameCanHaveASlash(t *testing.T) {
	if !profilePattern.MatchString("Team-Name/PowerUser") {
		t.Fatal("a profile name with a slash was refused")
	}
}

func TestTheWizardServesTheDiscovery(t *testing.T) {
	cli := accountCLI()
	cli.answers["ec2 describe-regions"] = `[]`
	server, handler := newServer(t, cli)
	code, body := post(t, server, handler, "/api/aws/discover",
		`{"profile":"team/operator","region":"eu-west-1"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, body)
	}
	placement, _ := body["placement"].(map[string]any)
	if body["region"] != "eu-west-1" || placement["network_id"] != "vpc-1" {
		t.Fatalf("body = %v", body)
	}
	code, _ = post(t, server, handler, "/api/aws/discover",
		`{"profile":"p","region":"nowhere"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("status for a region that is not a region = %d", code)
	}
}

// post sends a request of the browser to the wizard.
func post(t *testing.T, server *Server, handler http.Handler, path, body string) (
	int, map[string]any) {
	t.Helper()
	return request(t, handler, server.Token(), http.MethodPost, path, body)
}

// surveyedServer is a wizard that surveyed the account of accountCLI.
func surveyedServer(t *testing.T, cfg func(*fakeCLI)) (*Server, http.Handler) {
	t.Helper()
	cli := accountCLI()
	cli.answers["ec2 describe-regions"] = `[]`
	if cfg != nil {
		cfg(cli)
	}
	server, handler := newServer(t, cli)
	code, body := post(t, server, handler, "/api/aws/discover",
		`{"profile":"team/operator","region":"eu-west-1"}`)
	if code != http.StatusOK {
		t.Fatalf("survey: status = %d, body = %v", code, body)
	}
	return server, handler
}

func TestPlaceRecordsANetworkOfTheSurvey(t *testing.T) {
	server, handler := surveyedServer(t, nil)
	code, body := post(t, server, handler, "/api/aws/place",
		`{"mode":"existing","network":"vpc-1"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, body)
	}
	want := State{Region: "eu-west-1", PlaceMode: PlaceExisting, Network: "vpc-1",
		Subnets: []string{"subnet-a", "subnet-b"}, Access: AccessTailscale}
	if !reflect.DeepEqual(server.state, want) {
		t.Fatalf("state = %+v, want %+v", server.state, want)
	}
	saved, err := os.ReadFile(server.statePath())
	if err != nil || !strings.Contains(string(saved), `"network": "vpc-1"`) {
		t.Fatalf("saved state = %s, error = %v", saved, err)
	}
}

func TestPlaceRecordsANewNetworkWithTheFreeRange(t *testing.T) {
	server, handler := surveyedServer(t, nil)
	code, body := post(t, server, handler, "/api/aws/place",
		`{"mode":"new","network":"vpc-1"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, body)
	}
	want := State{Region: "eu-west-1", PlaceMode: PlaceNew, NetworkCIDR: "10.64.0.0/22",
		Access: AccessNone}
	if !reflect.DeepEqual(server.state, want) {
		t.Fatalf("state = %+v, want %+v", server.state, want)
	}
}

func TestPlaceRefusesWhatTheSurveyDoesNotAllow(t *testing.T) {
	onlyPublic := func(cli *fakeCLI) {
		cli.answers["ec2 describe-route-tables"] = `[
			{"vpc":"vpc-1","main":true,"subnets":[null],"nat":null,"gateway":"igw-1"}]`
		cli.answers["ec2 describe-vpcs"] = `[
			{"id":"vpc-1","cidr":"10.0.0.0/8","default":false,"tags":null}]`
	}
	server, handler := surveyedServer(t, onlyPublic)
	cases := []struct{ body, want string }{
		{`{"mode":"existing","network":"vpc-1"}`, "0 availability zones"},
		{`{"mode":"existing","network":"vpc-9"}`, "not in the survey"},
		{`{"mode":"new"}`, "no address range is free"},
		{`{"mode":"somewhere"}`, "does not exist"},
	}
	for _, c := range cases {
		code, body := post(t, server, handler, "/api/aws/place", c.body)
		message, _ := body["error"].(string)
		if code != http.StatusBadRequest || !strings.Contains(message, c.want) {
			t.Errorf("%s: status = %d, error = %q, want %q", c.body, code, message, c.want)
		}
	}
	if server.state.PlaceMode != "" {
		t.Fatalf("a refused choice was recorded: %+v", server.state)
	}
}

func TestPlaceNeedsASurvey(t *testing.T) {
	server, handler := newServer(t, accountCLI())
	code, body := post(t, server, handler, "/api/aws/place",
		`{"mode":"existing","network":"vpc-1"}`)
	message, _ := body["error"].(string)
	if code != http.StatusBadRequest || !strings.Contains(message, "survey the account") {
		t.Fatalf("status = %d, body = %v", code, body)
	}
}

func TestSignInWithoutARegionForgetsTheSurveyAndThePlace(t *testing.T) {
	server, handler := surveyedServer(t, nil)
	post(t, server, handler, "/api/target", `{"target":"aws","env":"prod"}`)
	post(t, server, handler, "/api/aws/place", `{"mode":"new"}`)
	code, body := post(t, server, handler, "/api/aws/signin",
		`{"profile":"team/operator","region":""}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, body)
	}
	if server.state.Account != "111122223333" || server.state.PlaceMode != "" || server.found != nil {
		t.Fatalf("state = %+v, survey kept = %v", server.state, server.found != nil)
	}
	code, _ = post(t, server, handler, "/api/aws/place", `{"mode":"new"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("a place without a new survey: status = %d", code)
	}
}

func TestSurveyOnlyRefusesTheReviewAndTheCreationOnAWS(t *testing.T) {
	cli := accountCLI()
	server, err := New(context.Background(), Config{DataDir: t.TempDir(), Version: "v9.9.9",
		CLI: cli.run, EnableAWS: true, SurveyOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := server.Handler("127.0.0.1:7069")
	if err != nil {
		t.Fatal(err)
	}
	post(t, server, handler, "/api/target", `{"target":"aws","env":"prod"}`)
	post(t, server, handler, "/api/key", `{"key":""}`)
	post(t, server, handler, "/api/aws/signin", `{"profile":"p","region":""}`)
	server.review = []Change{{ID: "x", Type: "y"}}
	for _, path := range []string{"/api/review", "/api/deploy"} {
		code, body := post(t, server, handler, path, "")
		message, _ := body["error"].(string)
		if code != http.StatusBadRequest ||
			!strings.Contains(message, "does not create the control plane") {
			t.Errorf("%s: status = %d, error = %q", path, code, message)
		}
	}
	if cli.called("cloudformation create-change-set") ||
		cli.called("cloudformation execute-change-set") {
		t.Fatal("CloudFormation was called")
	}
}
