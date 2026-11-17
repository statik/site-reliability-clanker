package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/api"
	"github.com/statik/site-reliability-clanker/internal/chatops"
	"github.com/statik/site-reliability-clanker/internal/notify"
	"github.com/statik/site-reliability-clanker/internal/signin"
)

// The settings of the chat apps. With the three settings of a platform, people approve a
// tier T2 change inside that platform, and the app takes the place of the webhook. Without
// them the platform gets the webhook message with a link, as before.
//
// The signing secret, the bot token and the app secret are secrets: on a cloud account they
// must come from a secret store and not from the environment of the image.
const (
	slackSigningSecretEnv = "CLANKER_SLACK_SIGNING_SECRET"
	slackBotTokenEnv      = "CLANKER_SLACK_BOT_TOKEN"
	slackChannelEnv       = "CLANKER_SLACK_CHANNEL"
	teamsAppIDEnv         = "CLANKER_TEAMS_APP_ID"
	teamsAppSecretEnv     = "CLANKER_TEAMS_APP_SECRET"
	teamsTenantIDEnv      = "CLANKER_TEAMS_TENANT_ID"
)

// chatApps holds the shared part of the chat approval and the adapter of each platform
// that is configured.
type chatApps struct {
	service *chatops.Service
	slack   *chatops.Slack
	teams   *chatops.Teams
}

// platformSettings reads the settings of one platform. It returns nil when none is set,
// and an error when only some are set.
func platformSettings(platform string, names ...string) (map[string]string, error) {
	values := map[string]string{}
	missing := []string{}
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			values[name] = value
		} else {
			missing = append(missing, name)
		}
	}
	if len(values) == 0 {
		return nil, nil
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("the %s app is configured in part: set also %s, or remove the "+
			"other settings of the app", platform, strings.Join(missing, ", "))
	}
	return values, nil
}

// buildChat makes the chat apps that the settings ask for. Chat approval needs the sign-in,
// because a person links a chat account through it.
func buildChat(c *components, o options) (*chatApps, error) {
	slack, err := platformSettings("Slack", slackSigningSecretEnv, slackBotTokenEnv,
		slackChannelEnv)
	if err != nil {
		return nil, err
	}
	teams, err := platformSettings("Teams", teamsAppIDEnv, teamsAppSecretEnv, teamsTenantIDEnv)
	if err != nil {
		return nil, err
	}
	if slack == nil && teams == nil {
		return nil, nil
	}
	signIn, err := loadSignIn(o)
	if err != nil {
		return nil, err
	}
	if signIn == nil {
		return nil, errors.New("a chat app is configured and the sign-in is not: a person " +
			"links a chat account through the sign-in. Set " + issuerEnv + ", " + clientIDEnv +
			", " + clientSecretEnv + ", " + publicURLEnv + " and " + adminEmailEnv +
			", or remove the settings of the chat app")
	}
	return newChatApps(chatops.New(c.db, c.orch, c.log, signIn.provider.PublicURL), slack, teams)
}

// newChatApps makes the adapter of each platform that has settings.
func newChatApps(service *chatops.Service, slack, teams map[string]string) (*chatApps, error) {
	apps := &chatApps{service: service}
	var err error
	if slack != nil {
		apps.slack, err = chatops.NewSlack(service, chatops.SlackConfig{
			SigningSecret: slack[slackSigningSecretEnv], BotToken: slack[slackBotTokenEnv],
			Channel: slack[slackChannelEnv]})
		if err != nil {
			return nil, err
		}
	}
	if teams != nil {
		apps.teams, err = chatops.NewTeams(service, chatops.TeamsConfig{
			AppID: teams[teamsAppIDEnv], AppSecret: teams[teamsAppSecretEnv],
			TenantID: teams[teamsTenantIDEnv]})
		if err != nil {
			return nil, err
		}
	}
	return apps, nil
}

// adapters returns the adapter of Slack and of Teams for the notifier. A chat app takes
// the place of the webhook of its platform, so that one event gives one message there.
func (a *chatApps) adapters(slack, teams notify.Adapter) (notify.Adapter, notify.Adapter) {
	if a != nil && a.slack != nil {
		slack = a.slack
	}
	if a != nil && a.teams != nil {
		teams = a.teams
	}
	return slack, teams
}

// run keeps the posted requests up to date until ctx ends. It does nothing when no chat
// app is configured.
func (a *chatApps) run(ctx context.Context) {
	if a != nil {
		a.service.Run(ctx)
	}
}

// serve adds the endpoints of the chat platforms and the link page to the API. The link
// page is below /auth/ and asks for a browser session itself.
func (a *chatApps) serve(server *api.Server, sessions *signin.Service) {
	a.service.Sessions = sessions
	auth := http.NewServeMux()
	auth.Handle("/auth/chat-link", a.service.LinkHandler())
	auth.Handle("/auth/", server.SignIn)
	server.SignIn = auth
	if a.slack != nil {
		server.Slack = a.slack
	}
	if a.teams != nil {
		server.Teams = a.teams
	}
}
