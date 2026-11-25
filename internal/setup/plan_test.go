package setup

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
)

func TestAWSPathEndsAtTheReviewAndOnlyReads(t *testing.T) {
	cli := accountCLI()
	cli.answers["ec2 describe-regions"] = `[]`
	server, handler := newServer(t, cli)
	steps := []struct{ path, body string }{
		{"/api/target", `{"target":"aws","env":"prod-1"}`},
		{"/api/model", `{"choice":"live","key":"sk-or-v1-0123456789abcdef0123456789"}`},
		{"/api/aws/signin", `{"profile":"team/operator","region":""}`},
		{"/api/aws/discover", `{"profile":"team/operator","region":"eu-west-1"}`},
		{"/api/aws/place", `{"mode":"existing","network":"vpc-1"}`},
		{"/api/review", ""},
	}
	for _, step := range steps {
		if code, body := post(t, server, handler, step.path, step.body); code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %v", step.path, code, body)
		}
	}
	code, body := post(t, server, handler, "/api/deploy", "")
	message, _ := body["error"].(string)
	if code != http.StatusBadRequest || !strings.Contains(message, "creates nothing on AWS") {
		t.Fatalf("deploy: status = %d, error = %q", code, message)
	}
	for _, path := range []string{"/api/connect", "/api/destroy", "/api/key"} {
		if code, _ := post(t, server, handler, path, ""); code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405: the API has no such call", path, code)
		}
	}
	for _, call := range cli.calls {
		verb, _, _ := strings.Cut(call[1], "-")
		switch verb {
		case "describe", "list", "get", "simulate":
		default:
			t.Errorf("the wizard made a call that is not a read: aws %s %s", call[0], call[1])
		}
	}
	if server.state.Deployed {
		t.Fatal("the state says that the control plane exists on AWS")
	}
}

func TestReviewOnAWSNeedsTheSignInAndTheNetwork(t *testing.T) {
	server, handler := newServer(t, accountCLI())
	post(t, server, handler, "/api/target", `{"target":"aws","env":"prod-1"}`)
	code, body := post(t, server, handler, "/api/review", "")
	if code != http.StatusBadRequest || !strings.Contains(body["error"].(string), "sign in") {
		t.Fatalf("before the sign-in: status = %d, body = %v", code, body)
	}
	post(t, server, handler, "/api/aws/signin", `{"profile":"p","region":""}`)
	code, body = post(t, server, handler, "/api/review", "")
	if code != http.StatusBadRequest || !strings.Contains(body["error"].(string), "network") {
		t.Fatalf("before the network: status = %d, body = %v", code, body)
	}
	_, state := request(t, handler, server.Token(), http.MethodGet, "/api/state", "")
	if state["aws_plan"] != nil {
		t.Fatalf("a plan before the network is chosen: %v", state["aws_plan"])
	}
}

func details(plan *AWSPlan) string {
	raw, _ := json.Marshal(plan)
	return string(raw)
}

func TestPlanForANetworkWithARouter(t *testing.T) {
	plan := awsPlan(State{Target: TargetAWS, Env: "prod-1", PlaceMode: PlaceExisting,
		Network: "vpc-1", Access: AccessTailscale})
	text := details(plan)
	for _, want := range []string{"t4g.small", "auto scaling group of one", "db.t4g.micro",
		"machine image and its snapshot", "Fargate", "/src/prod-1", "admits only the router",
		"internal load balancer", "Keycloak", "vpc-1", "US West (Oregon)", "2026-11-18"} {
		if !strings.Contains(text, want) {
			t.Errorf("the plan lacks %q: %s", want, text)
		}
	}
	if plan.TagKey != "clanker_env" || plan.TagValue != "prod-1" {
		t.Errorf("tag = %s=%s", plan.TagKey, plan.TagValue)
	}
	if math.Abs(plan.Weekly-16.16) > 0.005 {
		t.Errorf("one week = %.4f USD, want about 16.16", plan.Weekly)
	}
	if strings.Contains(text, "NAT gateway") {
		t.Errorf("the plan for a network that exists names a NAT gateway: %s", text)
	}
}

func TestPlanForANewNetworkWithoutARouter(t *testing.T) {
	plan := awsPlan(State{Target: TargetAWS, Env: "prod-1", PlaceMode: PlaceNew,
		NetworkCIDR: "10.64.0.0/22", Access: AccessNone})
	text := details(plan)
	for _, want := range []string{"10.64.0.0/22", "one NAT gateway", "found no Tailscale",
		"which you add", "the NAT gateway of the new network"} {
		if !strings.Contains(text, want) {
			t.Errorf("the plan lacks %q: %s", want, text)
		}
	}
	if awsPlan(State{Target: TargetLocal, PlaceMode: PlaceNew}) != nil {
		t.Error("a plan for the local target")
	}
}
