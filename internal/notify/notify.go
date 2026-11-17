// Package notify routes audit events to email, Slack, Microsoft Teams and Discord. It reads
// the same event stream as the audit log. A message of this package links to the dashboard
// and carries no approve action. Package chatops has the adapters with an approve action.
package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/scrub"
)

// Message is the channel-neutral content of a notification. Every field comes from event
// facts, never from free model text.
type Message struct {
	Title    string
	Body     string
	Facts    [][2]string
	Link     string
	Urgency  string
	Severity string
	// Event is the type of the audit event, and WorkItem is its work item, if it has one.
	Event    string
	WorkItem string
	// Sentence is the sentence that says what changes, without the advice to open the
	// dashboard that Body ends with.
	Sentence string
}

// Adapter sends a message on one channel.
type Adapter interface {
	Channel() string
	// Targets lists where the adapter sends to, without secret parts.
	Targets(audience string) []string
	Send(ctx context.Context, audience string, m Message) error
}

// Route selects events and names the channels that receive them.
type Route struct {
	Name     string   `json:"name"`
	Types    []string `json:"types"`
	Tiers    []string `json:"tiers"`
	Envs     []string `json:"envs"`
	Channels []string `json:"channels"`
	Audience string   `json:"audience"`
	Urgency  string   `json:"urgency"`
}

// Rules is the content of notifications/routes.json in the runbooks repository.
type Rules struct {
	Routes []Route `json:"routes"`
}

// ParseRules decodes routing rules.
func ParseRules(raw []byte) (Rules, error) {
	var rules Rules
	if err := json.Unmarshal(raw, &rules); err != nil {
		return Rules{}, fmt.Errorf("decode notification routes: %w", err)
	}
	for _, r := range rules.Routes {
		if len(r.Types) == 0 || len(r.Channels) == 0 {
			return Rules{}, fmt.Errorf("notification route %q needs types and channels", r.Name)
		}
		if r.Urgency != "immediate" && r.Urgency != "digest" {
			return Rules{}, fmt.Errorf("notification route %q: urgency %q is not immediate or "+
				"digest", r.Name, r.Urgency)
		}
	}
	return rules, nil
}

func (r Route) matches(e audit.Event) bool {
	if !slices.Contains(r.Types, e.Type) {
		return false
	}
	if len(r.Envs) > 0 && !slices.Contains(r.Envs, e.Env) {
		return false
	}
	if len(r.Tiers) > 0 {
		tier, _ := e.Data["tier"].(string)
		if !slices.Contains(r.Tiers, tier) {
			return false
		}
	}
	return true
}

// Clock gives the virtual time.
type Clock interface{ Now() time.Time }

// Notifier consumes audit events and sends notifications.
type Notifier struct {
	db       *sql.DB
	log      *audit.Log
	clock    Clock
	baseURL  string
	adapters map[string]Adapter
	loadRule func() (Rules, error)

	mu     sync.Mutex
	digest []digestEntry
	sent   map[string]bool
}

type digestEntry struct {
	route Route
	event audit.Event
}

// New returns a notifier. loadRules reads the current routing rules, so that a merged change
// to the rules takes effect without a restart.
func New(db *sql.DB, log *audit.Log, clock Clock, baseURL string, adapters []Adapter,
	loadRules func() (Rules, error)) *Notifier {
	n := &Notifier{db: db, log: log, clock: clock, baseURL: baseURL,
		adapters: map[string]Adapter{}, loadRule: loadRules, sent: map[string]bool{}}
	for _, a := range adapters {
		n.adapters[a.Channel()] = a
	}
	return n
}

// Run consumes events until ctx ends.
func (n *Notifier) Run(ctx context.Context) {
	events, stop := n.log.Subscribe()
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-events:
			if strings.HasPrefix(e.Type, "notify.") {
				continue
			}
			n.handle(ctx, e)
		}
	}
}

func (n *Notifier) handle(ctx context.Context, e audit.Event) {
	rules, err := n.loadRule()
	if err != nil {
		n.audit("notify.failed", e, "rules", "", err.Error())
		return
	}
	for _, route := range rules.Routes {
		if !route.matches(e) {
			continue
		}
		if route.Urgency == "digest" {
			n.mu.Lock()
			n.digest = append(n.digest, digestEntry{route: route, event: e})
			n.mu.Unlock()
			continue
		}
		n.deliver(ctx, route, e.Seq, n.message(e, route))
	}
}

func (n *Notifier) link(workItem string) string {
	if workItem == "" {
		return n.baseURL + "/"
	}
	return n.baseURL + "/work/" + workItem
}

func (n *Notifier) message(e audit.Event, route Route) Message {
	m := Message{Title: scrub.Text(e.Summary), Link: n.link(e.WorkItem), Urgency: route.Urgency,
		Severity: severity(e.Type), Event: e.Type, WorkItem: e.WorkItem}
	add := func(label, value string) {
		if value != "" {
			m.Facts = append(m.Facts, [2]string{label, scrub.Text(value)})
		}
	}
	add("Work item", e.WorkItem)
	add("Environment", e.Env)
	add("Layer", e.Layer)
	for _, key := range []string{"tier", "rule", "decision", "identity", "expires", "reason"} {
		if v, ok := e.Data[key].(string); ok {
			add(strings.ToUpper(key[:1])+key[1:], v)
		}
	}
	if v, ok := e.Data["approvers_required"].(float64); ok {
		add("Approvers required", fmt.Sprintf("%d", int(v)))
	} else if v, ok := e.Data["approvers_required"].(int); ok {
		add("Approvers required", fmt.Sprintf("%d", v))
	}
	if sentence, ok := e.Data["sentence"].(string); ok {
		m.Body = scrub.Text(sentence)
		m.Sentence = m.Body
	}
	if e.Type == "approval.requested" {
		m.Body += " Open the dashboard and sign in to decide. You cannot approve from this message."
		m.Body = strings.TrimSpace(m.Body)
	}
	add("Time", e.TS)
	return m
}

func severity(eventType string) string {
	switch {
	case strings.HasSuffix(eventType, ".failed"), strings.HasSuffix(eventType, ".refused"),
		eventType == "incident.opened":
		return "error"
	case eventType == "approval.requested", strings.HasPrefix(eventType, "grant."),
		eventType == "finding.opened":
		return "warning"
	default:
		return "info"
	}
}

func (n *Notifier) deliver(ctx context.Context, route Route, seq int64, m Message) {
	for _, channel := range route.Channels {
		key := fmt.Sprintf("%d/%s/%s", seq, channel, route.Audience)
		n.mu.Lock()
		duplicate := n.sent[key]
		n.sent[key] = true
		n.mu.Unlock()
		if duplicate {
			continue
		}
		adapter, ok := n.adapters[channel]
		if !ok {
			n.record(seq, channel, "", "skipped", 0, "the channel is not configured", m.Title)
			continue
		}
		target := strings.Join(adapter.Targets(route.Audience), ", ")
		var err error
		attempts := 0
		for attempts < 3 {
			attempts++
			sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = adapter.Send(sendCtx, route.Audience, m)
			cancel()
			if err == nil {
				break
			}
			time.Sleep(time.Duration(attempts) * 300 * time.Millisecond)
		}
		status, errText := "sent", ""
		if err != nil {
			status, errText = "failed", scrub.Text(err.Error())
		}
		n.record(seq, channel, target, status, attempts, errText, m.Title)
	}
}

func (n *Notifier) record(seq int64, channel, target, status string, attempts int, errText,
	subject string) {
	at := n.clock.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if _, err := n.db.Exec(`INSERT INTO deliveries (event_seq, channel, target, status,
		attempts, error, subject, at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, seq, channel, target, status,
		attempts, errText, subject, at); err != nil {
		panic(fmt.Sprintf("delivery table is not writable, stopping: %v", err))
	}
	eventType := "notify." + status
	_, err := n.log.Append(audit.Event{Type: eventType, Actor: "notifier",
		Summary: fmt.Sprintf("%s to %s: %s", channel, target, subject),
		Data: map[string]any{"event_seq": seq, "channel": channel, "target": target,
			"attempts": attempts, "error": errText}})
	if err != nil {
		panic(fmt.Sprintf("audit log is not writable, stopping: %v", err))
	}
}

func (n *Notifier) audit(eventType string, e audit.Event, channel, target, errText string) {
	n.record(e.Seq, channel, target, strings.TrimPrefix(eventType, "notify."), 0, errText,
		e.Summary)
}

// FlushDigest sends one digest message for each route that collected events.
func (n *Notifier) FlushDigest(ctx context.Context) int {
	n.mu.Lock()
	pending := n.digest
	n.digest = nil
	n.mu.Unlock()
	byRoute := map[string][]digestEntry{}
	order := []string{}
	for _, entry := range pending {
		if _, seen := byRoute[entry.route.Name]; !seen {
			order = append(order, entry.route.Name)
		}
		byRoute[entry.route.Name] = append(byRoute[entry.route.Name], entry)
	}
	for _, name := range order {
		entries := byRoute[name]
		m := Message{Title: fmt.Sprintf("Digest: %s (%d events)", name, len(entries)),
			Link: n.link(""), Urgency: "digest", Severity: "info"}
		for _, entry := range entries {
			m.Facts = append(m.Facts, [2]string{entry.event.TS[:16],
				scrub.Text(entry.event.Summary)})
		}
		n.deliver(ctx, entries[0].route, entries[len(entries)-1].event.Seq, m)
	}
	return len(pending)
}

// Delivery is one row of the delivery history.
type Delivery struct {
	ID       int64  `json:"id"`
	EventSeq int64  `json:"event_seq"`
	Channel  string `json:"channel"`
	Target   string `json:"target"`
	Status   string `json:"status"`
	Attempts int    `json:"attempts"`
	Error    string `json:"error"`
	Subject  string `json:"subject"`
	At       string `json:"at"`
}

// Deliveries returns the newest deliveries.
func (n *Notifier) Deliveries(limit int) ([]Delivery, error) {
	rows, err := n.db.Query(`SELECT id, event_seq, channel, target, status, attempts, error,
		subject, at FROM deliveries ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()
	deliveries := []Delivery{}
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.EventSeq, &d.Channel, &d.Target, &d.Status, &d.Attempts,
			&d.Error, &d.Subject, &d.At); err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, rows.Err()
}

// Channels describes the configured adapters for the dashboard.
func (n *Notifier) Channels() []map[string]any {
	names := []string{"email", "slack", "teams", "discord"}
	channels := []map[string]any{}
	for _, name := range names {
		adapter, ok := n.adapters[name]
		entry := map[string]any{"channel": name, "configured": ok}
		if ok {
			entry["targets"] = adapter.Targets("approvers")
		}
		channels = append(channels, entry)
	}
	return channels
}
