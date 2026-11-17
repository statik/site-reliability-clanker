package notify_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/notify"
)

var sample = notify.Message{
	Title:    "Approval needed: Upgrade Paperboat to 1.1.0",
	Body:     "Open the dashboard and sign in to decide.",
	Facts:    [][2]string{{"Work item", "WI-0002"}, {"Tier", "T2"}},
	Link:     "http://127.0.0.1:7070/work/WI-0002",
	Urgency:  "immediate",
	Severity: "warning",
}

func encode(t *testing.T, payload any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestSlackPayloadIsBlockKitWithALinkAndNoApproveAction(t *testing.T) {
	payload := notify.Slack{}.Payload(sample)
	text := encode(t, payload)
	blocks, ok := payload["blocks"].([]map[string]any)
	if !ok || len(blocks) < 3 || blocks[0]["type"] != "header" {
		t.Fatalf("want Block Kit blocks that start with a header, got %s", text)
	}
	if !strings.Contains(text, sample.Link) || !strings.Contains(text, "WI-0002") {
		t.Errorf("the payload must carry the link and the facts: %s", text)
	}
	if strings.Contains(strings.ToLower(text), `"text":"approve"`) {
		t.Errorf("a chat message must not carry an approve action: %s", text)
	}
}

func TestTeamsPayloadIsAnAdaptiveCard(t *testing.T) {
	text := encode(t, notify.Teams{}.Payload(sample))
	for _, want := range []string{"application/vnd.microsoft.card.adaptive", "AdaptiveCard",
		"FactSet", "Action.OpenUrl", sample.Link} {
		if !strings.Contains(text, want) {
			t.Errorf("the Teams payload lacks %q: %s", want, text)
		}
	}
}

func TestDiscordPayloadIsAnEmbed(t *testing.T) {
	payload := notify.Discord{}.Payload(sample)
	embeds, ok := payload["embeds"].([]map[string]any)
	if !ok || len(embeds) != 1 {
		t.Fatalf("want one embed, got %v", payload)
	}
	if embeds[0]["url"] != sample.Link || embeds[0]["color"] != 0xf1c21b {
		t.Errorf("want the link and the warning colour, got %v", embeds[0])
	}
}

func TestEmailBodyHasPlainTextAndHTMLParts(t *testing.T) {
	email := notify.Email{From: "clanker@example.com"}
	body := string(email.Body([]string{"alice@example.com"}, sample))
	for _, want := range []string{"multipart/alternative", "text/plain", "text/html",
		"Subject: Approval needed", sample.Link, "Work item: WI-0002"} {
		if !strings.Contains(body, want) {
			t.Errorf("the email lacks %q", want)
		}
	}
}

func TestEmailEscapesHTML(t *testing.T) {
	m := sample
	m.Title = `<script>alert(1)</script>`
	body := string(notify.Email{From: "clanker@example.com"}.Body([]string{"a@example.com"}, m))
	_, htmlPart, _ := strings.Cut(body, "text/html")
	if strings.Contains(htmlPart, "<script>") {
		t.Error("the HTML part must escape markup from event text")
	}
}

func TestWebhookTargetsHideThePath(t *testing.T) {
	slack := notify.Slack{WebhookURL: "https://hooks.slack.com/services/T000/B000/SECRET"}
	target := strings.Join(slack.Targets("approvers"), " ")
	if strings.Contains(target, "SECRET") || !strings.Contains(target, "hooks.slack.com") {
		t.Errorf("the target must show the host only, got %q", target)
	}
}

func TestParseRules(t *testing.T) {
	good := `{"routes":[{"name":"a","types":["x"],"channels":["email"],"urgency":"immediate"}]}`
	if rules, err := notify.ParseRules([]byte(good)); err != nil || len(rules.Routes) != 1 {
		t.Fatalf("want one route, got %v, %v", rules, err)
	}
	for name, bad := range map[string]string{
		"not JSON":    `{`,
		"no channels": `{"routes":[{"name":"a","types":["x"],"urgency":"digest"}]}`,
		"no types":    `{"routes":[{"name":"a","channels":["email"],"urgency":"digest"}]}`,
		"bad urgency": `{"routes":[{"name":"a","types":["x"],"channels":["email"],"urgency":"now"}]}`,
	} {
		if _, err := notify.ParseRules([]byte(bad)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
