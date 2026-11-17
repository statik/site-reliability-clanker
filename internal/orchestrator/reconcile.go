package orchestrator

import (
	"context"
	"fmt"

	"github.com/statik/site-reliability-clanker/internal/audit"
)

// Reconciled says what Reconcile ended.
type Reconciled struct {
	Jobs      int
	Grants    int
	WorkItems []string
}

// Reconcile ends the work that a stop of the control plane interrupted. Call it once at
// start, before anything else runs, and only when no other control plane writes.
//
// A job, a grant or an apply that was in progress has no process behind it any more.
// Reconcile ends each running job as lost, revokes each grant that did not end, and fails
// each work item that was in an apply or a verification. It does not continue an apply:
// the environment can be partly changed, and the saved plan no longer describes it.
//
// It leaves a work item alone that was not yet approved or not yet in the apply.
func (o *Orchestrator) Reconcile(ctx context.Context) (Reconciled, error) {
	var done Reconciled
	result, err := o.DB.ExecContext(ctx, `UPDATE jobs SET status = 'lost', ended_at = $1,
		log = log || $2 WHERE status = 'running'`, o.now(),
		"\nJOB LOST: the control plane stopped while this job ran\n")
	if err != nil {
		return done, fmt.Errorf("end the jobs that a stop interrupted: %w", err)
	}
	lost, err := result.RowsAffected()
	if err != nil {
		return done, fmt.Errorf("count the jobs that a stop interrupted: %w", err)
	}
	done.Jobs = int(lost)
	grants, err := o.Broker.Unended()
	if err != nil {
		return done, err
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
		done.Grants++
	}
	items, err := o.List()
	if err != nil {
		return done, err
	}
	for _, wi := range items {
		phase := map[string]string{StateApplying: "apply", StateVerifying: "verification"}[wi.State]
		if phase == "" {
			continue
		}
		reason := fmt.Sprintf("the control plane stopped during the %s. The environment can be "+
			"partly changed", phase)
		if err := o.setState(&wi, StateFailed, "orchestrator", reason); err != nil {
			return done, err
		}
		done.WorkItems = append(done.WorkItems, wi.ID)
	}
	return done, nil
}
