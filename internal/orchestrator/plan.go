package orchestrator

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
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
	"github.com/statik/site-reliability-clanker/internal/planfacts"
	"github.com/statik/site-reliability-clanker/internal/policy"
	"github.com/statik/site-reliability-clanker/internal/store"
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

// layerJob returns a job for one layer of the work item, with the tree of its commit. The
// plan and the apply of one commit share one checkout on the runner.
func (o *Orchestrator) layerJob(wi WorkItem, kind, layer string) (jobspec.Job, error) {
	archive, err := o.Forge.Archive(PlatformRepo, wi.Commit)
	if err != nil {
		return jobspec.Job{}, err
	}
	return jobspec.Job{Spec: jobspec.Spec{Kind: kind, Env: wi.Env, Cloud: o.Envs[wi.Env].Cloud,
		Layer: layer, CheckoutID: wi.ID + "/" + wi.Commit, Variables: o.JobVariables},
		Checkout: archive}, nil
}

// run gives the job to a runner and records the tool versions that the runner reports.
func (o *Orchestrator) run(ctx context.Context, job *Job, spec jobspec.Job) (jobspec.Result, error) {
	result, err := o.Runners.Run(ctx, spec)
	if result.ToolDigest != "" {
		job.ToolDigest = result.ToolDigest
		if _, dbErr := o.DB.Exec(`UPDATE jobs SET tool_digest = $1 WHERE id = $2`,
			result.ToolDigest, job.ID); dbErr != nil && err == nil {
			err = fmt.Errorf("record the tools of job %d: %w", job.ID, dbErr)
		}
	}
	return result, err
}

// policyModules reads the policy rules from the runbooks repository.
func (o *Orchestrator) policyModules() (map[string]string, error) {
	head, err := o.Forge.Head(RunbooksRepo, "main")
	if err != nil {
		return nil, fmt.Errorf("find policies: %w", err)
	}
	entries, err := o.Forge.Tree(RunbooksRepo, head, "policy")
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	modules := map[string]string{}
	for _, entry := range entries {
		if entry.Type != "blob" {
			continue
		}
		source, err := o.Forge.ReadFile(RunbooksRepo, head, entry.Path)
		if err != nil {
			return nil, fmt.Errorf("read policy %s: %w", entry.Path, err)
		}
		modules[entry.Path] = source
	}
	return modules, nil
}

// layerApplied reports whether the layer was applied and has outputs.
func (o *Orchestrator) layerApplied(env, layer string) bool {
	outputs, err := store.LayerOutputs(o.DB, env, layer)
	return err == nil && len(outputs) > 0
}

// canPlan reports whether the layer can be planned now. A layer reads the outputs of the
// layer below it, so the layer below must be applied first.
func (o *Orchestrator) canPlan(env, layer string) bool {
	index := slices.Index(Layers, layer)
	return index <= 0 || o.layerApplied(env, Layers[index-1])
}

// jobLog is the log of one job. It appends to the job row while the job runs, so the
// dashboard can show a job that is still in flight.
type jobLog struct {
	db  *sql.DB
	job int64

	mu      sync.Mutex
	pending []byte
	flushed time.Time
}

// logFlushInterval bounds how often a busy job writes its log to the database.
const logFlushInterval = 400 * time.Millisecond

// Write keeps the text and stores it when enough time passed since the last store.
func (l *jobLog) Write(text []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pending = append(l.pending, text...)
	if time.Since(l.flushed) < logFlushInterval {
		return len(text), nil
	}
	return len(text), l.flush()
}

// flush stores the pending text. The caller holds the lock.
func (l *jobLog) flush() error {
	if len(l.pending) == 0 {
		return nil
	}
	// PostgreSQL does not store a zero byte in a text column.
	text := strings.ReplaceAll(strings.ToValidUTF8(string(l.pending), "?"), "\x00", "")
	if _, err := l.db.Exec(`UPDATE jobs SET log = log || $1 WHERE id = $2`, text, l.job); err != nil {
		return fmt.Errorf("store log of job %d: %w", l.job, err)
	}
	l.pending, l.flushed = nil, time.Now()
	return nil
}

// Close stores what is left.
func (l *jobLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.flush()
}

func (o *Orchestrator) startJob(wi WorkItem, layer, kind, grantID string) (Job, *jobLog, error) {
	job := Job{WorkItem: wi.ID, Layer: layer, Kind: kind, Status: "running", GrantID: grantID,
		StartedAt: o.now()}
	err := o.DB.QueryRow(`INSERT INTO jobs (work_item, layer, kind, status, grant_id,
		tool_digest, started_at) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, job.WorkItem, job.Layer, job.Kind,
		job.Status, job.GrantID, job.ToolDigest, job.StartedAt).Scan(&job.ID)
	if err != nil {
		return Job{}, nil, fmt.Errorf("record job for %s %s: %w", wi.ID, layer, err)
	}
	return job, &jobLog{db: o.DB, job: job.ID}, nil
}

func (o *Orchestrator) endJob(job Job, log *jobLog, jobErr error) {
	status := "succeeded"
	if jobErr != nil {
		status = "failed"
		fmt.Fprintf(log, "\nJOB FAILED: %v\n", jobErr)
	}
	if err := log.Close(); err != nil {
		panic(fmt.Sprintf("job table is not writable, stopping: %v", err))
	}
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
	spec, err := o.layerJob(wi, jobspec.KindPlan, layer)
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
	spec.KubeConfig, spec.Log = cred.KubeConfig, logFile
	plan, err := o.runPlan(ctx, wi, layer, spec, &job, logFile)
	o.endJob(job, logFile, err)
	if err != nil {
		o.emit(audit.Event{Type: "plan.failed", Actor: "runner", Env: wi.Env, Layer: layer,
			WorkItem: wi.ID, Summary: fmt.Sprintf("Plan of %s failed", layer),
			Data: map[string]any{"job": job.ID, "error": err.Error()}})
		return Plan{}, err
	}
	return plan, nil
}

func (o *Orchestrator) runPlan(ctx context.Context, wi WorkItem, layer string,
	spec jobspec.Job, job *Job, logFile *jobLog) (Plan, error) {
	result, err := o.run(ctx, job, spec)
	if err != nil {
		return Plan{}, err
	}
	// The hash that an approver agrees to is the hash of the bytes that are stored here,
	// and an apply gets exactly these bytes.
	sha := jobspec.SHA256(result.Plan)
	if len(result.Plan) == 0 || sha != result.PlanSHA256 {
		return Plan{}, fmt.Errorf("plan of %s: the runner reported hash %q for a plan of %d "+
			"bytes with hash %s", layer, result.PlanSHA256, len(result.Plan), sha)
	}
	facts, err := planfacts.Parse(result.PlanJSON)
	if err != nil {
		return Plan{}, err
	}
	modules, err := o.policyModules()
	if err != nil {
		return Plan{}, err
	}
	decision, err := policy.Evaluate(ctx, policy.Input{Kind: wi.Kind, Layer: layer,
		PlanJSON: result.PlanJSON, Modules: modules})
	if err != nil {
		return Plan{}, err
	}
	fmt.Fprintf(logFile, "\npolicy: tier %s by rule %s\n", decision.Tier, decision.Rule)
	plan := Plan{WorkItem: wi.ID, Layer: layer, Commit: wi.Commit, SHA256: sha,
		Facts: facts, Policy: decision, CreatedAt: o.now()}
	factsJSON, _ := json.Marshal(facts)
	decisionJSON, _ := json.Marshal(decision)
	if _, err := o.DB.Exec(`UPDATE plans SET superseded = 1 WHERE work_item = $1 AND layer = $2`,
		wi.ID, layer); err != nil {
		return Plan{}, fmt.Errorf("supersede old plans of %s %s: %w", wi.ID, layer, err)
	}
	err = o.DB.QueryRow(`INSERT INTO plans (work_item, layer, commit_sha, plan_sha256,
		plan, plan_json, facts, policy, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		plan.WorkItem, plan.Layer, plan.Commit, plan.SHA256, result.Plan, result.PlanJSON,
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
	if changes || len(deferred) > 0 {
		tier, rule = o.review(ctx, wi, tier, rule)
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

// ReviewRule is the tier rule of a work item whose tier the adversarial review made higher.
const ReviewRule = "adversarial-review"

// review runs the adversarial review of a planned change and returns the tier and the
// rule of the work item. The review can make the tier of the policy engine higher. It can
// never make it lower: a model that is told, or tricked, to call a change safe changes
// nothing. When the review fails, the tier of the policy engine stands and the failure is
// in the audit log.
func (o *Orchestrator) review(ctx context.Context, wi WorkItem, tier, rule string) (string, string) {
	if o.Hooks.Review == nil {
		return tier, rule
	}
	plans, err := o.Plans(wi.ID, false)
	if err != nil {
		plans = nil
	}
	review, err := o.Hooks.Review(ctx, wi, plans, tier)
	if err != nil {
		o.emit(audit.Event{Type: "agent.failed", Actor: "reviewer", Env: wi.Env, WorkItem: wi.ID,
			Summary: "The adversarial review failed; the tier of the policy engine stands",
			Data:    map[string]any{"error": err.Error(), "policy_tier": tier}})
		return tier, rule
	}
	if review.Tier == "" {
		return tier, rule
	}
	final, finalRule := tier, rule
	summary := fmt.Sprintf("Adversarial review agrees with tier %s of the policy engine", tier)
	if review.Tier > tier {
		final, finalRule = review.Tier, ReviewRule
		summary = fmt.Sprintf("Adversarial review raised the tier from %s to %s", tier, final)
	}
	o.emit(audit.Event{Type: "review.completed", Actor: "reviewer", Env: wi.Env, WorkItem: wi.ID,
		Summary: summary, Data: map[string]any{"policy_tier": tier, "policy_rule": rule,
			"proposed_tier": review.Tier, "tier": final, "concerns": review.Concerns,
			"model": review.Source}})
	return final, finalRule
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
