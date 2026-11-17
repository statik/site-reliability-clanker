package chatops

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	"github.com/statik/site-reliability-clanker/internal/notify"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// A posted request stays open until its work item no longer waits for approval, or until
// the plan changes. Then the message is changed: it shows the outcome and has no action, so
// that nobody clicks on a decided change. The state in the database decides this, not the
// content of an event, because a subscriber of the audit log can miss an event.

// register makes a platform known to the shared part.
func (s *Service) register(p Platform) { s.platforms[p.Name()] = p }

// announce posts the request for approval of a work item on one platform and remembers
// where it is. m is the notification of the event approval.requested.
func (s *Service) announce(ctx context.Context, p Platform, m notify.Message) error {
	wi, err := s.orch.Get(m.WorkItem)
	if err != nil {
		return err
	}
	binding, err := s.orch.Binding(wi)
	if err != nil {
		return err
	}
	request := Request{WorkItem: wi.ID, Binding: binding, Title: m.Title, Sentence: m.Sentence,
		Facts: m.Facts, Link: s.workLink(wi.ID), Decide: slices.Contains(chatTiers, wi.Tier)}
	content, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode the request for approval of %s: %w", wi.ID, err)
	}
	posted, err := p.Post(ctx, request)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO chat_messages (platform, work_item, binding,
		place, message, content, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`, p.Name(), wi.ID,
		binding, posted.Place, posted.Message, string(content),
		store.FormatTime(s.now())); err != nil {
		return fmt.Errorf("remember the request for approval of %s in %s: %w", wi.ID,
			labels[p.Name()], err)
	}
	// Somebody can decide on the dashboard while the message is on its way.
	s.Sweep(ctx)
	return nil
}

// openRequest is a posted request that still has its actions.
type openRequest struct {
	id       int64
	platform string
	posted   Posted
	request  Request
}

func (s *Service) openRequests(ctx context.Context) ([]openRequest, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, platform, place, message, content
		FROM chat_messages WHERE open = 1 ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list the open requests in chat: %w", err)
	}
	defer rows.Close()
	open := []openRequest{}
	for rows.Next() {
		var o openRequest
		var content string
		if err := rows.Scan(&o.id, &o.platform, &o.posted.Place, &o.posted.Message,
			&content); err != nil {
			return nil, fmt.Errorf("read an open request in chat: %w", err)
		}
		if err := json.Unmarshal([]byte(content), &o.request); err != nil {
			return nil, fmt.Errorf("decode the open request %d in chat: %w", o.id, err)
		}
		open = append(open, o)
	}
	return open, rows.Err()
}

var decisionWords = map[string]string{"approve": "Approved", "reject": "Rejected",
	"request_changes": "Changes requested"}

// outcome returns the text that replaces the actions of a request, or nothing while the
// request is still the one to decide on.
func (s *Service) outcome(wi orchestrator.WorkItem, binding string) (string, error) {
	if wi.State == orchestrator.StateAwaitingApproval {
		current, err := s.orch.Binding(wi)
		if err != nil || current == binding {
			return "", err
		}
		return "The plan changed. A new request for approval replaces this one.", nil
	}
	approvals, err := s.orch.Approvals(wi.ID)
	if err != nil {
		return "", err
	}
	// The newest decision on the plan that this request shows is the outcome.
	for _, a := range slices.Backward(approvals) {
		if orchestrator.Binding(a.Commit, a.PlanHashes) != binding {
			continue
		}
		name := a.Approver
		if user, err := s.orch.GetUser(a.Approver); err == nil {
			name = user.Display
		}
		return fmt.Sprintf("%s by %s at %s.", decisionWords[a.Decision], name, formatTime(a.At)), nil
	}
	return "This change does not wait for approval any more. Its state is " + wi.State + ".", nil
}

// closeIfOver changes the message of one request when the request is over.
func (s *Service) closeIfOver(ctx context.Context, o openRequest) error {
	wi, err := s.orch.Get(o.request.WorkItem)
	if err != nil {
		return err
	}
	outcome, err := s.outcome(wi, o.request.Binding)
	if err != nil || outcome == "" {
		return err
	}
	platform, ok := s.platforms[o.platform]
	if !ok {
		// The app of this platform is not configured any more. Nothing can change the message.
		return nil
	}
	if err := platform.Close(ctx, o.posted, o.request, outcome); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE chat_messages SET open = 0 WHERE id = $1`,
		o.id); err != nil {
		return fmt.Errorf("remember that the request %d in chat is closed: %w", o.id, err)
	}
	return nil
}

// Sweep changes the message of each posted request that is over. A message that the
// platform does not take stays open, and the next sweep tries again.
func (s *Service) Sweep(ctx context.Context) {
	s.sweeping.Lock()
	defer s.sweeping.Unlock()
	open, err := s.openRequests(ctx)
	if err != nil {
		slog.Error("chat approval", "error", err)
		return
	}
	for _, o := range open {
		if err := s.closeIfOver(ctx, o); err != nil {
			slog.Warn("chat approval: a request in chat was not changed", "work_item",
				o.request.WorkItem, "platform", o.platform, "error", err)
		}
	}
}

// Run sweeps at start and after each event that can end a request, until ctx ends.
func (s *Service) Run(ctx context.Context) {
	events, stop := s.audit.Subscribe()
	defer stop()
	s.Sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-events:
			if e.Type == "workitem.state_changed" || e.Type == "approval.requested" {
				s.Sweep(ctx)
			}
		}
	}
}

// shown returns the request that a message on a platform shows, and whether it is still
// open. The Teams adapter needs it to draw the card again for the person who clicked.
func (s *Service) shown(ctx context.Context, platform, workItem, binding string) (Request, bool,
	error) {
	var content string
	var open int
	err := s.db.QueryRowContext(ctx, `SELECT content, open FROM chat_messages WHERE platform = $1
		AND work_item = $2 AND binding = $3 ORDER BY id DESC LIMIT 1`, platform, workItem,
		binding).Scan(&content, &open)
	if err != nil {
		return Request{}, false, err
	}
	var request Request
	if err := json.Unmarshal([]byte(content), &request); err != nil {
		return Request{}, false, fmt.Errorf("decode the request of %s in chat: %w", workItem, err)
	}
	return request, open == 1, nil
}
