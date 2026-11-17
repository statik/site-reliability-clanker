package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
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
		WHERE name = $1`, name).Scan(&u.Name, &u.Display, &u.Role, &u.Email, &u.LastSeen)
	if err != nil {
		return User{}, fmt.Errorf("user %q is not known: %w", name, err)
	}
	return u, nil
}

// GetUserByEmail returns the user with this email address. Upper and lower case are the
// same.
func (o *Orchestrator) GetUserByEmail(email string) (User, error) {
	var u User
	err := o.DB.QueryRow(`SELECT name, display, role, email, last_seen FROM users
		WHERE lower(email) = lower($1)`, email).Scan(&u.Name, &u.Display, &u.Role, &u.Email,
		&u.LastSeen)
	if err != nil {
		return User{}, fmt.Errorf("no user has the email address %q: %w", email, err)
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

// Binding is one value for what an approval covers: the commit and the hash of each plan.
// A message in a chat carries it, so that a click on a message of an older plan is refused.
func Binding(commit string, hashes map[string]string) string {
	sum := sha256.New()
	sum.Write([]byte(commit))
	for _, layer := range slices.Sorted(maps.Keys(hashes)) {
		sum.Write([]byte("\x1f" + layer + "\x1f" + hashes[layer]))
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// Binding returns the binding of the current commit and the current plans of a work item.
func (o *Orchestrator) Binding(wi WorkItem) (string, error) {
	plans, err := o.Plans(wi.ID, false)
	if err != nil {
		return "", err
	}
	return Binding(wi.Commit, planHashes(plans)), nil
}

// ValidApprovers returns the people whose approval covers the current commit and plans.
func (o *Orchestrator) ValidApprovers(wi WorkItem) ([]string, error) { return o.validApprovers(wi) }

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
	// Binding is set when the person saw the change outside the dashboard. The decision is
	// refused when it is not the binding of the current commit and plans.
	Binding string
	// Origin says where the decision came from when it is not the dashboard, for example the
	// chat platform and the chat account. The audit event of the decision records it.
	Origin map[string]string
}

// record adds the origin of a decision to the data of its audit event.
func (d Decision) record(data map[string]any) map[string]any {
	if d.Origin != nil {
		data["origin"] = d.Origin
	}
	return data
}

// Decide records a decision of a human on a work item that waits for approval. The decision
// is bound to the commit and to the hash of each saved plan. A refused decision is recorded
// in the audit log. Two decisions on one work item run one after the other, so that two
// clicks at the same moment do not both find the work item waiting.
func (o *Orchestrator) Decide(ctx context.Context, id string, d Decision) (WorkItem, error) {
	value, _ := o.decideLocks.LoadOrStore(id, &sync.Mutex{})
	mu, _ := value.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	wi, err := o.decide(ctx, id, d)
	if err != nil && (errors.Is(err, ErrForbidden) || errors.Is(err, ErrConflict) ||
		errors.Is(err, ErrInvalid)) {
		o.emit(audit.Event{Type: "approval.refused", Actor: d.User, WorkItem: id,
			Summary: "Decision refused: " + err.Error(),
			Data:    d.record(map[string]any{"decision": d.Decision, "reason": err.Error()})})
	}
	return wi, err
}

func (o *Orchestrator) decide(ctx context.Context, id string, d Decision) (WorkItem, error) {
	wi, err := o.Get(id)
	if err != nil {
		return WorkItem{}, err
	}
	user, err := o.GetUser(d.User)
	if err != nil {
		return WorkItem{}, fmt.Errorf("%w: %v", ErrForbidden, err)
	}
	if user.Role != "approver" && user.Role != "admin" {
		return WorkItem{}, fmt.Errorf("%w: %s has role %s and cannot decide on %s: an approver "+
			"or admin must decide", ErrForbidden, user.Name, user.Role, id)
	}
	if !slices.Contains([]string{"approve", "reject", "request_changes"}, d.Decision) {
		return WorkItem{}, fmt.Errorf("%w: decision %q is not one of approve, reject, "+
			"request_changes", ErrInvalid, d.Decision)
	}
	if d.Decision != "approve" && strings.TrimSpace(d.Comment) == "" {
		return WorkItem{}, fmt.Errorf("%w: a comment is required to reject or to request "+
			"changes", ErrInvalid)
	}
	if d.ScheduledFor != "" {
		when := store.ParseTime(d.ScheduledFor)
		if when.IsZero() {
			return WorkItem{}, fmt.Errorf("%w: scheduled_for %q is not an RFC 3339 time",
				ErrInvalid, d.ScheduledFor)
		}
		if when.Before(o.Clock.Now()) {
			return WorkItem{}, fmt.Errorf("%w: scheduled_for %s is in the past; the control "+
				"plane time is %s", ErrInvalid, d.ScheduledFor, o.now())
		}
	}
	if wi.State != StateAwaitingApproval {
		return WorkItem{}, fmt.Errorf("%w: %s is in state %s and does not wait for approval",
			ErrConflict, id, wi.State)
	}
	plans, err := o.Plans(wi.ID, false)
	if err != nil {
		return WorkItem{}, err
	}
	if d.Binding != "" && d.Binding != Binding(wi.Commit, planHashes(plans)) {
		return WorkItem{}, fmt.Errorf("%w: the plan of %s changed after this request for "+
			"approval: decide on the current plan", ErrConflict, id)
	}
	hashes, _ := json.Marshal(planHashes(plans))
	at := o.now()
	if _, err := o.DB.Exec(`INSERT INTO approvals (work_item, approver, decision, comment,
		commit_sha, plan_hashes, at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, wi.ID, user.Name, d.Decision,
		d.Comment, wi.Commit, string(hashes), at); err != nil {
		return WorkItem{}, fmt.Errorf("record decision on %s: %w", id, err)
	}
	o.emit(audit.Event{Type: "approval.recorded", Actor: user.Name, Env: wi.Env,
		WorkItem: wi.ID, Summary: fmt.Sprintf("%s: %s by %s", wi.Title, d.Decision, user.Display),
		Data: d.record(map[string]any{"decision": d.Decision, "comment": d.Comment,
			"commit": wi.Commit, "plan_hashes": planHashes(plans), "tier": wi.Tier})})
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
	default:
		// A window from any approver is kept, also when this approval is not the last one.
		if d.ScheduledFor != "" {
			wi.ScheduledFor = store.FormatTime(store.ParseTime(d.ScheduledFor))
			if err := o.save(wi); err != nil {
				return WorkItem{}, err
			}
		}
		return o.afterApproval(ctx, wi, d)
	}
}

func (o *Orchestrator) afterApproval(ctx context.Context, wi WorkItem, d Decision) (WorkItem, error) {
	approvers, err := o.validApprovers(wi)
	if err != nil {
		return WorkItem{}, err
	}
	need, err := o.ApproversRequired(wi.Tier)
	if err != nil {
		return WorkItem{}, fmt.Errorf("count the approvals of %s: %w", wi.ID, err)
	}
	if len(approvers) < need {
		return wi, nil
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
		if err == nil && o.Hooks.LayerApplied != nil {
			err = o.Hooks.LayerApplied(ctx, wi, layer)
		}
		switch {
		case err == nil:
			continue
		case errors.Is(err, jobspec.ErrStalePlan), errors.Is(err, errOutsideEnvelope):
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
	if wi.PRID != 0 {
		// Main moved under this change. Plan the branch together with what merged since, so
		// that the new plan does not undo another approved change.
		pr, err := o.Forge.SyncPull(wi.PRID)
		if err != nil {
			return wi, o.setState(&wi, StateChangesRequested, "forge", err.Error())
		}
		if pr.HeadCommit != wi.Commit {
			o.emit(audit.Event{Type: "pr.updated", Actor: "orchestrator", Env: wi.Env,
				WorkItem: wi.ID, Summary: fmt.Sprintf("Pull request #%d updated with main", pr.ID),
				Data: map[string]any{"pr": pr.ID, "commit": pr.HeadCommit}})
		}
		wi.Commit, wi.BaseCommit = pr.HeadCommit, pr.BaseCommit
	}
	for _, layer := range wi.Layers {
		if !o.canPlan(wi.Env, layer) {
			continue
		}
		if _, err := o.planLayer(ctx, wi, layer); err != nil {
			return wi, o.setState(&wi, StateFailed, "runner", err.Error())
		}
	}
	return o.requestApproval(ctx, wi, true)
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
	applyErr := o.runApply(ctx, wi, plan, grant, &job, logFile)
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
	job *Job, logFile *jobLog) error {
	spec, err := o.layerJob(wi, jobspec.KindApply, plan.Layer)
	if err != nil {
		return err
	}
	planFile, err := o.planFile(plan.ID)
	if err != nil {
		return err
	}
	// The broker compares the hash of the stored plan with the grant. The runner gets
	// these same bytes and compares them again before it applies.
	cred, err := o.Broker.Exchange(ctx, grant, broker.Presentation{Commit: wi.Commit,
		PlanSHA256: jobspec.SHA256(planFile)})
	if err != nil {
		return fmt.Errorf("the broker refused the grant: %w", err)
	}
	fmt.Fprintf(logFile, "grant %s accepted: %s\n", grant.ID, cred.Mechanism)
	o.emit(audit.Event{Type: "apply.started", Actor: "runner", Env: wi.Env, Layer: plan.Layer,
		WorkItem: wi.ID, Summary: fmt.Sprintf("Apply of %s started", plan.Layer),
		Data: map[string]any{"grant": grant.ID, "credential": cred.Mechanism,
			"plan_sha256": plan.SHA256}})
	spec.KubeConfig, spec.Log = cred.KubeConfig, logFile
	spec.Plan, spec.PlanSHA256 = planFile, grant.PlanSHA256
	result, err := o.run(ctx, job, spec)
	if err != nil {
		return err
	}
	return store.SetLayerOutputs(o.DB, wi.Env, plan.Layer, result.Outputs, o.now())
}
