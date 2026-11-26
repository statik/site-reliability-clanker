package main

import (
	"context"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/dispatch"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
)

// someLauncher is a launcher of runners that a test never calls.
type someLauncher struct{}

func (someLauncher) Launch(context.Context, string) error { return nil }

func TestTheCoreOnAWSStandsByAndNamesEachMissingSetting(t *testing.T) {
	withBackend := orchestrator.EnvConfig{Name: "prod", Cloud: "aws",
		Backend: map[string]string{"bucket": "state"}}
	noBackend := orchestrator.EnvConfig{Name: "prod", Cloud: "aws"}
	fargate, none := someLauncher{}, noLauncher{cloud: "aws"}
	cases := []struct {
		name      string
		env       orchestrator.EnvConfig
		hasIssuer bool
		launcher  dispatch.Launcher
		want      string
	}{
		{"all present", withBackend, true, fargate, ""},
		{"no bootstrap", noBackend, false, fargate, "set " + bootstrapEnv},
		{"no issuer", withBackend, false, fargate, "set " + bootstrapEnv},
		{"no backend", noBackend, true, fargate, "set " + bootstrapEnv},
		{"no runner setting", withBackend, true, none, "set " + awsRunnerEnv},
		{"nothing", noBackend, false, none, "set " + bootstrapEnv + " and " + awsRunnerEnv},
	}
	for _, c := range cases {
		if got := standbyReason("aws", c.env, c.hasIssuer, c.launcher); got != c.want {
			t.Errorf("%s: reason %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTheLocalTargetNeverStandsByAndAnotherCloudAlwaysDoes(t *testing.T) {
	if got := standbyReason("local", orchestrator.EnvConfig{Cloud: "local"}, false,
		someLauncher{}); got != "" {
		t.Errorf("local: reason %q, want none", got)
	}
	want := "the control plane cannot plan a layer on gcp yet"
	if got := standbyReason("gcp", orchestrator.EnvConfig{Cloud: "gcp"}, true,
		someLauncher{}); got != want {
		t.Errorf("gcp: reason %q, want %q", got, want)
	}
}

func TestACloudHasOnlyTheChannelsThatItsSettingsName(t *testing.T) {
	got := channelTargets("aws", "127.0.0.1:7071", environment(nil))
	if got != (targets{}) {
		t.Errorf("aws with no setting: %+v, want no channel", got)
	}
	got = channelTargets("aws", "127.0.0.1:7071", environment(map[string]string{
		"CLANKER_SMTP_ADDR": "smtp.example.com:587", "CLANKER_SLACK_WEBHOOK": "https://slack"}))
	if got != (targets{email: "smtp.example.com:587", slack: "https://slack"}) {
		t.Errorf("aws with email and Slack: %+v", got)
	}
}

func TestTheLocalTargetSendsToMailpitAndTheSinkWithoutASetting(t *testing.T) {
	got := channelTargets("local", "127.0.0.1:7071", environment(map[string]string{
		"CLANKER_TEAMS_WEBHOOK": "https://teams"}))
	want := targets{email: "127.0.0.1:1025", slack: "http://127.0.0.1:7071/slack",
		teams: "https://teams", discord: "http://127.0.0.1:7071/discord"}
	if got != want {
		t.Errorf("local: %+v, want %+v", got, want)
	}
}

func TestTheLinksOfNotificationsStartWithThePublicURL(t *testing.T) {
	public := environment(map[string]string{publicURLEnv: "https://src.example.com/"})
	if got := linkBase("10.0.1.5:7070", public); got != "https://src.example.com" {
		t.Errorf("with a public URL: %q", got)
	}
	if got := linkBase("127.0.0.1:7070", environment(nil)); got != "http://127.0.0.1:7070" {
		t.Errorf("without a public URL: %q", got)
	}
}
