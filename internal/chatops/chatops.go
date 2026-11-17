// Package chatops lets a person approve or reject a change inside Slack or Microsoft Teams.
//
// A chat account is linked once to a person through the sign-in of the control plane. After
// that, each click is verified with the signature of the chat platform and mapped to the
// user with the email address of that person. The decision goes through
// orchestrator.Decide, the same code as the dashboard, and the audit event of the decision
// records the chat account next to the user.
//
// A change of tier T2 or lower can be decided in chat. A change of tier T3 shows its
// summary and a link, and is decided on the page that shows the whole plan.
//
// The adapters were tested only with fakes and test keys. Nothing here ran against the real
// Slack or the real Microsoft Teams.
package chatops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// chatTiers are the tiers of a change that a person can decide in chat. A tier that is not
// in this list is decided on the dashboard.
var chatTiers = []string{"T0", "T1", "T2"}

// labels are the names of the platforms as a person reads them.
var labels = map[string]string{"slack": "Slack", "teams": "Microsoft Teams"}

// Account is an account on a chat platform, as the platform names it in a signed request.
type Account struct {
	// Platform is "slack" or "teams".
	Platform string
	// Workspace is the ID of the Slack workspace or of the Microsoft Entra tenant.
	Workspace string
	// ID is the ID of the account in that workspace or tenant.
	ID string
	// Name is the name that the platform shows for the account. It is for the eyes of a
	// person only. Nothing is decided by it.
	Name string
}

func (a Account) String() string {
	return fmt.Sprintf("%s account %s (%s) in %s", labels[a.Platform], a.Name, a.ID, a.Workspace)
}

func (a Account) origin() map[string]string {
	return map[string]string{"platform": a.Platform, "workspace": a.Workspace, "account": a.ID,
		"account_name": a.Name}
}

// Request is the content of a request for approval in a chat.
type Request struct {
	WorkItem string `json:"work_item"`
	// Binding is the binding of the commit and the plans that the request shows.
	Binding  string      `json:"binding"`
	Title    string      `json:"title"`
	Sentence string      `json:"sentence"`
	Facts    [][2]string `json:"facts"`
	// Link is the address of the page of the work item.
	Link string `json:"link"`
	// Decide is set when the tier allows a decision in chat.
	Decide bool `json:"decide"`
}

// Posted says where a request is on a platform, so that it can be changed later.
type Posted struct {
	// Place is the Slack channel or the Teams conversation.
	Place string
	// Message is the Slack timestamp or the Teams activity ID of the message.
	Message string
}

// Platform is what the shared part needs from the adapter of one chat platform.
type Platform interface {
	// Name is "slack" or "teams".
	Name() string
	// Post sends a request for approval and says where it is.
	Post(ctx context.Context, request Request) (Posted, error)
	// Close replaces a posted request with the same summary, the outcome and no action.
	Close(ctx context.Context, posted Posted, request Request, outcome string) error
}

// Sessions knows the person of a browser session. signin.Service implements it.
type Sessions interface {
	BrowserEmail(r *http.Request) (string, error)
}

// Service is the part that Slack and Teams share: the links, the decisions and the state of
// the posted requests.
type Service struct {
	db    *sql.DB
	orch  *orchestrator.Orchestrator
	audit *audit.Log
	// public is the address of the dashboard as a browser sees it, with no slash at the end.
	public string
	// Sessions is set at start, before the first request.
	Sessions Sessions
	now      func() time.Time

	platforms map[string]Platform
	// sweeping lets one sweep of the posted requests run at a time.
	sweeping sync.Mutex
}

// New returns the shared part. publicURL is the address of the dashboard as a browser sees
// it: the link page and the links in the messages are below it.
func New(db *sql.DB, orch *orchestrator.Orchestrator, log *audit.Log, publicURL string) *Service {
	return &Service{db: db, orch: orch, audit: log, public: strings.TrimSuffix(publicURL, "/"),
		now: time.Now, platforms: map[string]Platform{}}
}

func (s *Service) workLink(workItem string) string { return s.public + "/work/" + workItem }

// Click is one click on Approve or Reject in a chat, after the adapter verified the
// signature of the platform.
type Click struct {
	Account  Account
	WorkItem string
	// Binding is the binding that the clicked message carries.
	Binding string
	// Decision is "approve" or "reject".
	Decision string
}

// Answer is what the person who clicked reads. Nobody else sees it.
type Answer struct {
	Text string
	// URL and URLTitle are a link below the text, or empty.
	URL      string
	URLTitle string
	// Decided is set when the decision was recorded.
	Decided bool
}

// emit appends an audit event. A decision is never recorded here, so a failure does not
// stop the process: the caller gets the error or the log gets it.
func (s *Service) emit(e audit.Event) error {
	if _, err := s.audit.Append(e); err != nil {
		return fmt.Errorf("record %s in the audit log: %w", e.Type, err)
	}
	return nil
}

// refuse records that a linked person was refused before the decision reached the
// orchestrator, and returns the answer for that person.
func (s *Service) refuse(c Click, actor, reason string) Answer {
	data := map[string]any{"decision": c.Decision, "reason": reason, "origin": c.Account.origin()}
	if err := s.emit(audit.Event{Type: "chat.refused", Actor: actor, WorkItem: c.WorkItem,
		Summary: "Decision in chat refused: " + reason, Data: data}); err != nil {
		slog.Error("chat approval", "error", err)
	}
	return Answer{Text: reason, URL: s.workLink(c.WorkItem), URLTitle: "Open in Clanker"}
}

// plain removes the name of the error class from an error of the orchestrator.
func plain(err error) string {
	text := err.Error()
	for _, class := range []error{orchestrator.ErrForbidden, orchestrator.ErrConflict,
		orchestrator.ErrInvalid} {
		text = strings.TrimPrefix(text, class.Error()+": ")
	}
	return strings.ToUpper(text[:1]) + text[1:] + "."
}

// Decide handles one verified click. It returns what the person who clicked must read.
func (s *Service) Decide(ctx context.Context, c Click) Answer {
	email, err := s.linkedEmail(ctx, c.Account)
	if errors.Is(err, sql.ErrNoRows) {
		return s.askToLink(ctx, c.Account)
	}
	if err != nil {
		slog.Error("chat approval: read the link", "error", err)
		return Answer{Text: "The control plane could not read the link of this account. Try again."}
	}
	// The role is read at each click, not at the time of the link.
	user, err := s.orch.GetUserByEmail(email)
	if err != nil {
		return s.refuse(c, email, email+" has no role in this control plane: an admin must "+
			"add this email address.")
	}
	wi, err := s.orch.Get(c.WorkItem)
	if err != nil {
		return Answer{Text: "Work item " + c.WorkItem + " does not exist."}
	}
	if !slices.Contains(chatTiers, wi.Tier) {
		return s.refuse(c, user.Name, fmt.Sprintf("%s has tier %s. A change of this tier is "+
			"decided on the page that shows the whole plan.", wi.ID, wi.Tier))
	}
	return s.record(ctx, c, user)
}

// record gives the decision to the orchestrator. The orchestrator checks the role, the
// state and the binding, and records a refusal itself.
func (s *Service) record(ctx context.Context, c Click, user orchestrator.User) Answer {
	d := orchestrator.Decision{User: user.Name, Decision: c.Decision, Binding: c.Binding,
		Origin: c.Account.origin()}
	if c.Decision == "reject" {
		// The orchestrator needs a comment for a rejection. A chat button has no text field.
		d.Comment = "Rejected in " + labels[c.Account.Platform] + ". The person gave no comment."
	}
	_, err := s.orch.Decide(context.WithoutCancel(ctx), c.WorkItem, d)
	if errors.Is(err, orchestrator.ErrForbidden) || errors.Is(err, orchestrator.ErrConflict) ||
		errors.Is(err, orchestrator.ErrInvalid) {
		return Answer{Text: plain(err), URL: s.workLink(c.WorkItem), URLTitle: "Open in Clanker"}
	}
	if err != nil {
		slog.Error("chat approval: decide", "work_item", c.WorkItem, "error", err)
		return Answer{Text: "The control plane could not record the decision. Open the page " +
			"of the change to see its state.", URL: s.workLink(c.WorkItem),
			URLTitle: "Open in Clanker"}
	}
	return Answer{Decided: true, Text: fmt.Sprintf("Your decision on %s is recorded: %s, as %s.",
		c.WorkItem, c.Decision, user.Display)}
}

func (s *Service) linkedEmail(ctx context.Context, a Account) (string, error) {
	var email string
	err := s.db.QueryRowContext(ctx, `SELECT email FROM chat_links WHERE platform = $1
		AND workspace = $2 AND account = $3`, a.Platform, a.Workspace, a.ID).Scan(&email)
	return email, err
}

// formatTime shows a timestamp column to a person.
func formatTime(column string) string {
	return store.ParseTime(column).UTC().Format("2006-01-02 15:04 UTC")
}
