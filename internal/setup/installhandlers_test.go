package setup

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// placedServer is a wizard after the survey and the choice of the network of accountCLI,
// with an empty directory for the environments.
func placedServer(t *testing.T) (*Server, http.Handler, *fakeCLI) {
	t.Helper()
	cli := accountCLI()
	cli.answers["ec2 describe-regions"] = `[]`
	server, handler := newServer(t, cli)
	server.installer.Root = t.TempDir()
	for _, step := range []struct{ path, body string }{
		{"/api/target", `{"target":"aws","env":"prod-1"}`},
		{"/api/model", `{"choice":"live","key":"sk-or-v1-0123456789abcdef0123456789"}`},
		{"/api/aws/signin", `{"profile":"team/operator","region":""}`},
		{"/api/aws/discover", `{"profile":"team/operator","region":"eu-west-1"}`},
		{"/api/aws/place", `{"mode":"existing","network":"vpc-1"}`},
	} {
		if code, body := post(t, server, handler, step.path, step.body); code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %v", step.path, code, body)
		}
	}
	cli.onCall = func(args []string) {
		answer := `{"name":"example.org.","private":false}`
		if strings.Contains(strings.Join(args, " "), "DelegationSet") {
			answer = `["ns-1.example.net"]`
		}
		cli.answers["route53 get-hosted-zone"] = answer
	}
	lookup := LookupNS
	LookupNS = func(context.Context, string) ([]*net.NS, error) {
		return []*net.NS{{Host: "ns-1.example.net."}}, nil
	}
	t.Cleanup(func() { LookupNS = lookup })
	return server, handler, cli
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDetailsKeepTheValuesOfTheTagsOutOfTheState(t *testing.T) {
	server, handler, _ := placedServer(t)
	body := `{"zone_id":"ZLIVE","tags":{"example:owner":"owner@example.com"},"checkout":"` +
		repoRoot(t) + `","nanos":""}`
	if code, answer := post(t, server, handler, "/api/aws/details", body); code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, answer)
	}
	if server.state.ZoneName != "example.org" || server.state.Nanos != DefaultNanos ||
		strings.Join(server.state.TagKeys, ",") != "example:owner" ||
		server.tags["example:owner"] != "owner@example.com" {
		t.Errorf("state = %+v, tags = %v", server.state, server.tags)
	}
	saved, err := os.ReadFile(server.statePath())
	if err != nil || strings.Contains(string(saved), "owner@example.com") {
		t.Errorf("setup.json holds the value of a tag: %v", err)
	}
	code, state := request(t, handler, server.Token(), http.MethodGet, "/api/state", "")
	encoded, err := json.Marshal(state)
	if code != http.StatusOK || err != nil || strings.Contains(string(encoded), "owner@example.com") {
		t.Errorf("/api/state holds the value of a tag")
	}
}

func TestDetailsRefuseATagWithoutAValueAWrongCheckoutAndAWrongZone(t *testing.T) {
	server, handler, _ := placedServer(t)
	checkout := `"checkout":"` + repoRoot(t) + `"`
	cases := map[string]string{
		`{"zone_id":"ZLIVE","tags":{"example:owner":""},` + checkout + `}`: "give a value",
		`{"zone_id":"ZLIVE","tags":{},"checkout":"/nonexistent"}`:          "not a checkout",
		`{"zone_id":"not-a-zone","tags":{},` + checkout + `}`:              "not valid",
	}
	for body, want := range cases {
		code, answer := post(t, server, handler, "/api/aws/details", body)
		message, _ := answer["error"].(string)
		if code != http.StatusBadRequest || !strings.Contains(message, want) {
			t.Errorf("%s: status = %d, error = %q, want %q", body, code, message, want)
		}
	}
}

func TestCreateOnAWSNeedsTheRouterOfTheNetwork(t *testing.T) {
	server, handler, _ := placedServer(t)
	body := `{"zone_id":"ZLIVE","tags":{},"checkout":"` + repoRoot(t) + `"}`
	if code, answer := post(t, server, handler, "/api/aws/details", body); code != http.StatusOK {
		t.Fatalf("details: status = %d, body = %v", code, answer)
	}
	server.state.RouterGroup = ""
	post(t, server, handler, "/api/review", "")
	code, answer := post(t, server, handler, "/api/deploy", "")
	message, _ := answer["error"].(string)
	if code != http.StatusBadRequest || !strings.Contains(message, "Tailscale subnet router") {
		t.Fatalf("status = %d, error = %q", code, message)
	}
}

func TestRemoveNeedsTheNameOfTheEnvironment(t *testing.T) {
	server, handler, _ := placedServer(t)
	code, answer := post(t, server, handler, "/api/aws/remove", `{"env":"prod-2"}`)
	message, _ := answer["error"].(string)
	if code != http.StatusBadRequest || !strings.Contains(message, "type the name") {
		t.Fatalf("status = %d, error = %q", code, message)
	}
	if server.job != nil {
		t.Fatal("a removal started")
	}
}
