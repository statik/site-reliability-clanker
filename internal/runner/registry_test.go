package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// registryTofu copies the registry configurations that Helm and Docker read in a job to
// $FAKE_TOFU_SEEN: the file of HELM_REGISTRY_CONFIG, registry/config.json below
// HELM_CONFIG_HOME and config.json of DOCKER_CONFIG. A plan writes an empty plan file.
// Each command works.
const registryTofu = `#!/bin/sh
cp "$HELM_REGISTRY_CONFIG" "$FAKE_TOFU_SEEN/helm.json"
cp "$HELM_CONFIG_HOME/registry/config.json" "$FAKE_TOFU_SEEN/helm-home.json"
cp "$DOCKER_CONFIG/config.json" "$FAKE_TOFU_SEEN/docker.json"
for arg in "$@"; do
  case "$arg" in -out=*) : > "${arg#-out=}" ;; esac
done
`

// userRegistryConfig is the Docker configuration of a user of Docker Desktop: no
// credential in the file, and a helper that can wait for the keychain.
const userRegistryConfig = `{"auths": {}, "credsStore": "desktop"}`

// asksNoHelper reports why Helm's registry client would ask a credential helper with the
// configuration, or "" when it would not. The client asks the helper of credsStore or of
// credHelpers, and the native store of the platform when the file configures nothing.
func asksNoHelper(t *testing.T, file string) string {
	t.Helper()
	text, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Auths       map[string]json.RawMessage `json:"auths"`
		CredsStore  string                     `json:"credsStore"`
		CredHelpers map[string]string          `json:"credHelpers"`
	}
	if err := json.Unmarshal(text, &config); err != nil {
		return "it is not JSON: " + err.Error()
	}
	switch {
	case config.CredsStore != "":
		return "it names the credential store " + config.CredsStore
	case len(config.CredHelpers) > 0:
		return "it names credential helpers"
	case len(config.Auths) == 0:
		return "it has no auths, so the client asks the native store of the platform"
	}
	return ""
}

func TestATofuJobAsksNoCredentialHelperOfTheUserForARegistry(t *testing.T) {
	user := t.TempDir()
	if err := os.WriteFile(filepath.Join(user, "config.json"), []byte(userRegistryConfig),
		0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(user, "registry"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "registry", "config.json"),
		[]byte(userRegistryConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", user)
	t.Setenv("HELM_REGISTRY_CONFIG", filepath.Join(user, "config.json"))
	t.Setenv("HELM_CONFIG_HOME", user)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tofu"), []byte(registryTofu), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	seen := t.TempDir()
	t.Setenv("FAKE_TOFU_SEEN", seen)
	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "platform", "stacks", "local", "50-app"),
		0o755); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	_, err := newRunner(t).Plan(context.Background(), Job{Env: "local", Cloud: "local",
		Layer: "50-app", Checkout: Checkout{Dir: checkout}, Log: &log})
	if err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	for _, name := range []string{"helm.json", "helm-home.json", "docker.json"} {
		if why := asksNoHelper(t, filepath.Join(seen, name)); why != "" {
			t.Errorf("the registry configuration %s of the job asks a helper: %s", name, why)
		}
	}
}
