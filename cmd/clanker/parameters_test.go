package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/statik/site-reliability-clanker/internal/awsparams"
)

// oneStore is a Parameter Store with one page of parameters. It records its region.
type oneStore struct {
	parameters []types.Parameter
	err        error
	region     string
	asked      bool
}

func (s *oneStore) GetParametersByPath(context.Context, *ssm.GetParametersByPathInput,
	...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	s.asked = true
	return &ssm.GetParametersByPathOutput{Parameters: s.parameters}, s.err
}

func (s *oneStore) open(region string) awsparams.Store {
	s.region = region
	return s
}

func TestParametersBecomeTheSettingsOfTheCoreOnAWS(t *testing.T) {
	t.Setenv(parameterPathEnv, "/src/prod")
	t.Setenv(regionEnv, "eu-west-1")
	t.Setenv(adminEmailEnv, "from-the-image@example.com")
	t.Setenv(clientSecretEnv, "")
	store := &oneStore{parameters: []types.Parameter{
		{Name: aws.String("/src/prod/" + adminEmailEnv), Value: aws.String("admin@example.com")},
		{Name: aws.String("/src/prod/" + clientSecretEnv), Value: aws.String("secret")},
	}}
	if err := loadParameters(context.Background(), options{cloud: "aws"}, store.open); err != nil {
		t.Fatal(err)
	}
	if store.region != "eu-west-1" {
		t.Fatalf("region = %q", store.region)
	}
	if got := os.Getenv(adminEmailEnv); got != "admin@example.com" {
		t.Fatalf("%s = %q: the store must replace the environment", adminEmailEnv, got)
	}
	if got := os.Getenv(clientSecretEnv); got != "secret" {
		t.Fatalf("%s = %q", clientSecretEnv, got)
	}
}

func TestParametersAreNotReadWithoutAPathOrOnAnotherCloud(t *testing.T) {
	cases := []struct{ cloud, path string }{{"aws", ""}, {"local", "/src/prod"}, {"gcp", "/src/prod"}}
	for _, c := range cases {
		t.Setenv(parameterPathEnv, c.path)
		store := &oneStore{}
		if err := loadParameters(context.Background(), options{cloud: c.cloud}, store.open); err != nil {
			t.Fatalf("cloud %s, path %q: %v", c.cloud, c.path, err)
		}
		if store.asked {
			t.Fatalf("cloud %s, path %q: the store was asked", c.cloud, c.path)
		}
	}
}

func TestParametersNeedTheRegion(t *testing.T) {
	t.Setenv(parameterPathEnv, "/src/prod")
	t.Setenv(regionEnv, "")
	err := loadParameters(context.Background(), options{cloud: "aws"}, (&oneStore{}).open)
	if err == nil || !strings.Contains(err.Error(), regionEnv) {
		t.Fatalf("error = %v", err)
	}
}

func TestAFailureOfTheStoreStopsTheStart(t *testing.T) {
	t.Setenv(parameterPathEnv, "/src/prod")
	t.Setenv(regionEnv, "eu-west-1")
	denied := errors.New("AccessDeniedException")
	err := loadParameters(context.Background(), options{cloud: "aws"}, (&oneStore{err: denied}).open)
	if !errors.Is(err, denied) {
		t.Fatalf("error = %v", err)
	}
}
