package awstarget

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/broker"
)

const (
	debuggerRole = "arn:aws:iam::111122223333:role/clanker/clanker-example-debugger"
	runbookRole  = "arn:aws:iam::111122223333:role/clanker/clanker-example-runbook"
	endpoint     = "https://EXAMPLE0123.gr7.eu-west-1.eks.amazonaws.com"
)

// exampleCA returns a self-signed CA certificate, PEM in base64, as EKS gives it.
func exampleCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1),
		Subject: pkix.Name{CommonName: "kubernetes"}, NotBefore: expiry.Add(-time.Hour),
		NotAfter: expiry.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func clusterOutputs(ca string) map[string]any {
	return map[string]any{"cluster_name": "clanker-example", "cluster_endpoint": endpoint,
		"cluster_certificate_authority_data": ca, "kubeconfig_path": "",
		"kubernetes_version": "1.36"}
}

// clusterIssuer is an issuer for an environment in eu-west-1 whose layer 20-cluster has the
// outputs.
func clusterIssuer(client STS, r *recorder, outputs map[string]any) Credentials {
	c := issuer(client, r)
	c.Roles["debugger"], c.Roles["runbook"] = debuggerRole, runbookRole
	c.Region = "eu-west-1"
	c.ClusterOutputs = func(env string) (map[string]any, error) {
		if env != "example" {
			return nil, errors.New("asked for the cluster of environment " + env)
		}
		return outputs, nil
	}
	return c
}

func grantOf(kind, identity string) broker.Grant {
	return broker.Grant{ID: "G-0a1b2c3d4e5f", WorkItem: "WI-0201", Env: "example",
		Layer: kind, Identity: identity, Kind: kind}
}

// kubeconfigShape is the part of a kubeconfig that kubectl reads.
type kubeconfigShape struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Clusters   []struct {
		Name    string `json:"name"`
		Cluster struct {
			Server string `json:"server"`
			CAData string `json:"certificate-authority-data"`
		} `json:"cluster"`
	} `json:"clusters"`
	Users []struct {
		Name string `json:"name"`
		User struct {
			Exec struct {
				APIVersion      string   `json:"apiVersion"`
				Command         string   `json:"command"`
				Args            []string `json:"args"`
				InteractiveMode string   `json:"interactiveMode"`
			} `json:"exec"`
		} `json:"user"`
	} `json:"users"`
	Contexts []struct {
		Name    string `json:"name"`
		Context struct {
			Cluster string `json:"cluster"`
			User    string `json:"user"`
		} `json:"context"`
	} `json:"contexts"`
	CurrentContext string `json:"current-context"`
}

func TestADebugOrRunbookGrantGetsAKubeconfigThatSignsWithTheSession(t *testing.T) {
	ca := exampleCA(t)
	for kind, identity := range map[string]string{broker.KindDebug: "debugger",
		broker.KindRunbook: "runbook"} {
		client, events := &fakeSTS{}, &recorder{}
		cred, err := clusterIssuer(client, events, clusterOutputs(ca)).Issue(
			context.Background(), grantOf(kind, identity), 15*time.Minute)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if cred.AWS == nil || cred.AWS.SessionToken != token {
			t.Fatalf("%s: AWS session = %v, want the session of the role", kind, cred.AWS)
		}
		var config kubeconfigShape
		if err := json.Unmarshal([]byte(cred.KubeConfig), &config); err != nil {
			t.Fatalf("%s: the kubeconfig is not JSON: %v", kind, err)
		}
		user := "clanker-" + identity
		if config.APIVersion != "v1" || config.Kind != "Config" || len(config.Clusters) != 1 ||
			config.Clusters[0].Name != "clanker-example" ||
			config.Clusters[0].Cluster.Server != endpoint ||
			config.Clusters[0].Cluster.CAData != ca || len(config.Users) != 1 ||
			config.Users[0].Name != user || len(config.Contexts) != 1 ||
			config.Contexts[0].Context.Cluster != "clanker-example" ||
			config.Contexts[0].Context.User != user ||
			config.CurrentContext != config.Contexts[0].Name {
			t.Errorf("%s: kubeconfig = %s", kind, cred.KubeConfig)
		}
		exec := config.Users[0].User.Exec
		wantArgs := []string{"eks", "get-token", "--cluster-name", "clanker-example",
			"--region", "eu-west-1", "--output", "json"}
		if exec.APIVersion != "client.authentication.k8s.io/v1beta1" || exec.Command != "aws" ||
			!reflect.DeepEqual(exec.Args, wantArgs) || exec.InteractiveMode != "Never" {
			t.Errorf("%s: exec = %+v", kind, exec)
		}
		for _, secret := range []string{secretKey, token} {
			if strings.Contains(cred.KubeConfig, secret) ||
				strings.Contains(cred.Mechanism, secret) {
				t.Errorf("%s: the kubeconfig or the mechanism holds a key of the session", kind)
			}
		}
		if !strings.Contains(cred.Mechanism, "EKS cluster clanker-example") ||
			events.events[0].Data["cluster"] != "clanker-example" {
			t.Errorf("%s: mechanism %q, event %v", kind, cred.Mechanism, events.events[0].Data)
		}
	}
}

func TestAPlanOrApplyGrantGetsNoKubeconfig(t *testing.T) {
	for _, g := range []broker.Grant{applyGrant(), grantOf(broker.KindPlan, "planner")} {
		c := issuer(&fakeSTS{}, &recorder{})
		c.ClusterOutputs = func(string) (map[string]any, error) {
			t.Fatalf("%s: the issuer read the cluster", g.Kind)
			return nil, nil
		}
		cred, err := c.Issue(context.Background(), g, 15*time.Minute)
		if err != nil || cred.KubeConfig != "" || cred.AWS == nil {
			t.Errorf("%s: kubeconfig %q, session %v, error %v", g.Kind, cred.KubeConfig,
				cred.AWS, err)
		}
	}
}

func TestAKubeconfigNeedsTheOutputsOfACluster(t *testing.T) {
	ca := exampleCA(t)
	with := func(name string, value any) map[string]any {
		outputs := clusterOutputs(ca)
		outputs[name] = value
		return outputs
	}
	cases := map[string]struct {
		outputs map[string]any
		want    string
	}{
		"no cluster": {map[string]any{},
			"environment example has no cluster yet: apply layer 20-cluster first"},
		"no name": {with("cluster_name", ""),
			"layer 20-cluster of environment example has no output cluster_name"},
		"no CA output": {with("cluster_certificate_authority_data", nil),
			"has no output cluster_certificate_authority_data: apply the layer from a commit"},
		"an http endpoint": {with("cluster_endpoint", "http://cluster.example.com"),
			`is "http://cluster.example.com", which is not an https URL`},
		"a CA that is not base64": {with("cluster_certificate_authority_data", "not base64!"),
			"cluster_certificate_authority_data of layer 20-cluster of environment example: " +
				"it is not base64"},
		"a CA in PEM, not base64": {with("cluster_certificate_authority_data",
			base64.StdEncoding.EncodeToString([]byte("not a PEM block"))),
			"it is not a PEM certificate in base64"},
	}
	for label, c := range cases {
		client := &fakeSTS{}
		_, err := clusterIssuer(client, &recorder{}, c.outputs).Issue(context.Background(),
			grantOf(broker.KindDebug, "debugger"), 15*time.Minute)
		if err == nil || !strings.Contains(err.Error(), c.want) ||
			!strings.HasPrefix(err.Error(), "grant G-0a1b2c3d4e5f: ") {
			t.Errorf("%s: error = %v, want %q", label, err, c.want)
		}
		if client.in != nil {
			t.Errorf("%s: STS was called for a grant that gets no kubeconfig", label)
		}
	}
}

func TestAFailureToReadTheClusterGivesNoSession(t *testing.T) {
	client := &fakeSTS{}
	c := clusterIssuer(client, &recorder{}, nil)
	c.ClusterOutputs = func(string) (map[string]any, error) {
		return nil, errors.New("read outputs of example/20-cluster: connection refused")
	}
	_, err := c.Issue(context.Background(), grantOf(broker.KindRunbook, "runbook"),
		15*time.Minute)
	if err == nil || err.Error() != "grant G-0a1b2c3d4e5f: read outputs of example/20-cluster: "+
		"connection refused" || client.in != nil {
		t.Errorf("error = %v, STS called = %v", err, client.in != nil)
	}
}
