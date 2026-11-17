package orchestrator

import (
	"context"
	"fmt"

	"github.com/statik/site-reliability-clanker/internal/audit"
)

// Reconciled says what Reconcile ended.
type Reconciled struct {
	Jobs   int
	Grants int
	// Failed holds the work items that Reconcile failed.
	Failed []string
	// Replan holds the work items that the caller must plan again with PlanWorkItem.
	Replan []string
}

// endJobs ends each running job as lost and returns how many there were.
func (o *Orchestrator) endJobs(ctx context.Context) (int, error) {
	result, err := o.DB.ExecContext(ctx, `UPDATE jobs SET status = 'lost', ended_at = $1,
		log = log || $2 WHERE status = 'running'`, o.now(),
		"\nJOB LOST: the control plane stopped while this job ran\n")
	if err != nil {
		return 0, fmt.Errorf("end the jobs that a stop interrupted: %w", err)
	}
	lost, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count the jobs that a stop interrupted: %w", err)
	}
	return int(lost), nil
}

// revokeGrants revokes each grant that did not end and returns how many there were.
func (o *Orchestrator) revokeGrants(ctx context.Context) (int, error) {
	grants, err := o.Broker.Unended()
	if err != nil {
		return 0, err
	}
	for _, g := range grants {
		event := audit.Event{Type: "grant.revoked", Actor: "broker", Env: g.Env, Layer: g.Layer,
			WorkItem: g.WorkItem,
			Summary:  fmt.Sprintf("Grant %s revoked: the control plane stopped while it was open", g.ID),
			Data:     map[string]any{"grant": g.ID, "outcome": "revoked"}}
		// The grant is ended also when the target does not answer.
		if err := o.Broker.Revoke(ctx, g, "revoked"); err != nil {
			event.Type, event.Summary = "grant.revoke_failed", err.Error()
		}
		o.emit(event)
	}
	return len(grants), nil
}

// interrupted says what a stop of the control plane means for a work item in this state:
// the reason why it fails, or that it must be planned again. Both are empty for a work
// item that waits for a person or for its window, or that ended.
func interrupted(wi WorkItem) (failure string, replan bool) {
	switch wi.State {
	case StateApplying:
		return "the control plane stopped during the apply. The environment can be partly changed", false
	case StateVerifying:
		return "the control plane stopped during the verification. The environment can be " +
			"partly changed", false
	case StateDetected, StateDrafting, StatePlanned, StateApproved:
		if len(wi.Layers) > 0 {
			return "", true
		}
		return "the control plane stopped while this work was in progress", false
	default:
		return "", false
	}
}

// Reconcile ends the work that a stop of the control plane interrupted. Call it once at
// start, before anything else runs, and only when no other control plane writes.
//
// A job, a grant or an apply that was in progress has no process behind it any more.
// Reconcile ends each running job as lost, revokes each grant that did not end, and fails
// each work item that was in an apply or a verification. It does not continue an apply:
// the environment can be partly changed, and the saved plan no longer describes it.
//
// A work item that was in the plan, or was approved and not yet in the apply, must be
// planned again: Reconcile returns it in Replan. The new plan ends in a new request for
// approval, so no apply starts after a restart without a new decision. A work item in
// those states that has no layer to plan (a runbook, an incident, a debug session, a
// change with no pull request yet) fails, because nothing can continue it.
//
// It leaves a work item alone that waits for a person or for its window.
func (o *Orchestrator) Reconcile(ctx context.Context) (Reconciled, error) {
	var done Reconciled
	var err error
	if done.Jobs, err = o.endJobs(ctx); err != nil {
		return done, err
	}
	if done.Grants, err = o.revokeGrants(ctx); err != nil {
		return done, err
	}
	items, err := o.List()
	if err != nil {
		return done, err
	}
	for _, wi := range items {
		failure, replan := interrupted(wi)
		if replan {
			o.emit(audit.Event{Type: "workitem.resumed", Actor: "orchestrator", Env: wi.Env,
				WorkItem: wi.ID, Summary: fmt.Sprintf("%s: the control plane stopped while "+
					"this was %s. It is planned again", wi.Title, wi.State),
				Data: map[string]any{"state": wi.State}})
			done.Replan = append(done.Replan, wi.ID)
		}
		if failure == "" {
			continue
		}
		if err := o.setState(&wi, StateFailed, "orchestrator", failure); err != nil {
			return done, err
		}
		done.Failed = append(done.Failed, wi.ID)
	}
	return done, nil
}
