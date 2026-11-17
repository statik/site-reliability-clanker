package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/policy"
	"github.com/statik/site-reliability-clanker/internal/runner"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// User is a person who can sign in to the dashboard.
type User struct {
	Name     string `json:"name"`
	Display  string `json:"display"`
	Role     string `json:"role"`
	Email    string `json:"email"`
	LastSeen string `json:"last_seen"`
}

// GetUser returns one user.
func (o *Orchestrator) GetUser(name string) (User, error) {
	var u User
	err := o.DB.QueryRow(`SELECT name, display, role, email, last_seen FROM users
		WHERE name = ?`, name).Scan(&u.Name, &u.Display, &u.Role, &u.Email, &u.LastSeen)
	if err != nil {
		return User{}, fmt.Errorf("user %q is not known: %w", name, err)
	}
	return u, nil
}

func planHashes(plans []Plan) map[string]string {
	hashes := map[string]string{}
	for _, p := range plans {
		hashes[p.Layer] = p.SHA256
	}
	return hashes
}

// validApprovers returns the distinct people whose approval covers the current commit and
// the current plan hashes. An approval of an older commit or plan does not count.
func (o *Orchestrator) validApprovers(wi WorkItem) ([]string, error) {
	plans, err := o.Plans(wi.ID, false)
	if err != nil {
		return nil, err
	}
	approvals, err := o.Approvals(wi.ID)
	if err != nil {
		return nil, err
	}
	current := planHashes(plans)
	approvers := []string{}
	for _, a := range approvals {
		if a.Decision != "approve" || a.Commit != wi.Commit {
			continue
		}
		if !maps.Equal(a.PlanHashes, current) || slices.Contains(approvers, a.Approver) {
			continue
		}
		approvers = append(approvers, a.Approver)
	}
	return approvers, nil
}

// Decision is what an approver submits.
type Decision struct {
	User         string
	Decision     string
	Comment      string
	ScheduledFor string
}

// Decide records a decision of a human on a work item that waits for approval. The decision
// is bound to the commit and to the hash of each saved plan.
func (o *Orchestrator) Decide(ctx context.Context, id string, d Decision) (WorkItem, error) {
	wi, err := o.Get(id)
	if err != nil {
		return WorkItem{}, err
	}
	user, err := o.GetUser(d.User)
	if err != nil {
		return WorkItem{}, err
	}
	if user.Role != "approver" && user.Role != "admin" {
		return WorkItem{}, fmt.Errorf("%s has role %s and cannot decide on %s: an approver or "+
			"admin must decide", user.Name, user.Role, id)
	}
	if wi.State != StateAwaitingApproval {
		return WorkItem{}, fmt.Errorf("%s is in state %s and does not wait for approval", id,
			wi.State)
	}
	plans, err := o.Plans(wi.ID, false)
	if err != nil {
		return WorkItem{}, err
	}
	hashes, _ := json.Marshal(planHashes(plans))
	at := o.now()
	if _, err := o.DB.Exec(`INSERT INTO approvals (work_item, approver, decision, comment,
		commit_sha, plan_hashes, at) VALUES (?,?,?,?,?,?,?)`, wi.ID, user.Name, d.Decision,
		d.Comment, wi.Commit, string(hashes), at); err != nil {
		return WorkItem{}, fmt.Errorf("record decision on %s: %w", id, err)
	}
	o.emit(audit.Event{Type: "approval.recorded", Actor: user.Name, Env: wi.Env,
		WorkItem: wi.ID, Summary: fmt.Sprintf("%s: %s by %s", wi.Title, d.Decision, user.Display),
		Data: map[string]any{"decision": d.Decision, "comment": d.Comment, "commit": wi.Commit,
			"plan_hashes": planHashes(plans), "tier": wi.Tier}})
	switch d.Decision {
	case "reject":
		if wi.PRID != 0 {
			if err := o.Forge.ClosePull(wi.PRID); err != nil {
				return WorkItem{}, err
			}
		}
		return wi, o.setState(&wi, StateRejected, user.Name, d.Comment)
	case "request_changes":
		return wi, o.setState(&wi, StateChangesRequested, user.Name, d.Comment)
	case "approve":
		return o.afterApproval(ctx, wi, d)
	default:
		return WorkItem{}, fmt.Errorf("decision %q is not one of approve, reject, "+
			"request_changes", d.Decision)
	}
}

func (o *Orchestrator) afterApproval(ctx context.Context, wi WorkItem, d Decision) (WorkItem, error) {
	approvers, err := o.validApprovers(wi)
	if err != nil {
		return WorkItem{}, err
	}
	need := policy.ApproversRequired(wi.Tier)
	if len(approvers) < need {
		return wi, nil
	}
	if d.ScheduledFor != "" {
		wi.ScheduledFor = d.ScheduledFor
	}
	if err := o.setState(&wi, StateApproved, d.User,
		fmt.Sprintf("%d of %d approvers", len(approvers), need)); err != nil {
		return WorkItem{}, err
	}
	if wi.ScheduledFor != "" && o.Clock.Now().Before(store.ParseTime(wi.ScheduledFor)) {
		return wi, o.setState(&wi, StateScheduled, "orchestrator",
			"waits for the window at "+wi.ScheduledFor)
	}
	go o.applyInBackground(wi.ID)
	return wi, nil
}

func (o *Orchestrator) applyInBackground(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	if _, err := o.Apply(ctx, id); err != nil {
		o.emit(audit.Event{Type: "apply.error", Actor: "orchestrator", WorkItem: id,
			Summary: "Apply stopped: " + err.Error()})
	}
}

// RunDue starts the apply of scheduled work items whose window opened.
func (o *Orchestrator) RunDue() error {
	items, err := o.List()
	if err != nil {
		return err
	}
	now := o.Clock.Now()
	for _, wi := range items {
		if wi.State == StateScheduled && !now.Before(store.ParseTime(wi.ScheduledFor)) {
			go o.applyInBackground(wi.ID)
		}
	}
	return nil
}

// errOutsideEnvelope means that a deferred layer wants to do more than create resources.
var errOutsideEnvelope = errors.New("the plan is outside the create-only envelope")

// Apply runs the approved change layer by layer, then verifies the environment.
func (o *Orchestrator) Apply(ctx context.Context, id string) (WorkItem, error) {
	wi, err := o.Get(id)
	if err != nil {
		return WorkItem{}, err
	}
	unlock := o.lockEnv(wi.Env)
	defer unlock()
	wi, err = o.Get(id)
	if err != nil {
		return WorkItem{}, err
	}
	if wi.State != StateApproved && wi.State != StateScheduled {
		return wi, fmt.Errorf("%s is in state %s and cannot be applied", id, wi.State)
	}
	approvers, err := o.validApprovers(wi)
	if err != nil {
		return WorkItem{}, err
	}
	if err := o.setState(&wi, StateApplying, "orchestrator", "apply started"); err != nil {
		return WorkItem{}, err
	}
	for _, layer := range wi.Layers {
		err := o.applyLayer(ctx, &wi, layer, approvers)
		switch {
		case err == nil:
			continue
		case errors.Is(err, runner.ErrStalePlan), errors.Is(err, errOutsideEnvelope):
			return o.backToApproval(ctx, wi, err)
		default:
			if stateErr := o.setState(&wi, StateFailed, "runner", err.Error()); stateErr != nil {
				return WorkItem{}, stateErr
			}
			return wi, err
		}
	}
	if wi.PRID != 0 {
		merged, err := o.Forge.MergePull(wi.PRID, wi.Commit, "clanker-orchestrator", o.now())
		if err != nil {
			return wi, o.setState(&wi, StateFailed, "forge", err.Error())
		}
		o.emit(audit.Event{Type: "pr.merged", Actor: "orchestrator", Env: wi.Env,
			WorkItem: wi.ID, Summary: fmt.Sprintf("Pull request #%d merged after apply", wi.PRID),
			Data: map[string]any{"pr": wi.PRID, "merge_commit": merged, "commit": wi.Commit}})
	}
	return o.verify(ctx, wi, approvers)
}

// backToApproval handles a plan that no longer matches what was approved. The old approvals
// do not cover the new plan hash, so the work item waits for approval again.
func (o *Orchestrator) backToApproval(ctx context.Context, wi WorkItem, cause error) (WorkItem, error) {
	o.emit(audit.Event{Type: "apply.refused", Actor: "runner", Env: wi.Env, WorkItem: wi.ID,
		Summary: "Apply refused: " + cause.Error(),
		Data:    map[string]any{"reason": cause.Error()}})
	if err := o.setState(&wi, StatePlanned, "orchestrator", cause.Error()); err != nil {
		return WorkItem{}, err
	}
	for _, layer := range wi.Layers {
		if !o.canPlan(wi.Env, layer) {
			continue
		}
		if _, err := o.planLayer(ctx, wi, layer); err != nil {
			return wi, o.setState(&wi, StateFailed, "runner", err.Error())
		}
	}
	o.explain(ctx, &wi)
	if err := o.setState(&wi, StateAwaitingApproval, "orchestrator",
		"the plan changed; earlier approvals do not cover the new plan"); err != nil {
		return WorkItem{}, err
	}
	o.emit(audit.Event{Type: "approval.requested", Actor: "orchestrator", Env: wi.Env,
		WorkItem: wi.ID, Summary: "Approval needed again: " + wi.Title,
		Data: map[string]any{"tier": wi.Tier, "rule": wi.TierRule, "title": wi.Title,
			"sentence":           wi.Explanation.Sentence,
			"approvers_required": policy.ApproversRequired(wi.Tier)}})
	return wi, nil
}

func (o *Orchestrator) currentPlan(wi WorkItem, layer string) (Plan, bool, error) {
	plans, err := o.Plans(wi.ID, false)
	if err != nil {
		return Plan{}, false, err
	}
	for _, p := range plans {
		if p.Layer == layer && p.Commit == wi.Commit {
			return p, true, nil
		}
	}
	return Plan{}, false, nil
}

// applyLayer applies one layer under a grant. A layer without a saved plan was deferred at
// approval time: it is planned now and must only create resources.
func (o *Orchestrator) applyLayer(ctx context.Context, wi *WorkItem, layer string,
	approvers []string) error {
	plan, found, err := o.currentPlan(*wi, layer)
	if err != nil {
		return err
	}
	if !found {
		plan, err = o.planLayer(ctx, *wi, layer)
		if err != nil {
			return err
		}
		if !plan.Policy.CreateOnly {
			return fmt.Errorf("%w: layer %s", errOutsideEnvelope, layer)
		}
		o.emit(audit.Event{Type: "plan.within_envelope", Actor: "policy", Env: wi.Env,
			Layer: layer, WorkItem: wi.ID,
			Summary: fmt.Sprintf("Plan of %s only creates resources; the approval covers it",
				layer),
			Data: map[string]any{"plan_sha256": plan.SHA256}})
	}
	if !plan.Facts.HasChanges() {
		return nil
	}
	return o.applyPlan(ctx, *wi, plan, approvers)
}

func (o *Orchestrator) applyPlan(ctx context.Context, wi WorkItem, plan Plan,
	approvers []string) error {
	layer := plan.Layer
	grant, err := o.Broker.Issue(broker.Request{WorkItem: wi.ID, Env: wi.Env, Layer: layer,
		Kind: broker.KindApply, Commit: wi.Commit, PlanSHA256: plan.SHA256, Tier: wi.Tier,
		Approvers: approvers, Duration: 30 * time.Minute})
	if err != nil {
		return err
	}
	o.emit(audit.Event{Type: "grant.issued", Actor: "broker", Env: wi.Env, Layer: layer,
		WorkItem: wi.ID,
		Summary: fmt.Sprintf("Grant %s: %s may apply %s until %s", grant.ID, grant.Identity,
			layer, grant.Expires),
		Data: map[string]any{"grant": grant.ID, "identity": grant.Identity, "kind": grant.Kind,
			"commit": grant.Commit, "plan_sha256": grant.PlanSHA256, "tier": grant.Tier,
			"approvers": grant.Approvers, "not_before": grant.NotBefore,
			"expires": grant.Expires}})
	job, logFile, err := o.startJob(wi, layer, "apply", grant.ID)
	if err != nil {
		return err
	}
	applyErr := o.runApply(ctx, wi, plan, grant, logFile)
	o.endJob(job, logFile, applyErr)
	outcome := "used"
	if applyErr != nil {
		outcome = "revoked"
	}
	if err := o.Broker.Revoke(ctx, grant, outcome); err != nil {
		o.emit(audit.Event{Type: "grant.revoke_failed", Actor: "broker", Env: wi.Env,
			Layer: layer, WorkItem: wi.ID, Summary: err.Error()})
	} else {
		o.emit(audit.Event{Type: "grant.revoked", Actor: "broker", Env: wi.Env, Layer: layer,
			WorkItem: wi.ID, Summary: fmt.Sprintf("Grant %s ended; the credential is gone",
				grant.ID), Data: map[string]any{"grant": grant.ID, "outcome": outcome}})
	}
	eventType, summary := "apply.succeeded", fmt.Sprintf("Applied %s", layer)
	data := map[string]any{"job": job.ID, "grant": grant.ID, "plan_sha256": plan.SHA256,
		"commit": wi.Commit, "tool_digest": job.ToolDigest}
	if applyErr != nil {
		eventType, summary = "apply.failed", fmt.Sprintf("Apply of %s failed", layer)
		data["error"] = applyErr.Error()
	}
	o.emit(audit.Event{Type: eventType, Actor: "runner", Env: wi.Env, Layer: layer,
		WorkItem: wi.ID, Summary: summary, Data: data})
	return applyErr
}

func (o *Orchestrator) runApply(ctx context.Context, wi WorkItem, plan Plan, grant broker.Grant,
	logFile *os.File) error {
	checkout, err := o.checkout(wi)
	if err != nil {
		return err
	}
	// The runner presents what it holds. The broker compares it with the grant.
	held, err := runner.FileSHA256(plan.PlanPath)
	if err != nil {
		return err
	}
	cred, err := o.Broker.Exchange(ctx, grant, broker.Presentation{Commit: wi.Commit,
		PlanSHA256: held})
	if err != nil {
		return fmt.Errorf("the broker refused the grant: %w", err)
	}
	fmt.Fprintf(logFile, "grant %s accepted: %s\n", grant.ID, cred.Mechanism)
	o.emit(audit.Event{Type: "apply.started", Actor: "runner", Env: wi.Env, Layer: plan.Layer,
		WorkItem: wi.ID, Summary: fmt.Sprintf("Apply of %s started", plan.Layer),
		Data: map[string]any{"grant": grant.ID, "credential": cred.Mechanism,
			"plan_sha256": plan.SHA256}})
	return o.Runner.Apply(ctx, runner.Job{Env: wi.Env, Cloud: o.Envs[wi.Env].Cloud,
		Layer: plan.Layer, Checkout: checkout, KubeConfig: cred.KubeConfig, Log: logFile},
		plan.PlanPath, grant.PlanSHA256)
}
