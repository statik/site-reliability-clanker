package orchestrator

import (
	"context"
	"database/sql"
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
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/planfacts"
	"github.com/statik/site-reliability-clanker/internal/policy"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// LayersForPaths returns the layers of cloud that the changed paths touch, in apply order.
// A change to a shared module or to a deploy file of the application pack (its directory
// apps/<name>/ of the platform repository) touches the layer that uses it. On a cloud a
// change to layer 00-bootstrap is an error: an operator applies that layer with `clanker
// bootstrap`, and no agent role can apply it.
func LayersForPaths(cloud string, app pack.Pack, paths []string) ([]string, error) {
	touched := map[string]bool{}
	appDir, appLayer := app.PlatformDir()+"/", app.Layer
	stackPrefix := "platform/stacks/" + cloud + "/"
	for _, path := range paths {
		switch {
		case strings.HasPrefix(path, stackPrefix):
			layer, _, _ := strings.Cut(strings.TrimPrefix(path, stackPrefix), "/")
			touched[layer] = true
		case strings.HasPrefix(path, "platform/modules/shared/platform/"):
			touched["40-platform"] = true
		case strings.HasPrefix(path, "platform/modules/shared/app/"),
			strings.HasPrefix(path, appDir):
			touched[appLayer] = true
		}
	}
	bootstrap := Layers[0]
	if touched[bootstrap] && (EnvConfig{Cloud: cloud}).OnCloud() {
		return nil, fmt.Errorf("the change touches layer %s of cloud %s, which the control "+
			"plane does not apply: there is no applier role for it. Apply it with `clanker "+
			"bootstrap --cloud %s`, and give its new outputs to the control plane", bootstrap,
			cloud, cloud)
	}
	layers := []string{}
	for _, layer := range Layers {
		if touched[layer] {
			layers = append(layers, layer)
		}
	}
	return layers, nil
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
	wi.Layers, err = LayersForPaths(o.Envs[wi.Env].Cloud, o.App, paths)
	if err != nil {
		return WorkItem{}, fmt.Errorf("attach pull request %d to %s: %w", prID, id, err)
	}
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
// plan and the apply of one commit share one checkout on the runner. On a cloud the job
// names its state in the backend of the environment and gives the layer the common inputs
// of the platform contract. On the local target the runner keeps the state and the
// terraform.tfvars files of the stacks hold those inputs.
func (o *Orchestrator) layerJob(wi WorkItem, kind, layer string) (jobspec.Job, error) {
	env := o.Envs[wi.Env]
	spec := jobspec.Spec{Kind: kind, Env: wi.Env, Cloud: env.Cloud, Layer: layer,
		CheckoutID: wi.ID + "/" + wi.Commit, Variables: o.JobVariables}
	if env.OnCloud() {
		if len(env.Backend) == 0 {
			return jobspec.Job{}, fmt.Errorf("%s of environment %s on %s: the control plane "+
				"has no state backend for the environment. Give it the outputs of layer "+
				"00-bootstrap", layer, wi.Env, env.Cloud)
		}
		spec.Backend, spec.Variables = cloudState(env, layer, o.JobVariables)
	}
	archive, err := o.Forge.Archive(PlatformRepo, wi.Commit)
	if err != nil {
		return jobspec.Job{}, err
	}
	return jobspec.Job{Spec: spec, Checkout: archive}, nil
}

// cloudState returns the backend settings and the variables of a job for a layer of an
// environment on a cloud. The state key and the inputs are those of the platform contract:
// the layers above read layer 00-bootstrap at <env>/00-bootstrap.tfstate in the bucket
// state_location of the region. The variables of the account of the environment come
// between the shared variables and the inputs of the contract.
func cloudState(env EnvConfig, layer string, shared map[string]string) (
	map[string]string, map[string]string) {
	backend := maps.Clone(env.Backend)
	backend["key"] = env.Name + "/" + layer + ".tfstate"
	variables := maps.Clone(shared)
	if variables == nil {
		variables = map[string]string{}
	}
	maps.Copy(variables, env.Variables)
	variables["TF_VAR_env_name"] = env.Name
	variables["TF_VAR_state_location"] = env.Backend["bucket"]
	variables["TF_VAR_region"] = env.Region
	return backend, variables
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

// ApprovalSettings reads the approval settings from the runbooks repository of f. A repository
// that has no settings file gives the settings of policy.Approvals with no file. A file
// that is not valid, and each other failure to read, is an error.
func ApprovalSettings(f *forge.Forge) (policy.Approvals, error) {
	source, err := f.ReadFile(RunbooksRepo, "main", policy.ApprovalsPath)
	if errors.Is(err, forge.ErrFileNotFound) {
		return policy.Approvals{}, nil
	}
	if err != nil {
		return policy.Approvals{}, fmt.Errorf("read the approval settings: %w", err)
	}
	return policy.ParseApprovals([]byte(source))
}

// ApproversRequired is the number of distinct approvers that a change of the tier needs
// now. It reads the approval settings at each call.
func (o *Orchestrator) ApproversRequired(tier string) (int, error) {
	approvals, err := ApprovalSettings(o.Forge)
	if err != nil {
		return 0, err
	}
	return approvals.Required(tier), nil
}

// MaintenanceWindows reads the maintenance windows from the runbooks repository of f. A
// repository that has no windows file has no windows. A file that is not valid, and each
// other failure to read, is an error.
func MaintenanceWindows(f *forge.Forge) (policy.Windows, error) {
	source, err := f.ReadFile(RunbooksRepo, "main", policy.WindowsPath)
	if errors.Is(err, forge.ErrFileNotFound) {
		return policy.Windows{}, nil
	}
	if err != nil {
		return policy.Windows{}, fmt.Errorf("read the maintenance windows: %w", err)
	}
	return policy.ParseWindows([]byte(source))
}

// windowsFor returns the maintenance windows that the work item must wait for now.
// Only tier T3 waits for a window. The first deployment of an environment does not
// wait: nothing runs there yet, so no user can see an interruption. It reads the
// windows file at each call.
func (o *Orchestrator) windowsFor(wi WorkItem) (policy.Windows, error) {
	if wi.Tier != "T3" || wi.Kind == KindBlankSlate {
		return policy.Windows{}, nil
	}
	return MaintenanceWindows(o.Forge)
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

// planCredential gets a read-only credential for a plan job. On a cloud with an issuer
// every plan gets one: OpenTofu reads the state and the resources of the cloud for each
// layer. On the local target a layer that needs no cluster credential, or an environment
// without a cluster, gets an empty credential.
func (o *Orchestrator) planCredential(ctx context.Context, wi WorkItem, layer string) (
	broker.Grant, broker.Credential, error) {
	cloudIssuer := o.Envs[wi.Env].OnCloud() && o.Broker.HasIssuer()
	inCluster := o.layerApplied(wi.Env, "20-cluster") && slices.Index(Layers, layer) >= 3
	if !cloudIssuer && !inCluster {
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
	spec.KubeConfig, spec.AWS, spec.Log = cred.KubeConfig, cred.AWS, logFile
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
	if layer == o.App.Layer {
		if facts.Warnings, err = o.versionWarnings(wi, result.PlanJSON); err != nil {
			return Plan{}, fmt.Errorf("plan of %s: %w", layer, err)
		}
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
			"replace": facts.Replace, "outputs": facts.Outputs, "tier": decision.Tier,
			"rule": decision.Rule, "tool_digest": job.ToolDigest}})
	return plan, nil
}

// pinCommit gives a work item that has no commit yet the head of the main branch.
func (o *Orchestrator) pinCommit(wi *WorkItem) error {
	if wi.Commit != "" {
		return nil
	}
	head, err := o.Forge.Head(PlatformRepo, "main")
	if err != nil {
		return err
	}
	wi.Commit, wi.BaseCommit = head, head
	return nil
}

// planned is the result of the plans of a work item.
type planned struct {
	// tier and rule are the highest tier of the policy engine over the plans.
	tier, rule string
	// deferred are the layers that cannot be planned before the layer below them exists.
	deferred []string
	// changes is set when a plan changes a resource or an output.
	changes bool
}

// planLayers plans every layer of the work item that can be planned now.
func (o *Orchestrator) planLayers(ctx context.Context, wi WorkItem) (planned, error) {
	result := planned{tier: "T0", rule: "no-changes", deferred: []string{}}
	for _, layer := range wi.Layers {
		if !o.canPlan(wi.Env, layer) {
			result.deferred = append(result.deferred, layer)
			continue
		}
		plan, err := o.planLayer(ctx, wi, layer)
		if err != nil {
			return planned{}, err
		}
		result.changes = result.changes || plan.Facts.HasChanges()
		if plan.Policy.Tier > result.tier {
			result.tier, result.rule = plan.Policy.Tier, plan.Policy.Rule
		}
	}
	if len(result.deferred) > 0 && result.tier < "T3" {
		result.tier, result.rule = "T3", "deferred-layers"
	}
	return result, nil
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
	if err := o.pinCommit(&wi); err != nil {
		return WorkItem{}, err
	}
	planned, err := o.planLayers(ctx, wi)
	if err != nil {
		if stateErr := o.setState(&wi, StateFailed, "runner", err.Error()); stateErr != nil {
			return WorkItem{}, stateErr
		}
		return wi, err
	}
	tier, rule, deferred, changes := planned.tier, planned.rule, planned.deferred, planned.changes
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
	return o.requestApproval(ctx, wi, false)
}

// requestApproval writes the explanation of a planned work item and asks for the approvals
// that its tier needs. again is true when a new plan replaced an approved plan. A work item
// whose number of approvers cannot be read fails: nobody can approve it.
func (o *Orchestrator) requestApproval(ctx context.Context, wi WorkItem, again bool) (
	WorkItem, error) {
	need, err := o.ApproversRequired(wi.Tier)
	if err != nil {
		return wi, o.setState(&wi, StateFailed, "orchestrator", err.Error())
	}
	o.explain(ctx, &wi)
	reason := fmt.Sprintf("tier %s needs %d approvers", wi.Tier, need)
	summary := "Approval needed: " + wi.Title
	if again {
		reason = "the plan changed; earlier approvals do not cover the new plan"
		summary = "Approval needed again: " + wi.Title
	}
	if err := o.setState(&wi, StateAwaitingApproval, "orchestrator", reason); err != nil {
		return WorkItem{}, err
	}
	o.emit(audit.Event{Type: "approval.requested", Actor: "orchestrator", Env: wi.Env,
		WorkItem: wi.ID, Summary: summary,
		Data: map[string]any{"tier": wi.Tier, "rule": wi.TierRule,
			"sentence": wi.Explanation.Sentence, "approvers_required": need, "title": wi.Title}})
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
	add, change, destroy, outputs := 0, 0, 0, 0
	for _, p := range plans {
		add += p.Facts.Add
		change += p.Facts.Change
		destroy += p.Facts.Destroy + p.Facts.Replace
		outputs += len(p.Facts.Outputs)
	}
	sentence := fmt.Sprintf("%s: %d resources to add, %d to change, %d to destroy or replace "+
		"in %s.", wi.Title, add, change, destroy, strings.Join(wi.Layers, ", "))
	if outputs > 0 {
		sentence += fmt.Sprintf(" %d outputs change.", outputs)
	}
	return Explanation{
		Sentence:     sentence,
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
