package chatops

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/notify"
)

// The Slack adapter is a Slack app: it posts with the Web API and gets each click on its
// interactivity endpoint. It was tested only against a fake Slack.
//
// Protocol, as the Slack documentation describes it:
//   - Signature of a request: https://docs.slack.dev/authentication/verifying-requests-from-slack
//   - Click on a button (block_actions), the answer in 3 seconds and response_url:
//     https://docs.slack.dev/interactivity/handling-user-interaction
//   - https://docs.slack.dev/reference/methods/chat.postMessage and chat.update

const (
	// SlackPath is the interactivity endpoint. The gate lets it through with no sign-in: the
	// handler accepts only a request that Slack signed.
	SlackPath = "/chat/slack/interactions"
	// slackWindow is how far the timestamp of a request can be from the time of this
	// machine. An older request can be a replay.
	slackWindow = 5 * time.Minute
	// Slack limits the text of a header block to 150 characters and of a section to 3000.
	slackHeaderLimit  = 150
	slackSectionLimit = 3000

	slackApprove = "clanker_approve"
	slackReject  = "clanker_reject"
)

// SlackConfig is the registration of the Slack app.
type SlackConfig struct {
	// SigningSecret verifies that a request comes from Slack. BotToken posts messages.
	// Both are secrets.
	SigningSecret string
	BotToken      string
	// Channel is the ID or the name of the channel for the messages.
	Channel string
	// APIURL is the address of the Slack Web API. Empty selects the real one.
	APIURL string
}

// Slack is the adapter for Slack. It is a notify.Adapter for the channel "slack" and the
// handler of the interactivity endpoint.
type Slack struct {
	service *Service
	config  SlackConfig
	client  *http.Client
	now     func() time.Time
}

// NewSlack returns the adapter of a Slack app and makes it known to the shared part.
func NewSlack(service *Service, config SlackConfig) (*Slack, error) {
	if config.SigningSecret == "" || config.BotToken == "" || config.Channel == "" {
		return nil, errors.New("the Slack app needs the signing secret, the bot token and the " +
			"channel")
	}
	if config.APIURL == "" {
		config.APIURL = "https://slack.com/api"
	}
	s := &Slack{service: service, config: config, now: time.Now,
		client: &http.Client{Timeout: 10 * time.Second}}
	service.register(s)
	return s, nil
}

// Name implements Platform.
func (*Slack) Name() string { return "slack" }

// Channel implements notify.Adapter.
func (*Slack) Channel() string { return "slack" }

// Targets implements notify.Adapter.
func (s *Slack) Targets(string) []string {
	return []string{"Slack app in channel " + s.config.Channel}
}

// Send implements notify.Adapter. A request for approval gets the actions. Every other
// notification is the message that the webhook sends, posted by the app.
func (s *Slack) Send(ctx context.Context, _ string, m notify.Message) error {
	if m.Event == "approval.requested" && m.WorkItem != "" {
		return s.service.announce(ctx, s, m)
	}
	payload := notify.Slack{}.Payload(m)
	payload["channel"] = s.config.Channel
	_, err := s.call(ctx, "chat.postMessage", payload)
	return err
}

// slackEscape makes text safe for a mrkdwn field, where & < > have a meaning.
func slackEscape(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}

func slackButton(text, actionID, style, value string) map[string]any {
	button := map[string]any{"type": "button", "action_id": actionID, "value": value,
		"text": map[string]any{"type": "plain_text", "text": text}}
	if style != "" {
		button["style"] = style
	}
	return button
}

func slackSection(text string) map[string]any {
	if len(text) > slackSectionLimit {
		text = text[:slackSectionLimit-3] + "..."
	}
	return map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": text}}
}

// slackBlocks draws a request. An open request has its actions. A closed one has the
// outcome in their place.
func slackBlocks(request Request, outcome string) []map[string]any {
	title := request.Title
	if len(title) > slackHeaderLimit {
		title = title[:slackHeaderLimit-3] + "..."
	}
	blocks := []map[string]any{{"type": "header",
		"text": map[string]any{"type": "plain_text", "text": title}}}
	if request.Sentence != "" {
		blocks = append(blocks, slackSection(slackEscape(request.Sentence)))
	}
	fields := []map[string]any{}
	for _, f := range request.Facts {
		fields = append(fields, map[string]any{"type": "mrkdwn",
			"text": fmt.Sprintf("*%s*\n%s", slackEscape(f[0]), slackEscape(f[1]))})
	}
	// A section holds at most ten fields.
	for start := 0; start < len(fields); start += 10 {
		blocks = append(blocks, map[string]any{"type": "section",
			"fields": fields[start:min(start+10, len(fields))]})
	}
	elements := []map[string]any{}
	switch {
	case outcome != "":
		blocks = append(blocks, slackSection("*"+slackEscape(outcome)+"*"))
	case request.Decide:
		value := request.WorkItem + " " + request.Binding
		elements = append(elements, slackButton("Approve", slackApprove, "primary", value),
			slackButton("Reject", slackReject, "danger", value))
	default:
		blocks = append(blocks, slackSection("Decide on the page that shows the whole plan. "+
			"A change of this tier cannot be decided here."))
	}
	open := slackButton("Open in Clanker", "clanker_open", "", request.WorkItem)
	open["url"] = request.Link
	return append(blocks, map[string]any{"type": "actions", "elements": append(elements, open)})
}

// call runs one method of the Web API. Slack answers 200 also for a refusal, with ok false
// and the reason in error.
func (s *Slack) call(ctx context.Context, method string, payload map[string]any) (Posted, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Posted{}, fmt.Errorf("encode the Slack request %s: %w", method, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.APIURL+"/"+method,
		bytes.NewReader(body))
	if err != nil {
		return Posted{}, fmt.Errorf("build the Slack request %s: %w", method, err)
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Authorization", "Bearer "+s.config.BotToken)
	response, err := s.client.Do(request)
	if err != nil {
		return Posted{}, fmt.Errorf("call Slack %s: %w", method, err)
	}
	defer response.Body.Close()
	var answer struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	}
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err := json.Unmarshal(raw, &answer); err != nil {
		return Posted{}, fmt.Errorf("Slack %s answered %d and no JSON", method, response.StatusCode)
	}
	if !answer.OK {
		return Posted{}, fmt.Errorf("Slack refused %s: %s. Check the bot token, the scope "+
			"chat:write and that the app is a member of the channel", method, answer.Error)
	}
	return Posted{Place: answer.Channel, Message: answer.TS}, nil
}

// Post implements Platform.
func (s *Slack) Post(ctx context.Context, request Request) (Posted, error) {
	return s.call(ctx, "chat.postMessage", map[string]any{"channel": s.config.Channel,
		"text": request.Title, "blocks": slackBlocks(request, "")})
}

// Close implements Platform.
func (s *Slack) Close(ctx context.Context, posted Posted, request Request, outcome string) error {
	_, err := s.call(ctx, "chat.update", map[string]any{"channel": posted.Place,
		"ts": posted.Message, "text": request.Title + ": " + outcome,
		"blocks": slackBlocks(request, outcome)})
	return err
}

// signed reports whether Slack signed this body with the signing secret, a short time ago.
// The signature covers the version, the timestamp and the raw body.
func (s *Slack) signed(header http.Header, body []byte) bool {
	stamp := header.Get("X-Slack-Request-Timestamp")
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return false
	}
	if age := s.now().Sub(time.Unix(seconds, 0)); age > slackWindow || age < -slackWindow {
		return false
	}
	mac := hmac.New(sha256.New, []byte(s.config.SigningSecret))
	mac.Write([]byte("v0:" + stamp + ":"))
	mac.Write(body)
	want := "v0=" + hex.EncodeToString(mac.Sum(nil))
	// hmac.Equal takes the same time for every wrong signature.
	return hmac.Equal([]byte(want), []byte(header.Get("X-Slack-Signature")))
}

// slackClick is the part of a block_actions payload that the adapter reads.
type slackClick struct {
	Type string `json:"type"`
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
	Team struct {
		ID     string `json:"id"`
		Domain string `json:"domain"`
	} `json:"team"`
	ResponseURL string `json:"response_url"`
	Actions     []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
}

// click returns the decision that a payload asks for, or false when the payload is not a
// click on Approve or Reject. A click on the link button also comes here and asks nothing.
func (p slackClick) click() (Click, bool) {
	if p.Type != "block_actions" || len(p.Actions) != 1 {
		return Click{}, false
	}
	action := p.Actions[0]
	decision := map[string]string{slackApprove: "approve", slackReject: "reject"}[action.ActionID]
	workItem, binding, found := strings.Cut(action.Value, " ")
	if decision == "" || !found {
		return Click{}, false
	}
	name := p.User.Username
	if p.Team.Domain != "" {
		name += " in " + p.Team.Domain
	}
	return Click{Account: Account{Platform: "slack", Workspace: p.Team.ID, ID: p.User.ID,
		Name: name}, WorkItem: workItem, Binding: binding, Decision: decision}, true
}

// ServeHTTP is the interactivity endpoint. It reads the raw body before anything else,
// because the signature covers the raw body.
func (s *Slack) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil || r.Method != http.MethodPost {
		http.Error(w, "The request must be a POST of at most 1 MB.", http.StatusBadRequest)
		return
	}
	if !s.signed(r.Header, body) {
		http.Error(w, "The request has no valid signature of Slack.", http.StatusUnauthorized)
		return
	}
	form, err := url.ParseQuery(string(body))
	var payload slackClick
	if err == nil {
		err = json.Unmarshal([]byte(form.Get("payload")), &payload)
	}
	if err != nil {
		http.Error(w, "The request is not an interaction payload.", http.StatusBadRequest)
		return
	}
	// Slack wants the answer in 3 seconds. The decision runs after the answer, so that a
	// slow database does not make the click fail in Slack while the decision is recorded.
	w.WriteHeader(http.StatusOK)
	if click, ok := payload.click(); ok {
		go s.decide(context.WithoutCancel(r.Context()), click, payload.ResponseURL)
	}
}

// decide runs a click and tells the person the result in a message that only that person
// sees.
func (s *Slack) decide(ctx context.Context, click Click, responseURL string) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	answer := Answer{Text: "This app is installed for a whole organization, which the control " +
		"plane does not support: the request names no workspace. Decide on the dashboard.",
		URL: s.service.workLink(click.WorkItem), URLTitle: "Open in Clanker"}
	if click.Account.Workspace != "" && click.Account.ID != "" {
		answer = s.service.Decide(ctx, click)
	}
	if err := s.whisper(ctx, responseURL, answer); err != nil {
		slog.Warn("chat approval: the private answer in Slack was not sent", "error", err)
	}
	if answer.Decided {
		s.service.Sweep(ctx)
	}
}

// whisper sends the answer to the response_url of the click, as a message that only the
// person who clicked sees and that leaves the request in place. The address comes from the
// signed payload. It gets no token.
func (s *Slack) whisper(ctx context.Context, responseURL string, answer Answer) error {
	text := slackEscape(answer.Text)
	if answer.URL != "" {
		text += " <" + answer.URL + "|" + slackEscape(answer.URLTitle) + ">"
	}
	body, err := json.Marshal(map[string]any{"response_type": "ephemeral",
		"replace_original": false, "text": text})
	if err != nil {
		return fmt.Errorf("encode the answer: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, responseURL,
		bytes.NewReader(body))
	if err != nil {
		return errors.New("the response_url of the click is not a URL")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		// The error of the client holds the address, which works as a credential for a while.
		return errors.New("the request to the response_url failed")
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return fmt.Errorf("the response_url answered %d", response.StatusCode)
	}
	return nil
}
