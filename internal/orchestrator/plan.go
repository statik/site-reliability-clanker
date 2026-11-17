package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/policy"
	"github.com/statik/site-reliability-clanker/internal/runner"
)

// LayersForPaths returns the layers of cloud that the changed paths touch, in apply order.
// A change to a shared module or to the chart touches the layer that uses it.
func LayersForPaths(cloud string, paths []string) []string {
	touched := map[string]bool{}
	stackPrefix := "platform/stacks/" + cloud + "/"
	for _, path := range paths {
		switch {
		case strings.HasPrefix(path, stackPrefix):
			layer, _, _ := strings.Cut(strings.TrimPrefix(path, stackPrefix), "/")
			touched[layer] = true
		case strings.HasPrefix(path, "platform/modules/shared/platform/"):
			touched["40-platform"] = true
		case strings.HasPrefix(path, "platform/modules/shared/app/"),
			strings.HasPrefix(path, "apps/paperboat/chart/"):
			touched["50-app"] = true
		}
	}
	layers := []string{}
	for _, layer := range Layers {
		if touched[layer] {
			layers = append(layers, layer)
		}
	}
	return layers
}

// AttachPull links a pull request to a work item and derives the layers from its diff.
func (o *Orchestrator) AttachPull(id string, prID int64, actor string) (WorkItem, error) {
	wi, err := o.Get(id)
	if err != nil {
		return WorkItem{}, err
	}
	pr, err := o.Forge.RefreshPull(prID)
	if err != nil {
		return WorkItem{}, err
	}
	paths, err := o.Forge.ChangedPaths(pr.Repo, pr.BaseCommit, pr.HeadCommit)
	if err != nil {
		return WorkItem{}, err
	}
	wi.Layers = LayersForPaths(o.Envs[wi.Env].Cloud, paths)
	if len(wi.Layers) == 0 {
		return WorkItem{}, fmt.Errorf("attach pull request %d to %s: the diff touches no layer "+
			"of environment %s", prID, id, wi.Env)
	}
	wi.PRID, wi.Commit, wi.BaseCommit = pr.ID, pr.HeadCommit, pr.BaseCommit
	if err := o.Forge.SetPullWorkItem(pr.ID, wi.ID); err != nil {
		return WorkItem{}, err
	}
	o.emit(audit.Event{Type: "pr.opened", Actor: pr.Author, Env: wi.Env, WorkItem: wi.ID,
		Summary: fmt.Sprintf("Pull request #%d: %s", pr.ID, pr.Title),
		Data: map[string]any{"pr": pr.ID, "repo": pr.Repo, "branch": pr.Branch,
			"commit": pr.HeadCommit, "paths": paths}})
	if err := o.setState(&wi, StateDrafting, actor, "pull request opened"); err != nil {
		return WorkItem{}, err
	}
	return wi, nil
}

func (o *Orchestrator) workDir(wi WorkItem) string {
	return filepath.Join(o.DataDir, "work", wi.ID, wi.Commit)
}

func (o *Orchestrator) checkout(wi WorkItem) (string, error) {
	dir := o.workDir(wi)
	if _, err := os.Stat(filepath.Join(dir, "platform")); err == nil {
		return dir, nil
	}
	if err := o.Forge.Export(PlatformRepo, wi.Commit, dir); err != nil {
		return "", err
	}
	return dir, nil
}

func (o *Orchestrator) policyDir() (string, error) {
	head, err := o.Forge.Head(RunbooksRepo, "main")
	if err != nil {
		return "", fmt.Errorf("find policies: %w", err)
	}
	dir := filepath.Join(o.DataDir, "work", "runbooks", head)
	if _, err := os.Stat(filepath.Join(dir, "policy")); err != nil {
		if err := o.Forge.Export(RunbooksRepo, head, dir); err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, "policy"), nil
}

// layerApplied reports whether the layer has state with outputs.
func (o *Orchestrator) layerApplied(env, layer string) bool {
	outputs, err := o.Runner.Outputs(env, layer)
	return err == nil && len(outputs) > 0
}

// canPlan reports whether the layer can be planned now. A layer reads the outputs of the
// layer below it, so the layer below must be applied first.
func (o *Orchestrator) canPlan(env, layer string) bool {
	index := slices.Index(Layers, layer)
	return index <= 0 || o.layerApplied(env, Layers[index-1])
}

func (o *Orchestrator) startJob(wi WorkItem, layer, kind, grantID string) (Job, *os.File, error) {
	dir := filepath.Join(o.DataDir, "logs", wi.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Job{}, nil, fmt.Errorf("create log directory %s: %w", dir, err)
	}
	job := Job{WorkItem: wi.ID, Layer: layer, Kind: kind, Status: "running", GrantID: grantID,
		ToolDigest: o.Runner.ToolDigest(), StartedAt: o.now()}
	err := o.DB.QueryRow(`INSERT INTO jobs (work_item, layer, kind, status, grant_id,
		tool_digest, started_at) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, job.WorkItem, job.Layer, job.Kind,
		job.Status, job.GrantID, job.ToolDigest, job.StartedAt).Scan(&job.ID)
	if err != nil {
		return Job{}, nil, fmt.Errorf("record job for %s %s: %w", wi.ID, layer, err)
	}
	job.LogPath = filepath.Join(dir, fmt.Sprintf("%04d-%s-%s.log", job.ID, kind, layer))
	logFile, err := os.Create(job.LogPath)
	if err != nil {
		return Job{}, nil, fmt.Errorf("create job log %s: %w", job.LogPath, err)
	}
	if _, err := o.DB.Exec(`UPDATE jobs SET log_path = $1 WHERE id = $2`, job.LogPath,
		job.ID); err != nil {
		logFile.Close()
		return Job{}, nil, fmt.Errorf("record log path of job %d: %w", job.ID, err)
	}
	return job, logFile, nil
}

func (o *Orchestrator) endJob(job Job, logFile *os.File, jobErr error) {
	status := "succeeded"
	if jobErr != nil {
		status = "failed"
		fmt.Fprintf(logFile, "\nJOB FAILED: %v\n", jobErr)
	}
	logFile.Close()
	if _, err := o.DB.Exec(`UPDATE jobs SET status = $1, ended_at = $2 WHERE id = $3`, status,
		o.now(), job.ID); err != nil {
		panic(fmt.Sprintf("job table is not writable, stopping: %v", err))
	}
}

// planCredential gets a read-only credential for a plan job. A layer that needs no cluster
// credential, or an environment without a cluster, gets an empty credential.
func (o *Orchestrator) planCredential(ctx context.Context, wi WorkItem, layer string) (
	broker.Grant, broker.Credential, error) {
	if !o.layerApplied(wi.Env, "20-cluster") || slices.Index(Layers, layer) < 3 {
		return broker.Grant{}, broker.Credential{}, nil
	}
	grant, err := o.Broker.Issue(broker.Request{WorkItem: wi.ID, Env: wi.Env, Layer: layer,
		Kind: broker.KindPlan, Tier: "T0", Duration: 15 * time.Minute})
	if err != nil {
		return broker.Grant{}, broker.Credential{}, err
	}
	cred, err := o.Broker.Exchange(ctx, grant, broker.Presentation{})
	return grant, cred, err
}

// planLayer runs plan and policy for one layer and stores the result.
func (o *Orchestrator) planLayer(ctx context.Context, wi WorkItem, layer string) (Plan, error) {
	checkout, err := o.checkout(wi)
	if err != nil {
		return Plan{}, err
	}
	grant, cred, err := o.planCredential(ctx, wi, layer)
	if err != nil {
		return Plan{}, err
	}
	if grant.ID != "" {
		defer func() {
			if err := o.Broker.Revoke(ctx, grant, "revoked"); err != nil {
				o.emit(audit.Event{Type: "grant.revoke_failed", Actor: "broker", Env: wi.Env,
					Layer: layer, WorkItem: wi.ID, Summary: err.Error()})
			}
		}()
	}
	job, logFile, err := o.startJob(wi, layer, "plan", grant.ID)
	if err != nil {
		return Plan{}, err
	}
	plan, err := o.runPlan(ctx, wi, layer, checkout, cred.KubeConfig, job, logFile)
	o.endJob(job, logFile, err)
	if err != nil {
		o.emit(audit.Event{Type: "plan.failed", Actor: "runner", Env: wi.Env, Layer: layer,
			WorkItem: wi.ID, Summary: fmt.Sprintf("Plan of %s failed", layer),
			Data: map[string]any{"job": job.ID, "error": err.Error()}})
		return Plan{}, err
	}
	return plan, nil
}

func (o *Orchestrator) runPlan(ctx context.Context, wi WorkItem, layer, checkout,
	kubeconfig string, job Job, logFile *os.File) (Plan, error) {
	outDir := filepath.Join(o.DataDir, "plans", wi.ID, fmt.Sprintf("job-%d", job.ID))
	result, err := o.Runner.Plan(ctx, runner.Job{Env: wi.Env, Cloud: o.Envs[wi.Env].Cloud,
		Layer: layer, Checkout: checkout, KubeConfig: kubeconfig, Log: logFile}, outDir)
	if err != nil {
		return Plan{}, err
	}
	facts, err := runner.ReadFacts(result.JSONPath)
	if err != nil {
		return Plan{}, err
	}
	policyDir, err := o.policyDir()
	if err != nil {
		return Plan{}, err
	}
	decision, err := policy.Evaluate(ctx, policy.Input{Kind: wi.Kind, Layer: layer,
		PlanJSONPath: result.JSONPath, PolicyDir: policyDir})
	if err != nil {
		return Plan{}, err
	}
	fmt.Fprintf(logFile, "\npolicy: tier %s by rule %s\n", decision.Tier, decision.Rule)
	plan := Plan{WorkItem: wi.ID, Layer: layer, Commit: wi.Commit, SHA256: result.SHA256,
		PlanPath: result.PlanPath, JSONPath: result.JSONPath, Facts: facts, Policy: decision,
		CreatedAt: o.now()}
	factsJSON, _ := json.Marshal(facts)
	decisionJSON, _ := json.Marshal(decision)
	if _, err := o.DB.Exec(`UPDATE plans SET superseded = 1 WHERE work_item = $1 AND layer = $2`,
		wi.ID, layer); err != nil {
		return Plan{}, fmt.Errorf("supersede old plans of %s %s: %w", wi.ID, layer, err)
	}
	err = o.DB.QueryRow(`INSERT INTO plans (work_item, layer, commit_sha, plan_sha256,
		plan_path, json_path, facts, policy, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		plan.WorkItem, plan.Layer, plan.Commit, plan.SHA256, plan.PlanPath, plan.JSONPath,
		string(factsJSON), string(decisionJSON), plan.CreatedAt).Scan(&plan.ID)
	if err != nil {
		return Plan{}, fmt.Errorf("record plan of %s %s: %w", wi.ID, layer, err)
	}
	o.emit(audit.Event{Type: "plan.created", Actor: "runner", Env: wi.Env, Layer: layer,
		WorkItem: wi.ID,
		Summary: fmt.Sprintf("Plan of %s: %d to add, %d to change, %d to destroy, %d to replace",
			layer, facts.Add, facts.Change, facts.Destroy, facts.Replace),
		Data: map[string]any{"job": job.ID, "commit": wi.Commit, "plan_sha256": plan.SHA256,
			"add": facts.Add, "change": facts.Change, "destroy": facts.Destroy,
			"replace": facts.Replace, "tier": decision.Tier, "rule": decision.Rule,
			"tool_digest": job.ToolDigest}})
	return plan, nil
}

// PlanWorkItem plans every layer of the work item that can be planned now, assigns the tier
// and moves the work item to AwaitingApproval. Layers that cannot be planned yet are planned
// at apply time and must then stay inside the create-only envelope.
func (o *Orchestrator) PlanWorkItem(ctx context.Context, id string) (WorkItem, error) {
	wi, err := o.Get(id)
	if err != nil {
		return WorkItem{}, err
	}
	unlock := o.lockEnv(wi.Env)
	defer unlock()
	if wi.Commit == "" {
		head, err := o.Forge.Head(PlatformRepo, "main")
		if err != nil {
			return WorkItem{}, err
		}
		wi.Commit, wi.BaseCommit = head, head
	}
	tier, rule := "T0", "no-changes"
	deferred := []string{}
	changes := false
	for _, layer := range wi.Layers {
		if !o.canPlan(wi.Env, layer) {
			deferred = append(deferred, layer)
			continue
		}
		plan, err := o.planLayer(ctx, wi, layer)
		if err != nil {
			if stateErr := o.setState(&wi, StateFailed, "runner", err.Error()); stateErr != nil {
				return WorkItem{}, stateErr
			}
			return wi, err
		}
		changes = changes || plan.Facts.HasChanges()
		if plan.Policy.Tier > tier {
			tier, rule = plan.Policy.Tier, plan.Policy.Rule
		}
	}
	if len(deferred) > 0 && tier < "T3" {
		tier, rule = "T3", "deferred-layers"
	}
	wi.Tier, wi.TierRule = tier, rule
	if err := o.setState(&wi, StatePlanned, "orchestrator", "plans saved"); err != nil {
		return WorkItem{}, err
	}
	o.emit(audit.Event{Type: "policy.evaluated", Actor: "policy", Env: wi.Env, WorkItem: wi.ID,
		Summary: fmt.Sprintf("%s is tier %s by rule %s", wi.Title, tier, rule),
		Data:    map[string]any{"tier": tier, "rule": rule, "deferred_layers": deferred}})
	if !changes && len(deferred) == 0 {
		err := o.setState(&wi, StateDone, "orchestrator", "the plans change nothing")
		return wi, err
	}
	o.explain(ctx, &wi)
	if err := o.setState(&wi, StateAwaitingApproval, "orchestrator",
		fmt.Sprintf("tier %s needs %d approvers", tier, policy.ApproversRequired(tier))); err != nil {
		return WorkItem{}, err
	}
	o.emit(audit.Event{Type: "approval.requested", Actor: "orchestrator", Env: wi.Env,
		WorkItem: wi.ID, Summary: "Approval needed: " + wi.Title,
		Data: map[string]any{"tier": tier, "rule": rule, "sentence": wi.Explanation.Sentence,
			"approvers_required": policy.ApproversRequired(tier), "title": wi.Title}})
	return wi, nil
}

func (o *Orchestrator) explain(ctx context.Context, wi *WorkItem) {
	plans, err := o.Plans(wi.ID, false)
	if err != nil {
		plans = nil
	}
	explanation := TemplateExplanation(*wi, plans)
	if o.Hooks.Explain != nil {
		written, err := o.Hooks.Explain(ctx, *wi, plans)
		if err != nil {
			o.emit(audit.Event{Type: "agent.failed", Actor: "explainer", Env: wi.Env,
				WorkItem: wi.ID, Summary: "Explainer failed; the page shows template text",
				Data: map[string]any{"error": err.Error()}})
		} else {
			explanation = written
		}
	}
	wi.Explanation = explanation
	wi.Summary = explanation.Sentence
}

// TemplateExplanation builds the approval text from plan facts alone, without a model.
func TemplateExplanation(wi WorkItem, plans []Plan) Explanation {
	add, change, destroy := 0, 0, 0
	for _, p := range plans {
		add += p.Facts.Add
		change += p.Facts.Change
		destroy += p.Facts.Destroy + p.Facts.Replace
	}
	return Explanation{
		Sentence: fmt.Sprintf("%s: %d resources to add, %d to change, %d to destroy or replace "+
			"in %s.", wi.Title, add, change, destroy, strings.Join(wi.Layers, ", ")),
		Why:          wi.Trigger,
		Downtime:     "Not estimated",
		UserEffect:   "Not estimated",
		CostChange:   "Not estimated",
		Duration:     "Not estimated",
		PreChecks:    []string{"The plan is saved and its hash is bound to the approval"},
		Backup:       "No backup step is part of this change",
		Rollback:     "Revert the pull request and apply the previous commit",
		Verification: "Health and version checks after the apply",
		IfDeferred:   "The change stays in the queue. Nothing runs without approval.",
		Source:       "template",
	}
}
