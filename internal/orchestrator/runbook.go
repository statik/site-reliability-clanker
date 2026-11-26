package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/runbook"
	"github.com/statik/site-reliability-clanker/internal/scrub"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// standingKey holds the user who signed the standing approval of a runbook, and
// standingTimeKey the time of the signature.
func standingKey(name string) string     { return "standing_approval." + name }
func standingTimeKey(name string) string { return "standing_approval_at." + name }

// StandingApproval is the signature of a person that covers every future run of a runbook.
// Signer and SignedAt are empty when nobody signed.
type StandingApproval struct {
	Runbook  string `json:"runbook"`
	Signer   string `json:"signer"`
	SignedAt string `json:"signed_at"`
}

// standingSigner returns the user behind a signature or a revocation of a standing
// approval, who must be an approver or an admin.
func (o *Orchestrator) standingSigner(user, action string) (User, error) {
	u, err := o.GetUser(user)
	if err != nil {
		return User{}, fmt.Errorf("%w: %v", ErrForbidden, err)
	}
	if u.Role != "approver" && u.Role != "admin" {
		return User{}, fmt.Errorf("%w: %s has role %s and cannot %s a standing approval; only "+
			"an approver or admin can", ErrForbidden, u.Name, u.Role, action)
	}
	return u, nil
}

// SignStandingApproval records that a human approves every future run of a tier T1 runbook.
// A new signature replaces the one before it.
func (o *Orchestrator) SignStandingApproval(name, user string) error {
	u, err := o.standingSigner(user, "sign")
	if err != nil {
		return err
	}
	if _, _, err := o.standingRunbook(name); err != nil {
		return err
	}
	tx, err := o.DB.Begin()
	if err != nil {
		return fmt.Errorf("sign the standing approval of %s: %w", name, err)
	}
	defer tx.Rollback() //nolint:errcheck // a rollback after the commit does nothing
	for key, value := range map[string]string{standingKey(name): u.Name,
		standingTimeKey(name): o.now()} {
		if _, err := tx.Exec(`INSERT INTO kv(key, value) VALUES($1, $2)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			return fmt.Errorf("sign the standing approval of %s: %w", name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sign the standing approval of %s: %w", name, err)
	}
	o.emit(audit.Event{Type: "standing_approval.signed", Actor: u.Name,
		Summary: fmt.Sprintf("%s signed a standing approval for runbook %s", u.Display, name),
		Data:    map[string]any{"runbook": name, "tier": runbook.StandingTier}})
	return nil
}

// RevokeStandingApproval removes the standing approval of a runbook. A run that started
// before goes on; no run starts after it until somebody signs again. Any approver or admin
// can revoke a signature, also the signature of another person. The runbook needs no
// definition, so the approval of a runbook that was removed from the repository can go too.
func (o *Orchestrator) RevokeStandingApproval(name, user string) error {
	u, err := o.standingSigner(user, "revoke")
	if err != nil {
		return err
	}
	if !runbook.ValidName(name) {
		return fmt.Errorf("%w: runbook name %q is not valid", ErrInvalid, name)
	}
	tx, err := o.DB.Begin()
	if err != nil {
		return fmt.Errorf("revoke the standing approval of %s: %w", name, err)
	}
	defer tx.Rollback() //nolint:errcheck // a rollback after the commit does nothing
	var signer string
	err = tx.QueryRow(`DELETE FROM kv WHERE key = $1 RETURNING value`, standingKey(name)).Scan(
		&signer)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: runbook %s has no standing approval to revoke", ErrConflict, name)
	}
	if err != nil {
		return fmt.Errorf("revoke the standing approval of %s: %w", name, err)
	}
	if _, err := tx.Exec(`DELETE FROM kv WHERE key = $1`, standingTimeKey(name)); err != nil {
		return fmt.Errorf("revoke the standing approval of %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("revoke the standing approval of %s: %w", name, err)
	}
	o.emit(audit.Event{Type: "standing_approval.revoked", Actor: u.Name,
		Summary: fmt.Sprintf("%s revoked the standing approval that %s signed for runbook %s",
			u.Display, signer, name),
		Data: map[string]any{"runbook": name, "signer": signer}})
	return nil
}

// StandingApprover returns who signed the standing approval for a runbook, or "".
func (o *Orchestrator) StandingApprover(name string) (string, error) {
	return store.GetKV(o.DB, standingKey(name))
}

// StandingApproval returns the standing approval of a runbook. SignedAt is empty for a
// signature from before the time was recorded.
func (o *Orchestrator) StandingApproval(name string) (StandingApproval, error) {
	signer, err := o.StandingApprover(name)
	if err != nil || signer == "" {
		return StandingApproval{Runbook: name}, err
	}
	at, err := store.GetKV(o.DB, standingTimeKey(name))
	return StandingApproval{Runbook: name, Signer: signer, SignedAt: at}, err
}

// runbookDefinition reads the definition of a runbook at a commit of the runbooks
// repository.
func (o *Orchestrator) runbookDefinition(commit, name string) (runbook.Definition, error) {
	if !runbook.ValidName(name) {
		return runbook.Definition{}, fmt.Errorf("%w: runbook name %q is not valid", ErrInvalid,
			name)
	}
	raw, err := o.Forge.ReadFile(RunbooksRepo, commit, "jobs/"+name+".json")
	if err != nil {
		return runbook.Definition{}, fmt.Errorf("%w: runbook %s has no definition jobs/%s.json "+
			"in the runbooks repository at %s", ErrInvalid, name, name, commit[:8])
	}
	return runbook.Parse(name, []byte(raw))
}

// Runbooks returns the definitions of all runbooks at the head of main, in the order of
// their names.
func (o *Orchestrator) Runbooks() ([]runbook.Definition, error) {
	head, err := o.Forge.Head(RunbooksRepo, "main")
	if err != nil {
		return nil, fmt.Errorf("find runbooks: %w", err)
	}
	entries, err := o.Forge.Tree(RunbooksRepo, head, "jobs")
	if err != nil {
		return nil, fmt.Errorf("list runbooks: %w", err)
	}
	definitions := []runbook.Definition{}
	for _, entry := range entries {
		name, isDefinition := strings.CutSuffix(entry.Name, ".json")
		if entry.Type != "blob" || !isDefinition {
			continue
		}
		definition, err := o.runbookDefinition(head, name)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(a, b int) bool {
		return definitions[a].Name < definitions[b].Name
	})
	return definitions, nil
}

// standingRunbook returns the definition of a runbook at the head of main, and that
// commit. It refuses a runbook that a standing approval cannot cover.
func (o *Orchestrator) standingRunbook(name string) (runbook.Definition, string, error) {
	head, err := o.Forge.Head(RunbooksRepo, "main")
	if err != nil {
		return runbook.Definition{}, "", fmt.Errorf("find runbooks: %w", err)
	}
	definition, err := o.runbookDefinition(head, name)
	if err != nil {
		return runbook.Definition{}, "", err
	}
	if definition.Tier != runbook.StandingTier {
		return runbook.Definition{}, "", fmt.Errorf("%w: runbook %s needs tier %s, and a "+
			"standing approval covers only tier %s", ErrInvalid, name, definition.Tier,
			runbook.StandingTier)
	}
	return definition, head, nil
}

// CheckRunbook checks a request against the definition of the runbook at the head of main:
// the runbook must exist, a standing approval must be able to cover it, and the
// parameters must be exactly its inputs with permitted values. It changes nothing.
func (o *Orchestrator) CheckRunbook(name string, params map[string]string) (
	runbook.Definition, error) {
	definition, _, err := o.checkRunbook(name, params)
	return definition, err
}

func (o *Orchestrator) checkRunbook(name string, params map[string]string) (
	runbook.Definition, string, error) {
	definition, commit, err := o.standingRunbook(name)
	if err != nil {
		return runbook.Definition{}, "", err
	}
	if err := definition.CheckInputs(params); err != nil {
		return runbook.Definition{}, "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return definition, commit, nil
}

// runbookJob returns the job for a checked request at a commit of the runbooks repository.
// The job has the targets of the environment, so a script names no namespace.
func (o *Orchestrator) runbookJob(req RunbookRequest, commit string) (jobspec.Job, error) {
	if _, err := o.Forge.ReadFile(RunbooksRepo, commit, "jobs/"+req.Name+".sh"); err != nil {
		return jobspec.Job{}, fmt.Errorf("runbook %s has a definition and no script jobs/%s.sh "+
			"in the runbooks repository at %s", req.Name, req.Name, commit[:8])
	}
	if _, configured := o.Envs[req.Env]; !configured {
		return jobspec.Job{}, fmt.Errorf("runbook %s: environment %q is not configured",
			req.Name, req.Env)
	}
	targets, err := o.Targets(req.Env)
	if err != nil {
		return jobspec.Job{}, fmt.Errorf("runbook %s: %w", req.Name, err)
	}
	archive, err := o.Forge.Archive(RunbooksRepo, commit)
	if err != nil {
		return jobspec.Job{}, err
	}
	return jobspec.Job{Spec: jobspec.Spec{Kind: jobspec.KindRunbook, Env: req.Env,
		CheckoutID: "runbooks/" + commit, Runbook: req.Name, Params: req.Params,
		Namespace: targets.Namespace, DataNamespace: targets.DataNamespace},
		Checkout: archive}, nil
}

// RunbookRequest names a runbook job to run.
type RunbookRequest struct {
	Env     string
	Name    string
	Params  map[string]string
	Actor   string
	Trigger string
	Title   string
}

// RunRunbook runs a recorded, parameterised job under a standing approval. A runbook is tier
// T1: routine and reversible. Each use is logged and has its own short-lived grant. The
// request is checked against the definition of the runbook before a work item or a grant
// exists.
func (o *Orchestrator) RunRunbook(ctx context.Context, req RunbookRequest) (WorkItem, error) {
	definition, commit, err := o.checkRunbook(req.Name, req.Params)
	if err != nil {
		return WorkItem{}, err
	}
	approver, err := o.StandingApprover(req.Name)
	if err != nil {
		return WorkItem{}, err
	}
	if approver == "" {
		return WorkItem{}, fmt.Errorf("runbook %s has no standing approval: an approver must "+
			"sign one first", req.Name)
	}
	job, err := o.runbookJob(req, commit)
	if err != nil {
		return WorkItem{}, err
	}
	if req.Title == "" {
		req.Title = "Runbook: " + req.Name
	}
	wi, err := o.Create(NewWorkItem{Title: req.Title, Kind: "runbook:" + req.Name, Env: req.Env,
		Trigger: req.Trigger, Actor: req.Actor})
	if err != nil {
		return WorkItem{}, err
	}
	unlock := o.lockEnv(req.Env)
	defer unlock()
	wi.Tier, wi.TierRule, wi.Commit = definition.Tier, "standing-approval", commit
	wi.Summary = fmt.Sprintf("Runs the %s runbook under the standing approval that %s signed.",
		req.Name, approver)
	return o.runApprovedRunbook(ctx, wi, definition, job, approver)
}

// runApprovedRunbook moves the work item of a runbook from the approval to its end.
func (o *Orchestrator) runApprovedRunbook(ctx context.Context, wi WorkItem,
	definition runbook.Definition, job jobspec.Job, approver string) (WorkItem, error) {
	if err := o.setState(&wi, StateApproved, "orchestrator",
		"covered by the standing approval of "+approver); err != nil {
		return WorkItem{}, err
	}
	if err := o.setState(&wi, StateApplying, "orchestrator", "runbook started"); err != nil {
		return WorkItem{}, err
	}
	if runErr := o.execRunbook(ctx, wi, definition, job, "standing:"+approver); runErr != nil {
		return wi, o.setState(&wi, StateFailed, "runner", runErr.Error())
	}
	if err := o.setState(&wi, StateDone, "orchestrator", "runbook finished"); err != nil {
		return WorkItem{}, err
	}
	if o.Hooks.Applied != nil {
		o.Hooks.Applied(ctx, wi)
	}
	return wi, nil
}

// runbookGrant issues the short-lived grant of one run and records it.
func (o *Orchestrator) runbookGrant(wi WorkItem, name, approver string) (broker.Grant, error) {
	grant, err := o.Broker.Issue(broker.Request{WorkItem: wi.ID, Env: wi.Env, Layer: "runbook",
		Kind: broker.KindRunbook, Commit: wi.Commit, Tier: wi.Tier, Approvers: []string{approver},
		Duration: 15 * time.Minute})
	if err != nil {
		return broker.Grant{}, err
	}
	o.emit(audit.Event{Type: "grant.issued", Actor: "broker", Env: wi.Env, WorkItem: wi.ID,
		Summary: fmt.Sprintf("Grant %s: runbook %s until %s", grant.ID, name, grant.Expires),
		Data: map[string]any{"grant": grant.ID, "identity": grant.Identity, "kind": grant.Kind,
			"tier": wi.Tier, "approvers": grant.Approvers, "expires": grant.Expires,
			"not_before": grant.NotBefore}})
	return grant, nil
}

// endRunbookGrant ends the grant of a run and records it.
func (o *Orchestrator) endRunbookGrant(ctx context.Context, wi WorkItem, grant broker.Grant,
	runErr error) {
	outcome := "used"
	if runErr != nil {
		outcome = "revoked"
	}
	if err := o.Broker.Revoke(ctx, grant, outcome); err != nil {
		o.emit(audit.Event{Type: "grant.revoke_failed", Actor: "broker", Env: wi.Env,
			WorkItem: wi.ID, Summary: err.Error()})
		return
	}
	o.emit(audit.Event{Type: "grant.revoked", Actor: "broker", Env: wi.Env, WorkItem: wi.ID,
		Summary: fmt.Sprintf("Grant %s ended; the credential is gone", grant.ID),
		Data:    map[string]any{"grant": grant.ID, "outcome": outcome}})
}

// runRunbookJob exchanges the grant, runs the script on a runner and returns the outputs
// that the definition declares. A run that reports other outputs fails.
func (o *Orchestrator) runRunbookJob(ctx context.Context, job *Job, log io.Writer,
	grant broker.Grant, definition runbook.Definition, spec jobspec.Job) (map[string]any, error) {
	cred, err := o.Broker.Exchange(ctx, grant, broker.Presentation{})
	if err != nil {
		return nil, fmt.Errorf("the broker refused the grant: %w", err)
	}
	fmt.Fprintf(log, "grant %s accepted: %s\n$ %s.sh\n", grant.ID, cred.Mechanism,
		definition.Name)
	writeSorted(log, "param", spec.Params)
	writeSorted(log, "target", map[string]string{"application": spec.Namespace,
		"data": spec.DataNamespace})
	spec.KubeConfig, spec.AWS, spec.Log = cred.KubeConfig, cred.AWS, log
	result, err := o.run(ctx, job, spec)
	if err != nil {
		return nil, err
	}
	outputs, err := definition.CheckOutputs(result.Outputs)
	if err != nil {
		return nil, err
	}
	writeSorted(log, "output", outputs)
	return outputs, nil
}

// writeSorted writes one line "label name=value" for each value, in the order of the names.
func writeSorted[V any](log io.Writer, label string, values map[string]V) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(log, "%s %s=%v\n", label, name, values[name])
	}
}

// execRunbook runs the job of a runbook under its own grant. It records the inputs, the
// outputs and the result of the run in the audit log.
func (o *Orchestrator) execRunbook(ctx context.Context, wi WorkItem,
	definition runbook.Definition, spec jobspec.Job, approver string) error {
	grant, err := o.runbookGrant(wi, definition.Name, approver)
	if err != nil {
		return err
	}
	job, logFile, err := o.startJob(wi, "", "runbook", grant.ID)
	if err != nil {
		return err
	}
	outputs, runErr := o.runRunbookJob(ctx, &job, logFile, grant, definition, spec)
	o.endJob(job, logFile, runErr)
	o.endRunbookGrant(ctx, wi, grant, runErr)
	eventType, summary := "runbook.succeeded", fmt.Sprintf("Runbook %s finished", definition.Name)
	data := map[string]any{"job": job.ID, "grant": grant.ID, "runbook": definition.Name,
		"params": spec.Params, "commit": wi.Commit}
	if runErr != nil {
		eventType, summary = "runbook.failed", fmt.Sprintf("Runbook %s failed", definition.Name)
		data["error"] = runErr.Error()
	} else {
		data["outputs"] = outputs
	}
	o.emit(audit.Event{Type: eventType, Actor: "runner", Env: wi.Env, WorkItem: wi.ID,
		Summary: summary, Data: data})
	return runErr
}

// maxCommandOutput is the most text that one diagnostic command adds to the transcript. The
// newest lines are at the end of logs and events, so the end is what is kept.
const maxCommandOutput = 2800

func lastPart(text string) string {
	if len(text) <= maxCommandOutput {
		return text
	}
	cut := text[len(text)-maxCommandOutput:]
	if _, after, found := strings.Cut(cut, "\n"); found {
		cut = after
	}
	return "(earlier output left out)\n" + cut
}

// Targets returns where the components of the application run on an environment: the
// output of the layer of the platform, as the last apply of that layer wrote it.
func (o *Orchestrator) Targets(env string) (pack.Targets, error) {
	outputs, err := store.LayerOutputs(o.DB, env, pack.TargetsLayer)
	if err != nil {
		return pack.Targets{}, err
	}
	targets, err := pack.ParseTargets(outputs)
	if err != nil {
		return pack.Targets{}, fmt.Errorf("environment %s: %w", env, err)
	}
	return targets, nil
}

// debugReads returns the reads of a debug session. The first two read the state of the
// pods and of the deployments; the core always asks for them. Then comes one read for
// each read of the pack.
func debugReads(app pack.Pack) []jobspec.Read {
	reads := []jobspec.Read{{Read: jobspec.ReadPods}, {Read: jobspec.ReadWorkloads}}
	for _, read := range app.Diagnostics {
		switch read.Read {
		case pack.ReadLogs:
			component, _ := app.Component(read.Component)
			reads = append(reads, jobspec.Read{Read: jobspec.ReadLogs,
				Workload: component.Workload, Lines: read.Lines})
		case pack.ReadEvents:
			reads = append(reads, jobspec.Read{Read: jobspec.ReadEvents})
		}
	}
	return reads
}

// writeTranscript writes the record of a debug session: for each read the command that
// the runner made from it, and the scrubbed end of what the command printed. The core
// makes the command line itself, with the code of the runner, so the record does not
// depend on text from the runner.
func writeTranscript(out io.Writer, namespace string, reads []jobspec.Read,
	results []jobspec.CommandOutput) error {
	if len(results) != len(reads) {
		return fmt.Errorf("the runner answered %d of %d diagnostic reads", len(results),
			len(reads))
	}
	for i, read := range reads {
		args, err := read.KubectlArgs(namespace)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "$ kubectl %s\n", strings.Join(args, " "))
		fmt.Fprintln(out, lastPart(scrub.Text(strings.TrimSpace(results[i].Output))))
		if failure := results[i].Failure; failure != "" {
			fmt.Fprintf(out, "(command failed: %s)\n", failure)
		}
	}
	return nil
}

// DebugSession opens a time-boxed, read-only session with the debugger identity, runs the
// diagnostic reads of the application pack and records every command and its output. The
// pack names reads, not commands: the core makes each command and adds the namespace. It
// returns the
// scrubbed transcript.
func (o *Orchestrator) DebugSession(ctx context.Context, wi WorkItem, actor, reason string) (
	string, error) {
	targets, err := o.Targets(wi.Env)
	if err != nil {
		return "", err
	}
	grant, err := o.Broker.Issue(broker.Request{WorkItem: wi.ID, Env: wi.Env, Layer: "debug",
		Kind: broker.KindDebug, Tier: "T1", Approvers: []string{actor},
		Duration: 15 * time.Minute})
	if err != nil {
		return "", err
	}
	o.emit(audit.Event{Type: "grant.debug_issued", Actor: "broker", Env: wi.Env,
		WorkItem: wi.ID,
		Summary: fmt.Sprintf("Debug grant %s for %s: read-only, namespace %s, until %s",
			grant.ID, actor, targets.Namespace, grant.Expires),
		Data: map[string]any{"grant": grant.ID, "identity": grant.Identity, "kind": grant.Kind,
			"reason": reason, "expires": grant.Expires, "not_before": grant.NotBefore,
			"tier": "T1", "approvers": grant.Approvers}})
	job, logFile, err := o.startJob(wi, "", "debug", grant.ID)
	if err != nil {
		return "", err
	}
	var transcript strings.Builder
	reads := debugReads(o.App)
	runErr := func() error {
		cred, err := o.Broker.Exchange(ctx, grant, broker.Presentation{})
		if err != nil {
			return fmt.Errorf("the broker refused the debug grant: %w", err)
		}
		result, err := o.run(ctx, &job, jobspec.Job{Spec: jobspec.Spec{Kind: jobspec.KindDiagnose,
			Env: wi.Env, KubeConfig: cred.KubeConfig, AWS: cred.AWS, Namespace: targets.Namespace,
			Reads: reads}})
		if err != nil {
			return err
		}
		return writeTranscript(io.MultiWriter(logFile, &transcript), targets.Namespace, reads,
			result.Commands)
	}()
	o.endJob(job, logFile, runErr)
	if err := o.Broker.Revoke(ctx, grant, "used"); err != nil {
		o.emit(audit.Event{Type: "grant.revoke_failed", Actor: "broker", Env: wi.Env,
			WorkItem: wi.ID, Summary: err.Error()})
	} else {
		o.emit(audit.Event{Type: "grant.revoked", Actor: "broker", Env: wi.Env, WorkItem: wi.ID,
			Summary: fmt.Sprintf("Debug grant %s ended; the session is recorded", grant.ID),
			Data:    map[string]any{"grant": grant.ID, "outcome": "used", "job": job.ID}})
	}
	return transcript.String(), runErr
}
