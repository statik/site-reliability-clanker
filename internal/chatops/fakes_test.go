package chatops

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/statik/site-reliability-clanker/internal/notify"
)

const (
	slackSecret = "test-signing-secret"
	slackToken  = "xoxb-test-token"
	teamsApp    = "11111111-2222-3333-4444-555555555555"
	teamsTenant = "99999999-8888-7777-6666-555555555555"
	teamsSecret = "test-app-secret"
	botToken    = "test-bot-token"
)

// fakeSlack is the Slack Web API and the response_url of Slack, and it sends clicks that
// are signed with the test signing secret.
type fakeSlack struct {
	t       *testing.T
	w       *world
	adapter *Slack
	calls   calls
	url     string
	posts   atomic.Int64
}

func newFakeSlack(t *testing.T, w *world, site *http.ServeMux) *fakeSlack {
	t.Helper()
	f := &fakeSlack{t: t, w: w}
	mux := http.NewServeMux()
	api := func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+slackToken {
			fmt.Fprint(rw, `{"ok":false,"error":"invalid_auth"}`)
			return
		}
		f.calls.add(r)
		fmt.Fprintf(rw, `{"ok":true,"channel":"C0CHANGES","ts":"1700000000.%06d"}`, f.posts.Add(1))
	}
	mux.HandleFunc("POST /api/chat.postMessage", api)
	mux.HandleFunc("POST /api/chat.update", api)
	mux.HandleFunc("POST /response", func(_ http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("the response_url got a token")
		}
		f.calls.add(r)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	f.url = server.URL
	adapter, err := NewSlack(w.service, SlackConfig{SigningSecret: slackSecret,
		BotToken: slackToken, Channel: "#changes", APIURL: server.URL + "/api"})
	if err != nil {
		t.Fatal(err)
	}
	f.adapter = adapter
	site.Handle(SlackPath, adapter)
	return f
}

func (f *fakeSlack) name() string { return "slack" }
func (f *fakeSlack) mark() string { return slackApprove }

func (f *fakeSlack) send(m notify.Message) error {
	return f.adapter.Send(context.Background(), "approvers", m)
}

// slackButtons returns the value of each button of a posted message, by action ID.
func slackButtons(message map[string]any) map[string]string {
	buttons := map[string]string{}
	blocks, _ := message["blocks"].([]any)
	for _, block := range blocks {
		elements, _ := block.(map[string]any)["elements"].([]any)
		for _, element := range elements {
			button, _ := element.(map[string]any)
			id, _ := button["action_id"].(string)
			buttons[id], _ = button["value"].(string)
		}
	}
	return buttons
}

// newest returns the buttons of the newest request for a work item.
func (f *fakeSlack) newest(id string) map[string]string {
	buttons := map[string]string{}
	for _, post := range f.calls.to("chat.postMessage") {
		if found := slackButtons(post.body); found["clanker_open"] == id {
			buttons = found
		}
	}
	return buttons
}

func (f *fakeSlack) offers(id string) bool { return f.newest(id)[slackApprove] != "" }

// payload is the body of a click on a button, as Slack sends it.
func (f *fakeSlack) payload(person, actionID, value string) string {
	raw, _ := json.Marshal(map[string]any{"type": "block_actions",
		"user":         map[string]any{"id": "U" + strings.ToUpper(person), "username": person},
		"team":         map[string]any{"id": "T0ACME", "domain": "acme"},
		"response_url": f.url + "/response",
		"actions":      []map[string]any{{"type": "button", "action_id": actionID, "value": value}}})
	return "payload=" + url.QueryEscape(string(raw))
}

func slackSignature(secret, stamp, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + stamp + ":" + body))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

// deliver posts a body to the interactivity endpoint with these headers.
func (f *fakeSlack) deliver(body string, headers map[string]string) int {
	request, err := http.NewRequest(http.MethodPost, f.w.url+SlackPath, strings.NewReader(body))
	if err != nil {
		f.t.Error(err)
		return 0
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		f.t.Error(err)
		return 0
	}
	response.Body.Close()
	return response.StatusCode
}

// signedHeaders returns the headers of a request that Slack signed now.
func signedHeaders(body string) map[string]string {
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	return map[string]string{"X-Slack-Request-Timestamp": stamp,
		"X-Slack-Signature": slackSignature(slackSecret, stamp, body)}
}

// clickBody is the body of a click on a decision of the newest request of a work item. A
// request with no such button gets a made-up value, as a forged click has.
func (f *fakeSlack) clickBody(person, id, decision string) string {
	actionID := map[string]string{"approve": slackApprove, "reject": slackReject}[decision]
	value := f.newest(id)[actionID]
	if value == "" {
		value = id + " made-up-binding"
	}
	return f.payload(person, actionID, value)
}

func (f *fakeSlack) fire(person, id, decision string) {
	body := f.clickBody(person, id, decision)
	if status := f.deliver(body, signedHeaders(body)); status != http.StatusOK {
		f.t.Errorf("slack: a signed click answered %d", status)
	}
}

func (f *fakeSlack) answers() []string {
	texts := []string{}
	for _, call := range f.calls.to("/response") {
		text, _ := call.body["text"].(string)
		texts = append(texts, text)
	}
	return texts
}

func (f *fakeSlack) closed() []string {
	texts := []string{}
	for _, call := range f.calls.to("chat.update") {
		texts = append(texts, call.text)
	}
	return texts
}

// fakeTeams is the token endpoint of Microsoft, the Bot Connector and its published keys,
// and it sends activities with tokens that are signed with the test key.
type fakeTeams struct {
	t       *testing.T
	w       *world
	adapter *Teams
	calls   calls
	url     string
	// key signs for Teams. otherChannelKey is published for another channel only.
	key             *rsa.PrivateKey
	otherChannelKey *rsa.PrivateKey
	posts           atomic.Int64
	cards           calls
}

func publishedKey(t *testing.T, key *rsa.PrivateKey, id, channel string) map[string]any {
	t.Helper()
	raw, err := json.Marshal(jose.JSONWebKey{Key: &key.PublicKey, KeyID: id, Algorithm: "RS256",
		Use: "sig"})
	if err != nil {
		t.Fatal(err)
	}
	published := map[string]any{}
	if err := json.Unmarshal(raw, &published); err != nil {
		t.Fatal(err)
	}
	published["endorsements"] = []string{channel}
	return published
}

func newFakeTeams(t *testing.T, w *world, site *http.ServeMux) *fakeTeams {
	t.Helper()
	f := &fakeTeams{t: t, w: w, key: testKey(t), otherChannelKey: testKey(t)}
	mux := http.NewServeMux()
	writeJSON := func(rw http.ResponseWriter, value any) {
		rw.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(rw).Encode(value); err != nil {
			t.Error(err)
		}
	}
	mux.HandleFunc("GET /meta", func(rw http.ResponseWriter, _ *http.Request) {
		writeJSON(rw, map[string]any{"issuer": connectorIssuer, "jwks_uri": f.url + "/keys"})
	})
	mux.HandleFunc("GET /keys", func(rw http.ResponseWriter, _ *http.Request) {
		writeJSON(rw, map[string]any{"keys": []any{publishedKey(t, f.key, "teams-key", "msteams"),
			publishedKey(t, f.otherChannelKey, "other-key", "webchat")}})
	})
	mux.HandleFunc("POST /token", func(rw http.ResponseWriter, r *http.Request) {
		want := url.Values{"grant_type": {"client_credentials"}, "client_id": {teamsApp},
			"client_secret": {teamsSecret}, "scope": {"https://api.botframework.com/.default"}}
		if err := r.ParseForm(); err != nil || r.PostForm.Encode() != want.Encode() {
			http.Error(rw, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}
		writeJSON(rw, map[string]any{"token_type": "Bearer", "expires_in": 3600,
			"access_token": botToken})
	})
	connector := func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+botToken {
			http.Error(rw, "no token of the bot", http.StatusUnauthorized)
			return
		}
		f.calls.add(r)
		n := f.posts.Add(1)
		writeJSON(rw, map[string]any{"id": fmt.Sprintf("19:changes@thread.tacv2;messageid=%d", n),
			"activityId": strconv.FormatInt(n, 10)})
	}
	mux.HandleFunc("POST /v3/conversations", connector)
	mux.HandleFunc("PUT /v3/conversations/{conversation}/activities/{activity}", connector)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	f.url = server.URL
	adapter, err := NewTeams(w.service, TeamsConfig{AppID: teamsApp, AppSecret: teamsSecret,
		TenantID: teamsTenant, TokenURL: server.URL + "/token", MetadataURL: server.URL + "/meta"})
	if err != nil {
		t.Fatal(err)
	}
	f.adapter = adapter
	site.Handle(TeamsPath, adapter)
	// A person adds the app to a team. The bot learns its channel from this activity.
	added := f.activity("alice", "conversationUpdate")
	added["membersAdded"] = []map[string]any{{"id": "28:" + teamsApp}}
	if status, body := f.deliver(added, f.token(f.key, "teams-key", nil)); status != http.StatusOK {
		t.Fatalf("the activity that adds the bot answered %d: %s", status, body)
	}
	return f
}

func (f *fakeTeams) name() string { return "teams" }
func (f *fakeTeams) mark() string { return "Action.Execute" }

func (f *fakeTeams) send(m notify.Message) error {
	return f.adapter.Send(context.Background(), "approvers", m)
}

// token makes a token of the Bot Connector, signed with key. change edits the claims of a
// valid token.
func (f *fakeTeams) token(key *rsa.PrivateKey, keyID string, change func(map[string]any)) string {
	f.t.Helper()
	now := time.Now()
	claims := map[string]any{"iss": connectorIssuer, "aud": teamsApp, "serviceurl": f.url,
		"nbf": now.Add(-time.Minute).Unix(), "exp": now.Add(10 * time.Minute).Unix()}
	if change != nil {
		change(claims)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithHeader("kid", keyID))
	if err != nil {
		f.t.Fatal(err)
	}
	raw, _ := json.Marshal(claims)
	signed, err := signer.Sign(raw)
	if err != nil {
		f.t.Fatal(err)
	}
	token, err := signed.CompactSerialize()
	if err != nil {
		f.t.Fatal(err)
	}
	return token
}

// activity is an activity from a person in the channel of the bot.
func (f *fakeTeams) activity(person, kind string) map[string]any {
	return map[string]any{"type": kind, "channelId": "msteams", "serviceUrl": f.url,
		"from": map[string]any{"id": "29:" + person, "name": person,
			"aadObjectId": "aad-" + person},
		"recipient": map[string]any{"id": "28:" + teamsApp},
		"conversation": map[string]any{"id": "19:changes@thread.tacv2", "tenantId": teamsTenant,
			"conversationType": "channel"},
		"channelData": map[string]any{"tenant": map[string]any{"id": teamsTenant}}}
}

// deliver posts an activity to the messaging endpoint with a bearer token. An empty token
// sends no Authorization header.
func (f *fakeTeams) deliver(activity map[string]any, token string) (int, string) {
	raw, _ := json.Marshal(activity)
	request, err := http.NewRequest(http.MethodPost, f.w.url+TeamsPath, bytes.NewReader(raw))
	if err != nil {
		f.t.Error(err)
		return 0, ""
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		f.t.Error(err)
		return 0, ""
	}
	defer response.Body.Close()
	var body bytes.Buffer
	if _, err := body.ReadFrom(response.Body); err != nil {
		f.t.Error(err)
	}
	return response.StatusCode, body.String()
}

// cardData returns the data of each Action.Execute of a posted card, by verb.
func cardData(message map[string]any) map[string]any {
	data := map[string]any{}
	activity, _ := message["activity"].(map[string]any)
	attachments, _ := activity["attachments"].([]any)
	for _, attachment := range attachments {
		content, _ := attachment.(map[string]any)["content"].(map[string]any)
		actions, _ := content["actions"].([]any)
		for _, action := range actions {
			a, _ := action.(map[string]any)
			if verb, ok := a["verb"].(string); ok {
				data[verb] = a["data"]
			}
		}
	}
	return data
}

// newest returns the action data of the newest request for a work item.
func (f *fakeTeams) newest(id string) map[string]any {
	data := map[string]any{}
	for _, post := range f.calls.to("/v3/conversations") {
		if strings.Contains(post.text, "/work/"+id+`"`) {
			data = cardData(post.body)
		}
	}
	return data
}

func (f *fakeTeams) offers(id string) bool { return f.newest(id)["approve"] != nil }

// clickActivity is the invoke activity of a click on a decision of the newest request of a
// work item. A request with no such action gets made-up data, as a forged click has.
func (f *fakeTeams) clickActivity(person, id, decision string) map[string]any {
	data := f.newest(id)[decision]
	if data == nil {
		data = map[string]any{"work_item": id, "binding": "made-up-binding"}
	}
	invoke := f.activity(person, "invoke")
	invoke["name"] = "adaptiveCard/action"
	invoke["value"] = map[string]any{"trigger": "manual", "action": map[string]any{
		"type": "Action.Execute", "verb": decision, "data": data}}
	return invoke
}

func (f *fakeTeams) fire(person, id, decision string) {
	status, body := f.deliver(f.clickActivity(person, id, decision), f.token(f.key, "teams-key", nil))
	if status != http.StatusOK {
		f.t.Errorf("teams: a click with a valid token answered %d: %s", status, body)
		return
	}
	f.cards.mu.Lock()
	defer f.cards.mu.Unlock()
	f.cards.list = append(f.cards.list, recorded{text: body})
}

func (f *fakeTeams) answers() []string {
	f.cards.mu.Lock()
	defer f.cards.mu.Unlock()
	texts := []string{}
	for _, card := range f.cards.list {
		texts = append(texts, card.text)
	}
	return texts
}

func (f *fakeTeams) closed() []string {
	texts := []string{}
	for _, call := range f.calls.to("") {
		if call.method == http.MethodPut {
			texts = append(texts, call.text)
		}
	}
	return texts
}
