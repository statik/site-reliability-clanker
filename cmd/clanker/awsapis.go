package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/statik/site-reliability-clanker/internal/awsparams"
	"github.com/statik/site-reliability-clanker/internal/awsrunner"
	"github.com/statik/site-reliability-clanker/internal/awstarget"
	"github.com/statik/site-reliability-clanker/internal/dispatch"
)

// testEndpointEnv is for scripts/verify/aws-core-check.sh only. It sends every call of
// the core to Parameter Store, STS and ECS to a stand-in for AWS on this machine, with
// fixed keys instead of the role of the instance. The core uses it only when it starts
// with the flag --fake-aws, and only an address on the loopback interface. Parameter Store
// cannot give it.
const testEndpointEnv = "CLANKER_TEST_AWS_ENDPOINT"

// awsAPIs make the clients of the AWS services that the core calls.
type awsAPIs struct {
	parameters func(region string) awsparams.Store
	sts        func(region string) awstarget.STS
	ecs        func(region string) awsrunner.ECS
	metadata   func() awsrunner.Metadata
}

// realAWS are the clients of AWS, with the credentials of the role of the instance.
var realAWS = awsAPIs{parameters: awsparams.NewStore, sts: awstarget.NewSTS,
	ecs: awsrunner.NewECS, metadata: awsrunner.NewMetadata}

// chooseAWSAPIs returns the clients of AWS, or with --fake-aws the clients of the
// stand-in at the address of CLANKER_TEST_AWS_ENDPOINT. Each of the two without the
// other stops the start, so that a setting of a check cannot reach a real account.
func chooseAWSAPIs(fakeAWS bool, getenv func(string) string) (awsAPIs, error) {
	endpoint := getenv(testEndpointEnv)
	switch {
	case !fakeAWS && endpoint == "":
		return realAWS, nil
	case !fakeAWS:
		return awsAPIs{}, fmt.Errorf("%s is set, and it is for the checks only: unset it. A "+
			"check that uses a stand-in for AWS also starts the core with --fake-aws",
			testEndpointEnv)
	case endpoint == "":
		return awsAPIs{}, fmt.Errorf("--fake-aws is for the checks only and needs %s, the "+
			"address of the stand-in for AWS on this machine", testEndpointEnv)
	}
	if err := checkLoopback(endpoint); err != nil {
		return awsAPIs{}, fmt.Errorf("%s: %w", testEndpointEnv, err)
	}
	return fakeAWSAPIs(endpoint), nil
}

// checkLoopback refuses an address that is not http on the loopback interface.
func checkLoopback(endpoint string) error {
	address, err := url.Parse(endpoint)
	if err != nil || address.Scheme != "http" {
		return fmt.Errorf("%q is not an http URL", endpoint)
	}
	host := address.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("%q is not on the loopback interface: the stand-in for AWS runs "+
			"on this machine", endpoint)
	}
	return nil
}

// fakeAWSAPIs returns clients that send each call to the stand-in at endpoint, signed with
// fixed keys. The metadata service of an instance has no stand-in: the check sets
// CLANKER_CORE_URL, so the core does not ask it.
func fakeAWSAPIs(endpoint string) awsAPIs {
	keys := credentials.NewStaticCredentialsProvider("fake-aws", "fake-aws", "")
	return awsAPIs{
		parameters: func(region string) awsparams.Store {
			return ssm.New(ssm.Options{Region: region, BaseEndpoint: aws.String(endpoint),
				Credentials: keys})
		},
		sts: func(region string) awstarget.STS {
			return sts.New(sts.Options{Region: region, BaseEndpoint: aws.String(endpoint),
				Credentials: keys})
		},
		ecs: func(region string) awsrunner.ECS {
			return ecs.New(ecs.Options{Region: region, BaseEndpoint: aws.String(endpoint),
				Credentials: keys})
		},
		metadata: func() awsrunner.Metadata { return noMetadata{} },
	}
}

// noMetadata is the metadata service of a machine that is not an instance.
type noMetadata struct{}

func (noMetadata) GetMetadata(context.Context, *imds.GetMetadataInput,
	...func(*imds.Options)) (*imds.GetMetadataOutput, error) {
	return nil, errors.New("the stand-in for AWS has no metadata service: set " +
		dispatch.CoreURLEnv)
}
