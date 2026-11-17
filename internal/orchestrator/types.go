// Package orchestrator owns the work-item state machine. It connects the forge, the runner,
// the policy engine and the access broker, and it writes every step to the audit log.
package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/policy"
	"github.com/statik/site-reliability-clanker/internal/runner"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// Work item states, from section 5.1 of the plan.
const (
	StateDetected         = "Detected"
	StateDrafting         = "Drafting"
	StatePlanned          = "Planned"
	StateAwaitingApproval = "AwaitingApproval"
	StateChangesRequested = "ChangesRequested"
	StateApproved         = "Approved"
	StateScheduled        = "Scheduled"
	StateApplying         = "Applying"
	StateVerifying        = "Verifying"
	StateDone             = "Done"
	StateFailed           = "Failed"
	StateRolledBack       = "RolledBack"
	StateRejected         = "Rejected"
)

// Layers lists the layers in apply order.
var Layers = []string{"00-bootstrap", "10-network", "20-cluster", "30-data", "40-platform",
	"50-app"}

// PlatformRepo is the forge repository with the OpenTofu code of every environment.
const PlatformRepo = "platform-live"

// RunbooksRepo is the forge repository with runbook jobs and policies.
const RunbooksRepo = "runbooks"

// Explanation is the prose on the approval page. The facts next to it come from the plan.
type Explanation struct {
	Sentence     string   `json:"sentence"`
	Why          string   `json:"why"`
	Downtime     string   `json:"downtime"`
	UserEffect   string   `json:"user_effect"`
	CostChange   string   `json:"cost_change"`
	Duration     string   `json:"duration"`
	PreChecks    []string `json:"pre_checks"`
	Backup       string   `json:"backup"`
	Rollback     string   `json:"rollback"`
	Verification string   `json:"verification"`
	IfDeferred   string   `json:"if_deferred"`
	// Source is "template", "replay" or the model name.
	Source string `json:"source"`
}

// WorkItem is one unit of work on the dashboard.
type WorkItem struct {
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	Kind         string      `json:"kind"`
	Env          string      `json:"env"`
	Layers       []string    `json:"layers"`
	State        string      `json:"state"`
	Tier         string      `json:"tier"`
	TierRule     string      `json:"tier_rule"`
	Trigger      string      `json:"trigger"`
	PRID         int64       `json:"pr_id"`
	Commit       string      `json:"commit"`
	BaseCommit   string      `json:"base_commit"`
	Summary      string      `json:"summary"`
	Explanation  Explanation `json:"explanation"`
	ScheduledFor string      `json:"scheduled_for"`
	RollbackOf   string      `json:"rollback_of"`
	CreatedAt    string      `json:"created_at"`
	UpdatedAt    string      `json:"updated_at"`
}

// Plan is a saved plan for one layer of a work item.
type Plan struct {
	ID         int64           `json:"id"`
	WorkItem   string          `json:"work_item"`
	Layer      string          `json:"layer"`
	Commit     string          `json:"commit"`
	SHA256     string          `json:"plan_sha256"`
	PlanPath   string          `json:"-"`
	JSONPath   string          `json:"-"`
	Facts      runner.Facts    `json:"facts"`
	Policy     policy.Decision `json:"policy"`
	Superseded bool            `json:"superseded"`
	CreatedAt  string          `json:"created_at"`
}

// Approval is one decision by a human.
type Approval struct {
	ID         int64             `json:"id"`
	WorkItem   string            `json:"work_item"`
	Approver   string            `json:"approver"`
	Decision   string            `json:"decision"`
	Comment    string            `json:"comment"`
	Commit     string            `json:"commit"`
	PlanHashes map[string]string `json:"plan_hashes"`
	At         string            `json:"at"`
}

// Job is one run of the runner.
type Job struct {
	ID         int64  `json:"id"`
	WorkItem   string `json:"work_item"`
	Layer      string `json:"layer"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	LogPath    string `json:"-"`
	GrantID    string `json:"grant_id"`
	ToolDigest string `json:"tool_digest"`
	StartedAt  string `json:"started_at"`
	EndedAt    string `json:"ended_at"`
}

// EnvConfig describes one environment.
type EnvConfig struct {
	Name  string `json:"name"`
	Cloud string `json:"cloud"`
}

// Clock gives the virtual time.
type Clock interface{ Now() time.Time }

// Hooks are the points where other components take part in the pipeline.
type Hooks struct {
	// Explain writes the prose for the approval page. Nil selects the template text.
	Explain func(ctx context.Context, wi WorkItem, plans []Plan) (Explanation, error)
	// Verify checks the environment after an apply. Nil skips the check.
	Verify func(ctx context.Context, wi WorkItem) (VerifyResult, error)
	// Applied runs after a successful apply and verification, for example to take a snapshot.
	Applied func(ctx context.Context, wi WorkItem)
}

// VerifyResult is the outcome of the check after an apply.
type VerifyResult struct {
	OK     bool     `json:"ok"`
	Checks []string `json:"checks"`
	Reason string   `json:"reason"`
}

// Orchestrator runs work items.
type Orchestrator struct {
	DB      *sql.DB
	Audit   *audit.Log
	Forge   *forge.Forge
	Runner  *runner.Runner
	Broker  *broker.Broker
	Clock   Clock
	DataDir string
	Envs    map[string]EnvConfig
	Hooks   Hooks

	envLocks sync.Map
	idMu     sync.Mutex
}

func (o *Orchestrator) lockEnv(env string) func() {
	value, _ := o.envLocks.LoadOrStore(env, &sync.Mutex{})
	mu, _ := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (o *Orchestrator) now() string { return store.FormatTime(o.Clock.Now()) }

const workItemColumns = `id, title, kind, env, layers, state, tier, tier_rule, trigger, pr_id,
	commit_sha, base_commit, summary, explanation, scheduled_for, rollback_of, created_at,
	updated_at`

func scanWorkItem(row interface{ Scan(...any) error }) (WorkItem, error) {
	var wi WorkItem
	var layers, explanation string
	err := row.Scan(&wi.ID, &wi.Title, &wi.Kind, &wi.Env, &layers, &wi.State, &wi.Tier,
		&wi.TierRule, &wi.Trigger, &wi.PRID, &wi.Commit, &wi.BaseCommit, &wi.Summary,
		&explanation, &wi.ScheduledFor, &wi.RollbackOf, &wi.CreatedAt, &wi.UpdatedAt)
	if err != nil {
		return WorkItem{}, err
	}
	if err := json.Unmarshal([]byte(layers), &wi.Layers); err != nil {
		return WorkItem{}, fmt.Errorf("decode layers of %s: %w", wi.ID, err)
	}
	if err := json.Unmarshal([]byte(explanation), &wi.Explanation); err != nil {
		return WorkItem{}, fmt.Errorf("decode explanation of %s: %w", wi.ID, err)
	}
	return wi, nil
}

// Get returns one work item.
func (o *Orchestrator) Get(id string) (WorkItem, error) {
	wi, err := scanWorkItem(o.DB.QueryRow(`SELECT `+workItemColumns+
		` FROM work_items WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return WorkItem{}, fmt.Errorf("work item %s does not exist", id)
	}
	return wi, err
}

// List returns all work items, newest first.
func (o *Orchestrator) List() ([]WorkItem, error) {
	rows, err := o.DB.Query(`SELECT ` + workItemColumns +
		` FROM work_items ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list work items: %w", err)
	}
	defer rows.Close()
	items := []WorkItem{}
	for rows.Next() {
		wi, err := scanWorkItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, wi)
	}
	return items, rows.Err()
}

func (o *Orchestrator) save(wi WorkItem) error {
	layers, _ := json.Marshal(wi.Layers)
	explanation, _ := json.Marshal(wi.Explanation)
	wi.UpdatedAt = o.now()
	_, err := o.DB.Exec(`UPDATE work_items SET title=?, layers=?, state=?, tier=?, tier_rule=?,
		pr_id=?, commit_sha=?, base_commit=?, summary=?, explanation=?, scheduled_for=?,
		rollback_of=?, updated_at=? WHERE id=?`,
		wi.Title, string(layers), wi.State, wi.Tier, wi.TierRule, wi.PRID, wi.Commit,
		wi.BaseCommit, wi.Summary, string(explanation), wi.ScheduledFor, wi.RollbackOf,
		wi.UpdatedAt, wi.ID)
	if err != nil {
		return fmt.Errorf("save work item %s: %w", wi.ID, err)
	}
	return nil
}

// NewWorkItem describes a work item to create.
type NewWorkItem struct {
	Title   string
	Kind    string
	Env     string
	Layers  []string
	Trigger string
	Actor   string
}

// Create records a new work item in state Detected.
func (o *Orchestrator) Create(n NewWorkItem) (WorkItem, error) {
	if _, ok := o.Envs[n.Env]; !ok {
		return WorkItem{}, fmt.Errorf("create work item: environment %q is not configured", n.Env)
	}
	o.idMu.Lock()
	defer o.idMu.Unlock()
	var count int
	if err := o.DB.QueryRow(`SELECT COUNT(*) FROM work_items`).Scan(&count); err != nil {
		return WorkItem{}, fmt.Errorf("count work items: %w", err)
	}
	now := o.now()
	wi := WorkItem{
		ID: fmt.Sprintf("WI-%04d", count+1), Title: n.Title, Kind: n.Kind, Env: n.Env,
		Layers: n.Layers, State: StateDetected, Trigger: n.Trigger, CreatedAt: now, UpdatedAt: now,
	}
	if wi.Layers == nil {
		wi.Layers = []string{}
	}
	layers, _ := json.Marshal(wi.Layers)
	_, err := o.DB.Exec(`INSERT INTO work_items (id, title, kind, env, layers, state, trigger,
		created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		wi.ID, wi.Title, wi.Kind, wi.Env, string(layers), wi.State, wi.Trigger, now, now)
	if err != nil {
		return WorkItem{}, fmt.Errorf("insert work item %s: %w", wi.ID, err)
	}
	o.emit(audit.Event{Type: "workitem.created", Actor: n.Actor, Env: wi.Env, WorkItem: wi.ID,
		Summary: wi.Title, Data: map[string]any{"kind": wi.Kind, "trigger": wi.Trigger}})
	return wi, nil
}

// emit appends an audit event. A failure to write the audit log stops the process: the
// control plane must not act without a record.
func (o *Orchestrator) emit(e audit.Event) audit.Event {
	saved, err := o.Audit.Append(e)
	if err != nil {
		panic(fmt.Sprintf("audit log is not writable, stopping: %v", err))
	}
	return saved
}

func (o *Orchestrator) setState(wi *WorkItem, state, actor, reason string) error {
	from := wi.State
	wi.State = state
	if err := o.save(*wi); err != nil {
		return err
	}
	o.emit(audit.Event{Type: "workitem.state_changed", Actor: actor, Env: wi.Env,
		WorkItem: wi.ID, Summary: fmt.Sprintf("%s: %s → %s", wi.Title, from, state),
		Data: map[string]any{"from": from, "to": state, "reason": reason, "tier": wi.Tier,
			"kind": wi.Kind, "title": wi.Title}})
	return nil
}

// Plans returns the plans of a work item. Superseded plans are included when all is set.
func (o *Orchestrator) Plans(workItem string, all bool) ([]Plan, error) {
	query := `SELECT id, work_item, layer, commit_sha, plan_sha256, plan_path, json_path, facts,
		policy, superseded, created_at FROM plans WHERE work_item = ?`
	if !all {
		query += ` AND superseded = 0`
	}
	rows, err := o.DB.Query(query+` ORDER BY layer, id`, workItem)
	if err != nil {
		return nil, fmt.Errorf("list plans of %s: %w", workItem, err)
	}
	defer rows.Close()
	plans := []Plan{}
	for rows.Next() {
		var p Plan
		var facts, decision string
		if err := rows.Scan(&p.ID, &p.WorkItem, &p.Layer, &p.Commit, &p.SHA256, &p.PlanPath,
			&p.JSONPath, &facts, &decision, &p.Superseded, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan plan: %w", err)
		}
		if err := json.Unmarshal([]byte(facts), &p.Facts); err != nil {
			return nil, fmt.Errorf("decode facts of plan %d: %w", p.ID, err)
		}
		if err := json.Unmarshal([]byte(decision), &p.Policy); err != nil {
			return nil, fmt.Errorf("decode policy of plan %d: %w", p.ID, err)
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

// Approvals returns the decisions on a work item, oldest first.
func (o *Orchestrator) Approvals(workItem string) ([]Approval, error) {
	rows, err := o.DB.Query(`SELECT id, work_item, approver, decision, comment, commit_sha,
		plan_hashes, at FROM approvals WHERE work_item = ? ORDER BY id`, workItem)
	if err != nil {
		return nil, fmt.Errorf("list approvals of %s: %w", workItem, err)
	}
	defer rows.Close()
	approvals := []Approval{}
	for rows.Next() {
		var a Approval
		var hashes string
		if err := rows.Scan(&a.ID, &a.WorkItem, &a.Approver, &a.Decision, &a.Comment, &a.Commit,
			&hashes, &a.At); err != nil {
			return nil, fmt.Errorf("scan approval: %w", err)
		}
		if err := json.Unmarshal([]byte(hashes), &a.PlanHashes); err != nil {
			return nil, fmt.Errorf("decode plan hashes of approval %d: %w", a.ID, err)
		}
		approvals = append(approvals, a)
	}
	return approvals, rows.Err()
}

// Jobs returns the runner jobs of a work item, oldest first.
func (o *Orchestrator) Jobs(workItem string) ([]Job, error) {
	rows, err := o.DB.Query(`SELECT id, work_item, layer, kind, status, log_path, grant_id,
		tool_digest, started_at, ended_at FROM jobs WHERE work_item = ? ORDER BY id`, workItem)
	if err != nil {
		return nil, fmt.Errorf("list jobs of %s: %w", workItem, err)
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.WorkItem, &j.Layer, &j.Kind, &j.Status, &j.LogPath,
			&j.GrantID, &j.ToolDigest, &j.StartedAt, &j.EndedAt); err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// Job returns one job.
func (o *Orchestrator) Job(id int64) (Job, error) {
	var j Job
	err := o.DB.QueryRow(`SELECT id, work_item, layer, kind, status, log_path, grant_id,
		tool_digest, started_at, ended_at FROM jobs WHERE id = ?`, id).Scan(&j.ID, &j.WorkItem,
		&j.Layer, &j.Kind, &j.Status, &j.LogPath, &j.GrantID, &j.ToolDigest, &j.StartedAt,
		&j.EndedAt)
	if err != nil {
		return Job{}, fmt.Errorf("read job %d: %w", id, err)
	}
	return j, nil
}

// Transition moves a work item to a state outside the plan-and-apply pipeline, for example
// for an incident. It records the change in the audit log.
func (o *Orchestrator) Transition(id, state, actor, reason string) (WorkItem, error) {
	wi, err := o.Get(id)
	if err != nil {
		return WorkItem{}, err
	}
	return wi, o.setState(&wi, state, actor, reason)
}

// SetExplanation stores the prose of a work item.
func (o *Orchestrator) SetExplanation(id string, e Explanation) error {
	wi, err := o.Get(id)
	if err != nil {
		return err
	}
	wi.Explanation, wi.Summary = e, e.Sentence
	return o.save(wi)
}
