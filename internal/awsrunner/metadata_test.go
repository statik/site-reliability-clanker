package awsrunner_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"

	"github.com/statik/site-reliability-clanker/internal/awsrunner"
)

// fakeMetadata answers every request with its text or its error, and records the path.
type fakeMetadata struct {
	text string
	err  error
	path string
}

func (f *fakeMetadata) GetMetadata(_ context.Context, in *imds.GetMetadataInput,
	_ ...func(*imds.Options)) (*imds.GetMetadataOutput, error) {
	f.path = in.Path
	if f.err != nil {
		return nil, f.err
	}
	return &imds.GetMetadataOutput{Content: io.NopCloser(strings.NewReader(f.text))}, nil
}

func TestPrivateIPv4ReadsTheAddressOfTheInstance(t *testing.T) {
	metadata := &fakeMetadata{text: "10.0.1.5\n"}
	address, err := awsrunner.PrivateIPv4(context.Background(), metadata)
	if err != nil || address != "10.0.1.5" || metadata.path != "local-ipv4" {
		t.Fatalf("address = %q, error = %v, path = %q", address, err, metadata.path)
	}
}

func TestPrivateIPv4RefusesAnAnswerThatIsNotAnIPv4Address(t *testing.T) {
	cases := []struct {
		metadata *fakeMetadata
		want     string
	}{
		{&fakeMetadata{err: errors.New("no token")}, "from IMDSv2: no token"},
		{&fakeMetadata{text: "fd00::5"}, "which is not an IPv4 address"},
		{&fakeMetadata{text: ""}, "which is not an IPv4 address"},
	}
	for _, c := range cases {
		_, err := awsrunner.PrivateIPv4(context.Background(), c.metadata)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: error = %v, want %q", c.metadata, err, c.want)
		}
	}
}
