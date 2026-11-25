package awsrunner

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
)

// Metadata is the one call of the instance metadata service that this package uses.
type Metadata interface {
	GetMetadata(ctx context.Context, in *imds.GetMetadataInput, options ...func(*imds.Options)) (
		*imds.GetMetadataOutput, error)
}

// NewMetadata returns the instance metadata service with IMDSv2 only: when it gets no
// session token it fails, and does not fall back to IMDSv1. It makes no call.
func NewMetadata() Metadata {
	return imds.New(imds.Options{EnableFallback: aws.FalseTernary})
}

// PrivateIPv4 returns the private IPv4 address of this instance.
func PrivateIPv4(ctx context.Context, metadata Metadata) (string, error) {
	out, err := metadata.GetMetadata(ctx, &imds.GetMetadataInput{Path: "local-ipv4"})
	if err != nil {
		return "", fmt.Errorf("read the private IPv4 address of the instance from IMDSv2: %w",
			err)
	}
	defer out.Content.Close()
	text, err := io.ReadAll(io.LimitReader(out.Content, 64))
	if err != nil {
		return "", fmt.Errorf("read the private IPv4 address of the instance from IMDSv2: %w",
			err)
	}
	address, err := netip.ParseAddr(strings.TrimSpace(string(text)))
	if err != nil || !address.Is4() {
		return "", fmt.Errorf("IMDSv2 gave %q as the private IPv4 address of the instance, "+
			"which is not an IPv4 address", text)
	}
	return address.String(), nil
}
