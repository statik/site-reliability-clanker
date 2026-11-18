package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/scrub"
)

func TestRegisterSecretsGivesTheValuesToTheScrubber(t *testing.T) {
	settings := map[string]string{
		clientSecretEnv:      "oidc-client-value-01",
		slackBotTokenEnv:     " chat-bot-value-02 ",
		"OPENROUTER_API_KEY": "model-key-value-03",
		publicURLEnv:         "https://clanker.example.com",
	}
	key := []byte("signing key bytes of the broker!")
	held := secrets{databaseURL: "postgres://core:db-pass-value-04@db:5432/clanker",
		signingKey: key}
	registerSecrets(held, func(name string) string { return settings[name] })
	text := strings.Join([]string{"| oidc-client-value-01 |", "run with chat-bot-value-02",
		"key model-key-value-03.", "db-pass-value-04", base64.StdEncoding.EncodeToString(key),
		"see https://clanker.example.com"}, " ; ")
	want := strings.Join([]string{"| [REDACTED] |", "run with [REDACTED]", "key [REDACTED].",
		"[REDACTED]", "[REDACTED]", "see https://clanker.example.com"}, " ; ")
	if got := scrub.Text(text); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
