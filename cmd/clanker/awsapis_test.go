package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

func TestTheCoreUsesTheRealAWSWithoutTheFlagAndTheSetting(t *testing.T) {
	apis, err := chooseAWSAPIs(false, environment(nil))
	if err != nil || apis.sts == nil || apis.ecs == nil || apis.parameters == nil ||
		apis.metadata == nil {
		t.Fatalf("clients %+v, error %v; want the clients of AWS", apis, err)
	}
}

func TestTheTestEndpointNeedsTheFlagAndTheFlagNeedsTheEndpoint(t *testing.T) {
	cases := []struct {
		fake     bool
		endpoint string
		want     string
	}{
		{false, "http://127.0.0.1:7290", testEndpointEnv + " is set, and it is for the checks"},
		{true, "", "--fake-aws is for the checks only and needs " + testEndpointEnv},
		{true, "https://127.0.0.1:7290", "is not an http URL"},
		{true, "http://10.0.1.5:7290", "is not on the loopback interface"},
		{true, "http://ssm.eu-west-1.amazonaws.com", "is not on the loopback interface"},
		{true, "127.0.0.1:7290", "is not an http URL"},
	}
	for _, c := range cases {
		getenv := environment(map[string]string{testEndpointEnv: c.endpoint})
		if _, err := chooseAWSAPIs(c.fake, getenv); err == nil ||
			!strings.Contains(err.Error(), c.want) {
			t.Errorf("flag %v, endpoint %q: error %v, want %q", c.fake, c.endpoint, err,
				c.want)
		}
	}
}

// awsStandIn is a stand-in for AWS that records the target of each call and answers each
// with an empty page of parameters.
type awsStandIn struct {
	mu      sync.Mutex
	targets []string
}

func (s *awsStandIn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	s.mu.Lock()
	s.targets = append(s.targets, r.Header.Get("X-Amz-Target"))
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	_, _ = w.Write([]byte(`{"Parameters": []}`))
}

func TestTheFlagSendsTheCallsToTheStandIn(t *testing.T) {
	stand := &awsStandIn{}
	server := httptest.NewServer(stand)
	defer server.Close()
	getenv := environment(map[string]string{testEndpointEnv: server.URL})
	apis, err := chooseAWSAPIs(true, getenv)
	if err != nil {
		t.Fatal(err)
	}
	_, err = apis.parameters("eu-west-1").GetParametersByPath(context.Background(),
		&ssm.GetParametersByPathInput{Path: aws.String("/src/check")})
	if err != nil {
		t.Fatal(err)
	}
	if len(stand.targets) != 1 || stand.targets[0] != "AmazonSSM.GetParametersByPath" {
		t.Errorf("the stand-in got %v, want one call of GetParametersByPath", stand.targets)
	}
	if _, err := apis.metadata().GetMetadata(context.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "set CLANKER_CORE_URL") {
		t.Errorf("metadata error %v, want a request for CLANKER_CORE_URL", err)
	}
}

func TestParameterStoreCannotGiveTheTestEndpoint(t *testing.T) {
	t.Setenv(parameterPathEnv, "/src/prod")
	t.Setenv(regionEnv, "eu-west-1")
	store := &oneStore{parameters: []types.Parameter{
		{Name: aws.String("/src/prod/" + testEndpointEnv),
			Value: aws.String("http://127.0.0.1:1")},
	}}
	err := loadParameters(context.Background(), options{cloud: "aws"}, store.open)
	if err == nil || !strings.Contains(err.Error(), "is for the checks only, and Parameter "+
		"Store cannot give it") {
		t.Fatalf("error = %v, want a refusal of the parameter", err)
	}
}
