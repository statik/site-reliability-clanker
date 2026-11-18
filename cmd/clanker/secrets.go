package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/launch"
	"github.com/statik/site-reliability-clanker/internal/scrub"
)

// signingKeyEnv holds the signing key of the broker for the core on a cloud, in base64.
// It is for a test on a laptop. The image for a cloud account must not carry the key in
// its environment: there the key must come from a secret store.
const signingKeyEnv = "CLANKER_SIGNING_KEY"

// secrets are the two values that the core cannot start without.
type secrets struct {
	databaseURL string
	signingKey  []byte
}

// secretSettings are the settings of the environment that hold a secret. The file
// secrets.env and Parameter Store put their settings into the environment before the core
// reads it, so this list covers them too.
var secretSettings = []string{signingKeyEnv, clientSecretEnv, slackSigningSecretEnv,
	slackBotTokenEnv, teamsAppSecretEnv, launch.TokenEnv, "OPENROUTER_API_KEY",
	"CLANKER_LLM_API_KEY"}

// registerSecrets gives the scrubber the values of the secrets that the core holds, so
// that none of them goes to a model or to a notification in any form of text.
func registerSecrets(s secrets, getenv func(string) string) {
	for _, name := range secretSettings {
		scrub.Add(strings.TrimSpace(getenv(name)))
	}
	if address, err := url.Parse(s.databaseURL); err == nil && address.User != nil {
		password, _ := address.User.Password()
		scrub.Add(password)
	}
	scrub.Add(base64.StdEncoding.EncodeToString(s.signingKey))
}

// loadSecrets returns the database address and the signing key. On the local target they
// belong to the data directory, and the first start makes them. On a cloud the core makes
// nothing: a value that is absent is an error.
func loadSecrets(o options) (secrets, error) {
	databaseURL, err := database(o)
	if err != nil {
		return secrets{}, err
	}
	if o.cloud == "local" {
		key, err := localSigningKey(o.dataDir)
		return secrets{databaseURL: databaseURL, signingKey: key}, err
	}
	encoded := os.Getenv(signingKeyEnv)
	if encoded == "" {
		return secrets{}, fmt.Errorf("the control plane for %s needs the signing key of the "+
			"broker: set %s to the key in base64", o.cloud, signingKeyEnv)
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return secrets{}, fmt.Errorf("%s is not base64: %w", signingKeyEnv, err)
	}
	return secrets{databaseURL: databaseURL, signingKey: key}, nil
}

// localSigningKey reads the signing key of the broker from the data directory, or makes
// one on the first start. Only the user can read the file.
func localSigningKey(dataDir string) ([]byte, error) {
	path := filepath.Join(dataDir, "broker.key")
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("broker signing key %s has %d bytes, want %d: delete it to "+
				"create a new key", path, len(key), ed25519.PrivateKeySize)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read broker signing key %s: %w", path, err)
	}
	if key, err = broker.NewKey(); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("write broker signing key %s: %w", path, err)
	}
	return key, nil
}
