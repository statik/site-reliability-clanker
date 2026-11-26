package awstarget

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"

	"github.com/statik/site-reliability-clanker/internal/broker"
)

// ClusterLayer is the layer whose outputs name the cluster of an environment.
const ClusterLayer = "20-cluster"

// Cluster is what a kubeconfig needs from the outputs of layer 20-cluster on AWS.
type Cluster struct {
	Name     string
	Endpoint string
	// CAData is the certificate of the cluster CA, PEM in base64, as a kubeconfig takes it.
	CAData string
}

// ParseCluster reads the outputs of layer 20-cluster on AWS, as the last apply of the layer
// reported them. An environment whose layer 20-cluster was never applied has no outputs.
func ParseCluster(env string, outputs map[string]any) (Cluster, error) {
	if len(outputs) == 0 {
		return Cluster{}, fmt.Errorf("environment %s has no cluster yet: apply layer %s first",
			env, ClusterLayer)
	}
	text := func(name string) (string, error) {
		value, _ := outputs[name].(string)
		if value == "" {
			return "", fmt.Errorf("layer %s of environment %s has no output %s: apply the layer "+
				"from a commit that has it", ClusterLayer, env, name)
		}
		return value, nil
	}
	var c Cluster
	var err error
	if c.Name, err = text("cluster_name"); err != nil {
		return Cluster{}, err
	}
	if c.Endpoint, err = text("cluster_endpoint"); err != nil {
		return Cluster{}, err
	}
	if c.CAData, err = text("cluster_certificate_authority_data"); err != nil {
		return Cluster{}, err
	}
	if address, err := url.Parse(c.Endpoint); err != nil || address.Scheme != "https" ||
		address.Host == "" {
		return Cluster{}, fmt.Errorf("the output cluster_endpoint of layer %s of environment "+
			"%s is %q, which is not an https URL", ClusterLayer, env, c.Endpoint)
	}
	if err := checkCA(c.CAData); err != nil {
		return Cluster{}, fmt.Errorf("the output cluster_certificate_authority_data of layer %s "+
			"of environment %s: %w", ClusterLayer, env, err)
	}
	return c, nil
}

// checkCA refuses data that is not a PEM certificate in base64.
func checkCA(data string) error {
	text, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("it is not base64: %w", err)
	}
	block, _ := pem.Decode(text)
	if block == nil || block.Type != "CERTIFICATE" {
		return errors.New("it is not a PEM certificate in base64")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return fmt.Errorf("it is not a valid certificate: %w", err)
	}
	return nil
}

// needsKubeconfig reports whether the job of a grant runs kubectl with a kubeconfig of the
// broker. A plan or an apply job gets none: the providers of the layers on AWS authenticate
// with `aws eks get-token` on their own.
func needsKubeconfig(g broker.Grant) bool {
	return g.Kind == broker.KindDebug || g.Kind == broker.KindRunbook
}

// Kubeconfig returns the text of a kubeconfig for the cluster, with one user for the
// identity of a grant. The user gets its token from `aws eks get-token`, which signs with
// the keys in its environment. The runner puts the keys of the STS session of the grant
// only into the environment of the program of the job, so the text holds no secret.
func Kubeconfig(c Cluster, region, identity string) (string, error) {
	user := "clanker-" + identity
	config := map[string]any{
		"apiVersion": "v1",
		"kind":       "Config",
		"clusters": []any{map[string]any{"name": c.Name, "cluster": map[string]any{
			"server": c.Endpoint, "certificate-authority-data": c.CAData}}},
		"users": []any{map[string]any{"name": user, "user": map[string]any{
			"exec": map[string]any{
				"apiVersion": "client.authentication.k8s.io/v1beta1",
				"command":    "aws",
				"args": []string{"eks", "get-token", "--cluster-name", c.Name, "--region",
					region, "--output", "json"},
				"interactiveMode": "Never",
			}}}},
		"contexts": []any{map[string]any{"name": "grant", "context": map[string]any{
			"cluster": c.Name, "user": user}}},
		"current-context": "grant",
	}
	text, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode the kubeconfig of the cluster %s: %w", c.Name, err)
	}
	return string(text), nil
}
