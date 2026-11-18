package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
)

func postJSON(ctx context.Context, endpoint string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode webhook payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "clanker-notifier")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("post to webhook host %s: %w", hostOf(endpoint), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		text, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("webhook host %s answered %d: %s", hostOf(endpoint), resp.StatusCode,
			strings.TrimSpace(string(text)))
	}
	return nil
}

// hostOf returns the host of a webhook URL. The path of a webhook URL is a secret.
func hostOf(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "(invalid URL)"
	}
	return u.Host
}

func severityColor(severity string) (hex string, decimal int) {
	switch severity {
	case "error":
		return "#da1e28", 0xda1e28
	case "warning":
		return "#f1c21b", 0xf1c21b
	default:
		return "#0f62fe", 0x0f62fe
	}
}

// Slack sends Block Kit messages to an incoming webhook.
type Slack struct{ WebhookURL string }

// Channel implements Adapter.
func (Slack) Channel() string { return "slack" }

// Targets implements Adapter.
func (s Slack) Targets(string) []string { return []string{"webhook at " + hostOf(s.WebhookURL)} }

// Payload builds the Block Kit body.
func (Slack) Payload(m Message) map[string]any {
	fields := []map[string]any{}
	for _, f := range m.Facts {
		fields = append(fields, map[string]any{"type": "mrkdwn",
			"text": fmt.Sprintf("*%s*\n%s", f[0], f[1])})
	}
	blocks := []map[string]any{
		{"type": "header", "text": map[string]any{"type": "plain_text", "text": truncate(m.Title, 150)}},
	}
	if m.Body != "" {
		blocks = append(blocks, map[string]any{"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": m.Body}})
	}
	for start := 0; start < len(fields); start += 10 {
		end := min(start+10, len(fields))
		blocks = append(blocks, map[string]any{"type": "section", "fields": fields[start:end]})
	}
	blocks = append(blocks, map[string]any{"type": "actions", "elements": []map[string]any{{
		"type": "button", "text": map[string]any{"type": "plain_text", "text": "Open in S.R.C."},
		"url": m.Link}}})
	return map[string]any{"text": m.Title, "blocks": blocks}
}

// Send implements Adapter.
func (s Slack) Send(ctx context.Context, _ string, m Message) error {
	return postJSON(ctx, s.WebhookURL, s.Payload(m))
}

// Teams sends Adaptive Cards to a Workflows webhook.
type Teams struct{ WebhookURL string }

// Channel implements Adapter.
func (Teams) Channel() string { return "teams" }

// Targets implements Adapter.
func (t Teams) Targets(string) []string { return []string{"webhook at " + hostOf(t.WebhookURL)} }

// Payload builds the Adaptive Card body.
func (Teams) Payload(m Message) map[string]any {
	facts := []map[string]any{}
	for _, f := range m.Facts {
		facts = append(facts, map[string]any{"title": f[0], "value": f[1]})
	}
	style := map[string]string{"error": "attention", "warning": "warning", "info": "accent"}
	body := []map[string]any{{"type": "TextBlock", "text": m.Title, "weight": "Bolder",
		"size": "Medium", "wrap": true, "color": style[m.Severity]}}
	if m.Body != "" {
		body = append(body, map[string]any{"type": "TextBlock", "text": m.Body, "wrap": true})
	}
	body = append(body, map[string]any{"type": "FactSet", "facts": facts})
	card := map[string]any{
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":    "AdaptiveCard", "version": "1.4", "body": body,
		"actions": []map[string]any{{"type": "Action.OpenUrl", "title": "Open in S.R.C.",
			"url": m.Link}},
	}
	return map[string]any{"type": "message", "attachments": []map[string]any{{
		"contentType": "application/vnd.microsoft.card.adaptive", "content": card}}}
}

// Send implements Adapter.
func (t Teams) Send(ctx context.Context, _ string, m Message) error {
	return postJSON(ctx, t.WebhookURL, t.Payload(m))
}

// Discord sends embeds to a channel webhook.
type Discord struct{ WebhookURL string }

// Channel implements Adapter.
func (Discord) Channel() string { return "discord" }

// Targets implements Adapter.
func (d Discord) Targets(string) []string {
	return []string{"webhook at " + hostOf(d.WebhookURL)}
}

// Payload builds the embed body.
func (Discord) Payload(m Message) map[string]any {
	fields := []map[string]any{}
	for i, f := range m.Facts {
		if i == 25 {
			break
		}
		fields = append(fields, map[string]any{"name": f[0], "value": truncate(f[1], 1000),
			"inline": len(f[1]) < 40})
	}
	_, color := severityColor(m.Severity)
	return map[string]any{"username": "S.R.C.", "embeds": []map[string]any{{
		"title": truncate(m.Title, 250), "description": m.Body, "url": m.Link, "color": color,
		"fields": fields}}}
}

// Send implements Adapter.
func (d Discord) Send(ctx context.Context, _ string, m Message) error {
	return postJSON(ctx, d.WebhookURL, d.Payload(m))
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit-3] + "..."
}

// Email sends multipart mail over SMTP.
type Email struct {
	Addr string
	From string
	// Recipients returns the addresses of an audience, for example "approvers".
	Recipients func(audience string) []string
}

// Channel implements Adapter.
func (Email) Channel() string { return "email" }

// Targets implements Adapter.
func (e Email) Targets(audience string) []string { return e.Recipients(audience) }

// Body builds the MIME message with a plain-text part and an HTML part.
func (e Email) Body(to []string, m Message) []byte {
	const boundary = "clanker-boundary-7f3a"
	accent, _ := severityColor(m.Severity)
	var text, rows strings.Builder
	text.WriteString(m.Title + "\n\n")
	if m.Body != "" {
		text.WriteString(m.Body + "\n\n")
	}
	for _, f := range m.Facts {
		fmt.Fprintf(&text, "%s: %s\n", f[0], f[1])
		fmt.Fprintf(&rows, `<tr><td style="padding:6px 16px 6px 0;color:#525252">%s</td>`+
			`<td style="padding:6px 0;color:#161616">%s</td></tr>`, html.EscapeString(f[0]),
			html.EscapeString(f[1]))
	}
	text.WriteString("\nOpen in S.R.C.: " + m.Link + "\n")
	htmlBody := fmt.Sprintf(`<div style="font-family:'IBM Plex Sans',Helvetica,Arial,sans-serif;`+
		`max-width:640px;border-top:4px solid %s;background:#f4f4f4;padding:24px">`+
		`<h2 style="font-weight:400;color:#161616;margin:0 0 12px">%s</h2>`+
		`<p style="color:#161616;line-height:1.5">%s</p>`+
		`<table style="border-collapse:collapse;font-size:14px;margin:16px 0">%s</table>`+
		`<a href="%s" style="display:inline-block;background:#0f62fe;color:#ffffff;`+
		`padding:12px 16px;text-decoration:none">Open in S.R.C.</a></div>`,
		accent, html.EscapeString(m.Title), html.EscapeString(m.Body), rows.String(),
		html.EscapeString(m.Link))
	var msg strings.Builder
	fmt.Fprintf(&msg, "From: S.R.C. <%s>\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n"+
		"Content-Type: multipart/alternative; boundary=%s\r\n\r\n", e.From,
		strings.Join(to, ", "), truncate(m.Title, 200), boundary)
	fmt.Fprintf(&msg, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", boundary,
		text.String())
	fmt.Fprintf(&msg, "--%s\r\nContent-Type: text/html; charset=utf-8\r\n\r\n%s\r\n--%s--\r\n",
		boundary, htmlBody, boundary)
	return []byte(msg.String())
}

// Send implements Adapter.
func (e Email) Send(_ context.Context, audience string, m Message) error {
	to := e.Recipients(audience)
	if len(to) == 0 {
		return fmt.Errorf("send email: audience %q has no recipients", audience)
	}
	if err := smtp.SendMail(e.Addr, nil, e.From, to, e.Body(to, m)); err != nil {
		return fmt.Errorf("send email through %s: %w", e.Addr, err)
	}
	return nil
}
