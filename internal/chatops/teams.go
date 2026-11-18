package chatops

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/statik/site-reliability-clanker/internal/notify"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// The Teams adapter is a bot on the Bot Framework protocol, with plain HTTP and no SDK. It
// was tested only against a fake Bot Connector and a test key.
//
// Protocol, as the Microsoft documentation describes it:
//   - Tokens in both directions:
//     https://learn.microsoft.com/en-us/azure/bot-service/rest-api/bot-framework-rest-connector-authentication
//   - The routes of the Bot Connector:
//     https://learn.microsoft.com/en-us/azure/bot-service/rest-api/bot-framework-rest-connector-api-reference
//   - Action.Execute, the invoke activity and its answer:
//     https://learn.microsoft.com/en-us/adaptive-cards/authoring-cards/universal-action-model
//   - The card in the answer goes only to the person who clicked:
//     https://learn.microsoft.com/en-us/microsoftteams/platform/task-modules-and-cards/cards/universal-actions-for-adaptive-cards/user-specific-views

const (
	// TeamsPath is the messaging endpoint of the bot. The gate lets it through with no
	// sign-in: the handler accepts only a request with a valid token of the Bot Connector.
	TeamsPath = "/chat/teams/messages"

	teamsChannel = "msteams"
	// conversationKey is the row of the kv table with the channel where the bot posts.
	conversationKey = "chatops.teams.conversation"
	cardType        = "application/vnd.microsoft.card.adaptive"
)

// TeamsConfig is the registration of the bot. The bot is a single-tenant app: Microsoft
// ended the creation of multi-tenant bots on 2025-07-31.
type TeamsConfig struct {
	// AppID is the Microsoft app ID of the bot. AppSecret is its client secret, a secret.
	AppID     string
	AppSecret string
	// TenantID is the Microsoft Entra tenant of the bot and of the people who decide.
	TenantID string
	// TokenURL and MetadataURL are the addresses of the token endpoint and of the OpenID
	// metadata of the Bot Connector. Empty selects the real ones.
	TokenURL    string
	MetadataURL string
}

// Teams is the adapter for Microsoft Teams. It is a notify.Adapter for the channel "teams"
// and the handler of the messaging endpoint.
type Teams struct {
	service *Service
	config  TeamsConfig
	keys    *connectorKeys
	// connector is an HTTP client that adds the token of the bot to each request.
	connector *http.Client
}

// NewTeams returns the adapter of a Teams bot and makes it known to the shared part.
func NewTeams(service *Service, config TeamsConfig) (*Teams, error) {
	if config.AppID == "" || config.AppSecret == "" || config.TenantID == "" {
		return nil, errors.New("the Teams bot needs the app ID, the app secret and the tenant ID")
	}
	if config.TokenURL == "" {
		config.TokenURL = "https://login.microsoftonline.com/" + url.PathEscape(config.TenantID) +
			"/oauth2/v2.0/token"
	}
	if config.MetadataURL == "" {
		config.MetadataURL = connectorMetadata
	}
	plain := &http.Client{Timeout: 10 * time.Second}
	// The documentation shows the credentials in the form of the token request.
	credentials := clientcredentials.Config{ClientID: config.AppID,
		ClientSecret: config.AppSecret, TokenURL: config.TokenURL,
		Scopes:    []string{"https://api.botframework.com/.default"},
		AuthStyle: oauth2.AuthStyleInParams}
	// The request for the token uses the client of this context, which has a time limit.
	connector := credentials.Client(context.WithValue(context.Background(), oauth2.HTTPClient,
		plain))
	connector.Timeout = plain.Timeout
	t := &Teams{service: service, config: config, connector: connector,
		keys: &connectorKeys{metadataURL: config.MetadataURL, client: plain, now: time.Now}}
	service.register(t)
	return t, nil
}

// Name implements Platform.
func (*Teams) Name() string { return "teams" }

// Channel implements notify.Adapter.
func (*Teams) Channel() string { return "teams" }

// Targets implements notify.Adapter.
func (*Teams) Targets(string) []string {
	return []string{"Teams bot in the channel where the app is installed"}
}

// Send implements notify.Adapter. A request for approval gets the actions. Every other
// notification is the card that the webhook sends, posted by the bot.
func (t *Teams) Send(ctx context.Context, _ string, m notify.Message) error {
	if m.Event == "approval.requested" && m.WorkItem != "" {
		return t.service.announce(ctx, t, m)
	}
	_, err := t.start(ctx, notify.Teams{}.Payload(m)["attachments"])
	return err
}

// teamsCard draws a request as an Adaptive Card. An open request has its actions. A closed
// one has the outcome in their place. private is a text and a link for one person.
func teamsCard(request Request, outcome string, private Answer) map[string]any {
	text := func(value string, more map[string]any) map[string]any {
		block := map[string]any{"type": "TextBlock", "text": value, "wrap": true}
		for key, v := range more {
			block[key] = v
		}
		return block
	}
	body := []map[string]any{text(request.Title, map[string]any{"weight": "Bolder",
		"size": "Medium"})}
	if request.Sentence != "" {
		body = append(body, text(request.Sentence, nil))
	}
	facts := []map[string]any{}
	for _, f := range request.Facts {
		facts = append(facts, map[string]any{"title": f[0], "value": f[1]})
	}
	body = append(body, map[string]any{"type": "FactSet", "facts": facts})
	switch {
	case outcome != "":
		body = append(body, text(outcome, map[string]any{"weight": "Bolder"}))
	case !request.Decide:
		body = append(body, text("Decide on the page that shows the whole plan. A change of "+
			"this tier cannot be decided here.", nil))
	}
	if private.Text != "" {
		body = append(body, text(private.Text, map[string]any{"color": "Attention"}))
	}
	actions := cardActions(request, outcome == "" && request.Decide, private)
	return map[string]any{"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"type": "AdaptiveCard", "version": "1.4", "body": body, "actions": actions}
}

// cardActions returns the actions of a card: the two decisions when the request is open,
// the link to the page, and the link for one person.
func cardActions(request Request, open bool, private Answer) []map[string]any {
	actions := []map[string]any{}
	if open {
		data := map[string]any{"work_item": request.WorkItem, "binding": request.Binding}
		for _, verb := range []string{"approve", "reject"} {
			actions = append(actions, map[string]any{"type": "Action.Execute",
				"title": strings.ToUpper(verb[:1]) + verb[1:], "verb": verb, "data": data})
		}
	}
	actions = append(actions, map[string]any{"type": "Action.OpenUrl", "title": "Open in S.R.C.",
		"url": request.Link})
	if private.URL != "" && private.URL != request.Link {
		actions = append(actions, map[string]any{"type": "Action.OpenUrl",
			"title": private.URLTitle, "url": private.URL})
	}
	return actions
}

func cardAttachments(card map[string]any) []map[string]any {
	return []map[string]any{{"contentType": cardType, "content": card}}
}

// conversation is the channel where the bot posts, as the bot learned it when a person
// added the app to a team.
type conversation struct {
	ServiceURL string `json:"service_url"`
	Channel    string `json:"channel"`
}

func (t *Teams) conversation() (conversation, error) {
	raw, err := store.GetKV(t.service.db, conversationKey)
	if err != nil {
		return conversation{}, err
	}
	var c conversation
	if raw == "" || json.Unmarshal([]byte(raw), &c) != nil {
		return conversation{}, errors.New("the Teams bot does not know a channel yet: add the " +
			"app to a team in Microsoft Teams")
	}
	return c, nil
}

// call sends one request to the Bot Connector, with the token of the bot.
func (t *Teams) call(ctx context.Context, method, address string, body any) (map[string]string,
	error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode the request to the Bot Connector: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build the request to the Bot Connector: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := t.connector.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call the Bot Connector: %w. Check the app ID, the app secret "+
			"and the tenant ID of the Teams bot", err)
	}
	defer response.Body.Close()
	text, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("the Bot Connector answered %d: %s", response.StatusCode,
			strings.TrimSpace(string(text[:min(len(text), 300)])))
	}
	answer := map[string]string{}
	_ = json.Unmarshal(text, &answer) // an answer with no body is valid
	return answer, nil
}

// start posts a message with these attachments as a new thread in the channel of the bot.
//
// The form of this request for a channel comes from an archived page of the Microsoft
// documentation. It was not run against a real tenant:
// https://learn.microsoft.com/en-us/previous-versions/microsoftteams/platform/resources/bot-v3/bot-conversations/bots-conv-proactive
func (t *Teams) start(ctx context.Context, attachments any) (Posted, error) {
	where, err := t.conversation()
	if err != nil {
		return Posted{}, err
	}
	channelData := map[string]any{"channel": map[string]any{"id": where.Channel},
		"tenant": map[string]any{"id": t.config.TenantID}}
	answer, err := t.call(ctx, http.MethodPost, where.ServiceURL+"/v3/conversations",
		map[string]any{"isGroup": true, "tenantId": t.config.TenantID, "channelData": channelData,
			"activity": map[string]any{"type": "message", "attachments": attachments}})
	if err != nil {
		return Posted{}, err
	}
	if answer["id"] == "" || answer["activityId"] == "" {
		return Posted{}, errors.New("the Bot Connector did not name the new message")
	}
	return Posted{Place: answer["id"], Message: answer["activityId"]}, nil
}

// Post implements Platform.
func (t *Teams) Post(ctx context.Context, request Request) (Posted, error) {
	return t.start(ctx, cardAttachments(teamsCard(request, "", Answer{})))
}

// Close implements Platform. It replaces the card for everybody in the channel.
func (t *Teams) Close(ctx context.Context, posted Posted, request Request, outcome string) error {
	where, err := t.conversation()
	if err != nil {
		return err
	}
	_, err = t.call(ctx, http.MethodPut, where.ServiceURL+"/v3/conversations/"+
		url.PathEscape(posted.Place)+"/activities/"+url.PathEscape(posted.Message),
		map[string]any{"type": "message", "id": posted.Message,
			"attachments": cardAttachments(teamsCard(request, outcome, Answer{}))})
	return err
}

// teamsActivity is the part of an activity that the adapter reads.
type teamsActivity struct {
	Type       string `json:"type"`
	Name       string `json:"name"`
	Action     string `json:"action"`
	ChannelID  string `json:"channelId"`
	ServiceURL string `json:"serviceUrl"`
	From       struct {
		Name     string `json:"name"`
		ObjectID string `json:"aadObjectId"`
	} `json:"from"`
	Recipient struct {
		ID string `json:"id"`
	} `json:"recipient"`
	Conversation struct {
		ID       string `json:"id"`
		TenantID string `json:"tenantId"`
	} `json:"conversation"`
	MembersAdded []struct {
		ID string `json:"id"`
	} `json:"membersAdded"`
	ChannelData struct {
		Tenant struct {
			ID string `json:"id"`
		} `json:"tenant"`
	} `json:"channelData"`
	Value struct {
		Action struct {
			Verb string `json:"verb"`
			Data struct {
				WorkItem string `json:"work_item"`
				Binding  string `json:"binding"`
			} `json:"data"`
		} `json:"action"`
	} `json:"value"`
}

// tenant returns the tenant that the activity names, or nothing when it names none or two
// different ones.
func (a teamsActivity) tenant() string {
	if a.ChannelData.Tenant.ID == "" {
		return a.Conversation.TenantID
	}
	if a.Conversation.TenantID != "" && a.Conversation.TenantID != a.ChannelData.Tenant.ID {
		return ""
	}
	return a.ChannelData.Tenant.ID
}

// botAdded reports whether the activity says that the bot was added to a conversation. The
// app manifest allows the bot in a team only, so the conversation is a channel.
func (a teamsActivity) botAdded() bool {
	if a.Conversation.ID == "" {
		return false
	}
	if a.Type == "installationUpdate" {
		return a.Action == "add"
	}
	for _, member := range a.MembersAdded {
		if a.Type == "conversationUpdate" && member.ID == a.Recipient.ID {
			return true
		}
	}
	return false
}

// accepted reads an activity and checks that the Bot Connector sent it for this bot, from
// Teams, in the tenant of the bot. When it is not, accepted writes the answer and returns
// false.
func (t *Teams) accepted(w http.ResponseWriter, r *http.Request) (teamsActivity, bool) {
	var a teamsActivity
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil || r.Method != http.MethodPost || json.Unmarshal(body, &a) != nil {
		http.Error(w, "The request must be a POST of one activity.", http.StatusBadRequest)
		return a, false
	}
	// Nothing of the activity is used before the token is verified. The check needs the
	// channel and the serviceUrl of the activity to compare them with the token.
	if err := t.keys.verify(r.Context(), r.Header.Get("Authorization"), t.config.AppID,
		a); err != nil {
		http.Error(w, "The request has no valid token of the Bot Connector: "+err.Error(),
			http.StatusUnauthorized)
		return a, false
	}
	if a.ChannelID != teamsChannel || a.tenant() != t.config.TenantID {
		http.Error(w, "The activity is not from Microsoft Teams in the tenant of this bot.",
			http.StatusForbidden)
		return a, false
	}
	return a, true
}

// ServeHTTP is the messaging endpoint.
func (t *Teams) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a, ok := t.accepted(w, r)
	if !ok {
		return
	}
	switch {
	case a.botAdded():
		// In a team, the conversation of this activity is the channel that the person
		// chose. A reply has ";messageid=" after the channel.
		channel, _, _ := strings.Cut(a.Conversation.ID, ";")
		raw, _ := json.Marshal(conversation{ServiceURL: strings.TrimSuffix(a.ServiceURL, "/"),
			Channel: channel})
		if err := store.SetKV(t.service.db, conversationKey, string(raw)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	case a.Type == "invoke" && a.Name == "adaptiveCard/action":
		t.invoke(w, r, a)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

// click returns the decision that an invoke activity asks for, or false when the activity
// names no person, no change or no decision.
func (a teamsActivity) click() (Click, bool) {
	data, verb := a.Value.Action.Data, a.Value.Action.Verb
	click := Click{Account: Account{Platform: "teams", Workspace: a.tenant(), ID: a.From.ObjectID,
		Name: a.From.Name}, WorkItem: data.WorkItem, Binding: data.Binding, Decision: verb}
	known := verb == "approve" || verb == "reject"
	return click, known && a.From.ObjectID != "" && data.WorkItem != ""
}

// privateCard draws the card that the person who clicked sees in place of the request: the
// request again with the answer, and with no decision when the request is over.
func (t *Teams) privateCard(ctx context.Context, click Click, answer Answer) map[string]any {
	request, open, err := t.service.shown(ctx, "teams", click.WorkItem, click.Binding)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Error("chat approval: read the request of a card", "error", err)
		}
		request = Request{WorkItem: click.WorkItem, Title: "Request for approval of " +
			click.WorkItem, Link: t.service.workLink(click.WorkItem)}
	}
	outcome := ""
	switch {
	case answer.Decided:
		outcome, answer = answer.Text, Answer{}
	case err == nil && !open:
		outcome = "This request is over."
	}
	return teamsCard(request, outcome, answer)
}

// invoke handles a click on an action of a card. The answer is a card that only the person
// who clicked sees. The card for everybody changes in the sweep.
func (t *Teams) invoke(w http.ResponseWriter, r *http.Request, a teamsActivity) {
	answer := Answer{Text: "This click names no person, no change or no decision. Decide on " +
		"the dashboard."}
	click, ok := a.click()
	if ok {
		answer = t.service.Decide(r.Context(), click)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]any{"statusCode": http.StatusOK,
		"type": cardType, "value": t.privateCard(r.Context(), click, answer)}); err != nil {
		return
	}
	if answer.Decided {
		go t.service.Sweep(context.WithoutCancel(r.Context()))
	}
}
