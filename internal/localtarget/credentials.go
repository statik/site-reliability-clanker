// Package localtarget holds the parts of the control plane that work only on the local
// target, because they start kubectl. Only the wiring of the local target imports it. The
// core must not import it.
package localtarget

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/pack"
)

//go:embed identities.yaml
var identitiesManifest []byte

//go:embed identities-app.yaml
var appIdentitiesManifest []byte

const identityNamespace = "clanker-system"

// clusterLayers are the layers that need a Kubernetes credential on the local target.
var clusterLayers = map[string]bool{"30-data": true, "40-platform": true, "50-app": true}

// Credentials issues the credentials of grants on the local target. A credential is a
// kubeconfig with a short-lived token of a Kubernetes service account.
type Credentials struct {
	// AdminKubeconfig returns the path of the administrator kubeconfig of an environment.
	AdminKubeconfig func(env string) (string, error)
	// App is the application pack. Its namespaces get the bindings of the identities.
	App pack.Pack
}

func kubectl(ctx context.Context, kubeconfig string, stdin []byte, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", kubeconfig},
		args...)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err,
			strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// appIdentities returns the bindings of the identities in the namespaces of the
// application. The names are from the pack, which checked them against a pattern.
func appIdentities(app pack.Pack) []byte {
	return []byte(strings.NewReplacer("{{namespace}}", app.Namespace,
		"{{data_namespace}}", app.DataNamespace).Replace(string(appIdentitiesManifest)))
}

// ensureIdentities creates the service accounts and bindings on the cluster.
func ensureIdentities(ctx context.Context, admin, env string, app pack.Pack) error {
	if _, err := kubectl(ctx, admin, identitiesManifest, "apply", "-f", "-"); err != nil {
		return fmt.Errorf("create broker identities on %s: %w", env, err)
	}
	if _, err := kubectl(ctx, admin, nil, "get", "namespace", app.Namespace); err != nil {
		return nil
	}
	if _, err := kubectl(ctx, admin, appIdentities(app), "apply", "-f", "-"); err != nil {
		return fmt.Errorf("create namespace bindings on %s: %w", env, err)
	}
	return nil
}

// Issue returns the credential of a grant that the broker checked. The token lives for
// remaining, or for ten minutes when remaining is shorter.
func (c Credentials) Issue(ctx context.Context, g broker.Grant, remaining time.Duration) (
	broker.Credential, error) {
	if g.Kind == broker.KindApply && !clusterLayers[g.Layer] {
		return broker.Credential{
			Mechanism: "local Docker socket; this layer has no cluster credential"}, nil
	}
	admin, err := c.AdminKubeconfig(g.Env)
	if err != nil {
		return broker.Credential{}, err
	}
	if err := ensureIdentities(ctx, admin, g.Env, c.App); err != nil {
		return broker.Credential{}, err
	}
	account := "clanker-" + g.Identity
	// The Kubernetes API refuses a token lifetime under ten minutes.
	lifetime := max(remaining, 10*time.Minute)
	token, err := kubectl(ctx, admin, nil, "create", "token", account, "-n", identityNamespace,
		fmt.Sprintf("--duration=%ds", int(lifetime.Seconds())),
		"--audience=https://kubernetes.default.svc.cluster.local")
	if err != nil {
		return broker.Credential{}, fmt.Errorf("issue token for %s under grant %s: %w", account,
			g.ID, err)
	}
	text, err := grantKubeconfig(ctx, admin, g.ID, account, strings.TrimSpace(token))
	if err != nil {
		return broker.Credential{}, err
	}
	return broker.Credential{KubeConfig: text, Mechanism: fmt.Sprintf(
		"Kubernetes TokenRequest for service account %s, lifetime %s", account,
		lifetime.Round(time.Second))}, nil
}

// grantKubeconfig returns a kubeconfig for the cluster of the administrator kubeconfig,
// with the token of the grant as its only user.
func grantKubeconfig(ctx context.Context, admin, grantID, user, token string) (string, error) {
	raw, err := kubectl(ctx, admin, nil, "config", "view", "--raw", "--minify", "-o", "json")
	if err != nil {
		return "", fmt.Errorf("read cluster address for grant %s: %w", grantID, err)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return "", fmt.Errorf("decode administrator kubeconfig: %w", err)
	}
	clusters, _ := config["clusters"].([]any)
	if len(clusters) != 1 {
		return "", fmt.Errorf("administrator kubeconfig has %d clusters, want 1", len(clusters))
	}
	cluster, _ := clusters[0].(map[string]any)
	clusterName, _ := cluster["name"].(string)
	config["users"] = []any{map[string]any{"name": user, "user": map[string]any{"token": token}}}
	config["contexts"] = []any{map[string]any{"name": "grant", "context": map[string]any{
		"cluster": clusterName, "user": user}}}
	config["current-context"] = "grant"
	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode kubeconfig for grant %s: %w", grantID, err)
	}
	return string(out), nil
}

// Withdraw makes the credential of an ended grant invalid. For an apply, runbook or debug
// grant on a cluster it deletes and recreates the service account, which makes every token
// of that account invalid at once.
func (c Credentials) Withdraw(ctx context.Context, g broker.Grant) error {
	if g.Kind == broker.KindPlan || (g.Kind == broker.KindApply && !clusterLayers[g.Layer]) {
		return nil
	}
	admin, err := c.AdminKubeconfig(g.Env)
	if err != nil {
		return err
	}
	account := "clanker-" + g.Identity
	if _, err := kubectl(ctx, admin, nil, "delete", "serviceaccount", account, "-n",
		identityNamespace, "--ignore-not-found"); err != nil {
		return fmt.Errorf("revoke tokens of %s for grant %s: %w", account, g.ID, err)
	}
	return ensureIdentities(ctx, admin, g.Env, c.App)
}

// ManualChange returns a function that changes the cluster of an environment outside the
// pipeline, as a person with kubectl would. The simulator uses it to cause drift. The
// change is in the namespace of the data services of the application.
func ManualChange(adminKubeconfig func(env string) (string, error), namespace string) func(
	context.Context, string) error {
	return func(ctx context.Context, env string) error {
		admin, err := adminKubeconfig(env)
		if err != nil {
			return err
		}
		_, err = kubectl(ctx, admin, nil, "-n", namespace, "patch", "configmap",
			"backup-policy", "--type=merge", "-p", `{"data":{"retention_days":"1"}}`)
		return err
	}
}
