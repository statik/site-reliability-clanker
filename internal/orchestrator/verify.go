package orchestrator

import (
	"context"
	"fmt"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/forge"
)

// verify checks the environment after the apply. A failed check of a change that came from a
// pull request starts the rollback.
func (o *Orchestrator) verify(ctx context.Context, wi WorkItem, approvers []string) (WorkItem, error) {
	if err := o.setState(&wi, StateVerifying, "orchestrator", "apply finished"); err != nil {
		return WorkItem{}, err
	}
	result := VerifyResult{OK: true, Checks: []string{"no verification hook is configured"}}
	if o.Hooks.Verify != nil {
		var err error
		result, err = o.Hooks.Verify(ctx, wi)
		if err != nil {
			result = VerifyResult{OK: false, Reason: err.Error()}
		}
	}
	eventType, summary := "verify.passed", "Verification passed: "+wi.Title
	if !result.OK {
		eventType, summary = "verify.failed", "Verification failed: "+result.Reason
	}
	o.emit(audit.Event{Type: eventType, Actor: "verifier", Env: wi.Env, WorkItem: wi.ID,
		Summary: summary, Data: map[string]any{"checks": result.Checks, "reason": result.Reason}})
	if result.OK {
		if err := o.setState(&wi, StateDone, "orchestrator", "verified"); err != nil {
			return WorkItem{}, err
		}
		if o.Hooks.Applied != nil {
			o.Hooks.Applied(ctx, wi)
		}
		return wi, nil
	}
	if wi.PRID == 0 || wi.RollbackOf != "" {
		return wi, o.setState(&wi, StateFailed, "verifier", result.Reason)
	}
	if err := o.rollback(ctx, wi, approvers, result.Reason); err != nil {
		o.emit(audit.Event{Type: "rollback.failed", Actor: "orchestrator", Env: wi.Env,
			WorkItem: wi.ID, Summary: "Rollback failed: " + err.Error()})
		return wi, o.setState(&wi, StateFailed, "orchestrator",
			"verification failed and the rollback failed: "+err.Error())
	}
	if err := o.setState(&wi, StateRolledBack, "orchestrator", result.Reason); err != nil {
		return WorkItem{}, err
	}
	if o.Hooks.Applied != nil {
		o.Hooks.Applied(ctx, wi)
	}
	return wi, nil
}

// revertFiles returns the file content that restores the tree of base for every path that
// the change touched.
func (o *Orchestrator) revertFiles(wi WorkItem) (map[string]string, []string, error) {
	paths, err := o.Forge.ChangedPaths(PlatformRepo, wi.BaseCommit, wi.Commit)
	if err != nil {
		return nil, nil, err
	}
	files := map[string]string{}
	deletes := []string{}
	for _, path := range paths {
		content, err := o.Forge.ReadFile(PlatformRepo, wi.BaseCommit, path)
		if err != nil {
			deletes = append(deletes, path)
			continue
		}
		files[path] = content
	}
	return files, deletes, nil
}

// rollback restores the code from before the change with a new pull request, then plans and
// applies it. The approval of the original change covers its stated rollback, so the grant
// names the original approvers.
func (o *Orchestrator) rollback(ctx context.Context, original WorkItem, approvers []string,
	reason string) error {
	files, deletes, err := o.revertFiles(original)
	if err != nil {
		return err
	}
	branch := "rollback/" + original.ID
	if _, err := o.Forge.Commit(forge.CommitRequest{Repo: PlatformRepo, Branch: branch,
		Base: "main", Author: "clanker-orchestrator", Files: files, Deletes: deletes,
		Message: fmt.Sprintf("Roll back %s: %s", original.ID, original.Title)}); err != nil {
		return err
	}
	pr, err := o.Forge.OpenPull(forge.PullRequest{Repo: PlatformRepo, Branch: branch,
		Title:  fmt.Sprintf("Roll back %s: %s", original.ID, original.Title),
		Body:   "Automatic rollback. Verification failed: " + reason,
		Author: "clanker-orchestrator"}, o.now())
	if err != nil {
		return err
	}
	wi, err := o.Create(NewWorkItem{Title: "Roll back: " + original.Title, Kind: "rollback",
		Env: original.Env, Trigger: "Verification of " + original.ID + " failed: " + reason,
		Actor: "orchestrator"})
	if err != nil {
		return err
	}
	wi.RollbackOf = original.ID
	if err := o.save(wi); err != nil {
		return err
	}
	if wi, err = o.AttachPull(wi.ID, pr.ID, "orchestrator"); err != nil {
		return err
	}
	wi.RollbackOf = original.ID
	o.emit(audit.Event{Type: "rollback.authorized", Actor: "orchestrator", Env: wi.Env,
		WorkItem: wi.ID,
		Summary: fmt.Sprintf("Rollback of %s runs under the approval of the original change",
			original.ID),
		Data: map[string]any{"original": original.ID, "approvers": approvers, "reason": reason}})
	tier := "T0"
	plans := []Plan{}
	for _, layer := range wi.Layers {
		plan, err := o.planLayer(ctx, wi, layer)
		if err != nil {
			return err
		}
		if plan.Policy.Tier > tier {
			tier = plan.Policy.Tier
		}
		plans = append(plans, plan)
	}
	if tier > original.Tier {
		return fmt.Errorf("rollback of %s is tier %s, above tier %s of the original approval: "+
			"a human must approve it", original.ID, tier, original.Tier)
	}
	wi.Tier, wi.TierRule = original.Tier, "rollback-of-approved-change"
	wi.Explanation = TemplateExplanation(wi, plans)
	wi.Summary = fmt.Sprintf("Restores the state from before %s because verification failed.",
		original.ID)
	if err := o.setState(&wi, StateApplying, "orchestrator", "automatic rollback"); err != nil {
		return err
	}
	for _, plan := range plans {
		if !plan.Facts.HasChanges() {
			continue
		}
		if err := o.applyPlan(ctx, wi, plan, approvers); err != nil {
			if stateErr := o.setState(&wi, StateFailed, "runner", err.Error()); stateErr != nil {
				return stateErr
			}
			return err
		}
	}
	if _, err := o.Forge.MergePull(pr.ID, wi.Commit, "clanker-orchestrator", o.now()); err != nil {
		return err
	}
	return o.finishRollback(ctx, wi)
}

func (o *Orchestrator) finishRollback(ctx context.Context, wi WorkItem) error {
	if o.Hooks.Verify != nil {
		result, err := o.Hooks.Verify(ctx, wi)
		if err != nil || !result.OK {
			reason := result.Reason
			if err != nil {
				reason = err.Error()
			}
			o.emit(audit.Event{Type: "verify.failed", Actor: "verifier", Env: wi.Env,
				WorkItem: wi.ID, Summary: "Verification after rollback failed: " + reason})
			if stateErr := o.setState(&wi, StateFailed, "verifier", reason); stateErr != nil {
				return stateErr
			}
			return fmt.Errorf("verification after rollback failed: %s", reason)
		}
		o.emit(audit.Event{Type: "verify.passed", Actor: "verifier", Env: wi.Env,
			WorkItem: wi.ID, Summary: "Verification passed after rollback",
			Data: map[string]any{"checks": result.Checks}})
	}
	return o.setState(&wi, StateDone, "orchestrator", "rollback verified")
}
