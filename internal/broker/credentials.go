package broker

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/store"
)

//go:embed identities.yaml
var identitiesManifest []byte

//go:embed identities-app.yaml
var appIdentitiesManifest []byte

const identityNamespace = "clanker-system"

// Credential is what the broker gives a runner for one grant.
type Credential struct {
	GrantID string
	// KubeConfig is the path of a kubeconfig with a short-lived token. It is empty for a
	// layer that needs no cluster credential.
	KubeConfig string
	// Mechanism says in plain words what the credential is.
	Mechanism string
}

// clusterLayers are the layers that need a Kubernetes credential on the local target.
var clusterLayers = map[string]bool{"30-data": true, "40-platform": true, "50-app": true}

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

// EnsureIdentities creates the service accounts and bindings on the cluster of env.
func (b *Broker) EnsureIdentities(ctx context.Context, env string) error {
	admin, err := b.adminKubeconfig(env)
	if err != nil {
		return err
	}
	if _, err := kubectl(ctx, admin, identitiesManifest, "apply", "-f", "-"); err != nil {
		return fmt.Errorf("create broker identities on %s: %w", env, err)
	}
	if _, err := kubectl(ctx, admin, nil, "get", "namespace", "paperboat"); err != nil {
		return nil
	}
	if _, err := kubectl(ctx, admin, appIdentitiesManifest, "apply", "-f", "-"); err != nil {
		return fmt.Errorf("create namespace bindings on %s: %w", env, err)
	}
	return nil
}

// Exchange checks the grant and returns a credential that expires with it.
func (b *Broker) Exchange(ctx context.Context, g Grant, p Presentation) (Credential, error) {
	if err := b.Check(g, p); err != nil {
		return Credential{}, err
	}
	cred := Credential{GrantID: g.ID}
	if g.Kind == KindApply && !clusterLayers[g.Layer] {
		cred.Mechanism = "local Docker socket; this layer has no cluster credential"
		return cred, b.setStatus(g.ID, "active", false)
	}
	admin, err := b.adminKubeconfig(g.Env)
	if err != nil {
		return Credential{}, err
	}
	if err := b.EnsureIdentities(ctx, g.Env); err != nil {
		return Credential{}, err
	}
	account := "clanker-" + g.Identity
	remaining := store.ParseTime(g.Expires).Sub(b.clock.Now())
	// The Kubernetes API refuses a token lifetime under ten minutes.
	lifetime := max(remaining, 10*time.Minute)
	token, err := kubectl(ctx, admin, nil, "create", "token", account, "-n", identityNamespace,
		fmt.Sprintf("--duration=%ds", int(lifetime.Seconds())), "--audience=https://kubernetes.default.svc.cluster.local")
	if err != nil {
		return Credential{}, fmt.Errorf("issue token for %s under grant %s: %w", account, g.ID, err)
	}
	path, err := b.writeKubeconfig(ctx, admin, g.ID, account, strings.TrimSpace(token))
	if err != nil {
		return Credential{}, err
	}
	cred.KubeConfig = path
	cred.Mechanism = fmt.Sprintf("Kubernetes TokenRequest for service account %s, lifetime %s",
		account, lifetime.Round(time.Second))
	return cred, b.setStatus(g.ID, "active", false)
}

func (b *Broker) writeKubeconfig(ctx context.Context, admin, grantID, user, token string) (string, error) {
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
	path := filepath.Join(b.credsDir, grantID+".kubeconfig")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return "", fmt.Errorf("write kubeconfig for grant %s: %w", grantID, err)
	}
	return path, nil
}

// Revoke ends a grant and removes its credential. For an apply, runbook or debug grant on a
// cluster it also deletes and recreates the service account, which makes every token of that
// account invalid at once.
func (b *Broker) Revoke(ctx context.Context, g Grant, outcome string) error {
	path := filepath.Join(b.credsDir, g.ID+".kubeconfig")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove credential of grant %s: %w", g.ID, err)
	}
	if err := b.setStatus(g.ID, outcome, true); err != nil {
		return err
	}
	if g.Kind == KindPlan || (g.Kind == KindApply && !clusterLayers[g.Layer]) {
		return nil
	}
	admin, err := b.adminKubeconfig(g.Env)
	if err != nil {
		return err
	}
	account := "clanker-" + g.Identity
	if _, err := kubectl(ctx, admin, nil, "delete", "serviceaccount", account, "-n",
		identityNamespace, "--ignore-not-found"); err != nil {
		return fmt.Errorf("revoke tokens of %s for grant %s: %w", account, g.ID, err)
	}
	return b.EnsureIdentities(ctx, g.Env)
}

// ExpireOverdue marks grants whose window ended without a revoke. It returns them.
func (b *Broker) ExpireOverdue() ([]Grant, error) {
	now := store.FormatTime(b.clock.Now())
	rows, err := b.db.Query(`SELECT `+grantColumns+` FROM grants
		WHERE status IN ('issued', 'active') AND expires <= ?`, now)
	if err != nil {
		return nil, fmt.Errorf("find overdue grants: %w", err)
	}
	overdue := []Grant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		overdue = append(overdue, g)
	}
	rows.Close()
	for _, g := range overdue {
		if err := b.setStatus(g.ID, "expired", true); err != nil {
			return nil, err
		}
	}
	return overdue, nil
}
