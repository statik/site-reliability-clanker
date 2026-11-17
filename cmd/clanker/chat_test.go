package main

import (
	"strings"
	"testing"
)

// clearChat removes every setting that buildChat reads, so that the environment of the
// person who runs the tests changes nothing.
func clearChat(t *testing.T) {
	t.Helper()
	for _, name := range []string{slackSigningSecretEnv, slackBotTokenEnv, slackChannelEnv,
		teamsAppIDEnv, teamsAppSecretEnv, teamsTenantIDEnv, issuerEnv, clientIDEnv,
		clientSecretEnv, publicURLEnv, adminEmailEnv} {
		t.Setenv(name, "")
	}
}

func TestWithNoChatSettingsNoChatAppIsBuilt(t *testing.T) {
	clearChat(t)
	apps, err := buildChat(&components{}, options{cloud: "local"})
	if apps != nil || err != nil {
		t.Fatalf("buildChat = %v, %v; want no app and no error", apps, err)
	}
}

func TestAChatAppWithSomeSettingsStopsTheStart(t *testing.T) {
	clearChat(t)
	t.Setenv(slackBotTokenEnv, "xoxb-test")
	_, err := buildChat(&components{}, options{cloud: "local"})
	if err == nil || !strings.Contains(err.Error(), slackSigningSecretEnv) ||
		!strings.Contains(err.Error(), slackChannelEnv) || strings.Contains(err.Error(), "xoxb") {
		t.Fatalf("error = %v, want the names of the missing settings and no secret", err)
	}
	clearChat(t)
	t.Setenv(teamsAppIDEnv, "app")
	t.Setenv(teamsTenantIDEnv, "tenant")
	if _, err := buildChat(&components{}, options{cloud: "local"}); err == nil ||
		!strings.Contains(err.Error(), teamsAppSecretEnv) {
		t.Fatalf("error = %v, want the name of the missing setting", err)
	}
}

func TestAChatAppWithNoSignInStopsTheStart(t *testing.T) {
	clearChat(t)
	t.Setenv(slackSigningSecretEnv, "secret")
	t.Setenv(slackBotTokenEnv, "xoxb-test")
	t.Setenv(slackChannelEnv, "#changes")
	_, err := buildChat(&components{}, options{cloud: "local"})
	if err == nil || !strings.Contains(err.Error(), "the sign-in is not") ||
		!strings.Contains(err.Error(), issuerEnv) {
		t.Fatalf("error = %v, want one that asks for the sign-in", err)
	}
}
