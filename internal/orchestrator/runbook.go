package orchestrator

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
	"github.com/statik/site-reliability-clanker/internal/scrub"
	"github.com/statik/site-reliability-clanker/internal/store"
)

var runbookName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,40}$`)

func standingKey(runbook string) string { return "standing_approval." + runbook }

// SignStandingApproval records that a human approves every future run of a tier T1 runbook.
func (o *Orchestrator) SignStandingApproval(runbook, user string) error {
	u, err := o.GetUser(user)
	if err != nil {
		return err
	}
	if u.Role != "approver" && u.Role != "admin" {
		return fmt.Errorf("%s has role %s and cannot sign a standing approval", u.Name, u.Role)
	}
	if err := store.SetKV(o.DB, standingKey(runbook), u.Name); err != nil {
		return err
	}
	o.emit(audit.Event{Type: "standing_approval.signed", Actor: u.Name,
		Summary: fmt.Sprintf("%s signed a standing approval for runbook %s", u.Display, runbook),
		Data:    map[string]any{"runbook": runbook, "tier": "T1"}})
	return nil
}

// StandingApprover returns who signed the standing approval for a runbook, or "".
func (o *Orchestrator) StandingApprover(runbook string) (string, error) {
	return store.GetKV(o.DB, standingKey(runbook))
}

// runbookJob returns a job for a script of the runbooks repository at the head of main,
// and that commit.
func (o *Orchestrator) runbookJob(env, name string, params map[string]string) (jobspec.Job, string, error) {
	head, err := o.Forge.Head(RunbooksRepo, "main")
	if err != nil {
		return jobspec.Job{}, "", fmt.Errorf("find runbooks: %w", err)
	}
	archive, err := o.Forge.Archive(RunbooksRepo, head)
	if err != nil {
		return jobspec.Job{}, "", err
	}
	return jobspec.Job{Spec: jobspec.Spec{Kind: jobspec.KindRunbook, Env: env,
		CheckoutID: "runbooks/" + head, Runbook: name, Params: params}, Checkout: archive}, head, nil
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
// T1: routine and reversible. Each use is logged and has its own short-lived grant.
func (o *Orchestrator) RunRunbook(ctx context.Context, req RunbookRequest) (WorkItem, error) {
	if !runbookName.MatchString(req.Name) {
		return WorkItem{}, fmt.Errorf("runbook name %q is not valid", req.Name)
	}
	approver, err := o.StandingApprover(req.Name)
	if err != nil {
		return WorkItem{}, err
	}
	if approver == "" {
		return WorkItem{}, fmt.Errorf("runbook %s has no standing approval: an approver must "+
			"sign one first", req.Name)
	}
	scripts, commit, err := o.runbookJob(req.Env, req.Name, req.Params)
	if err != nil {
		return WorkItem{}, err
	}
	if _, err := o.Forge.ReadFile(RunbooksRepo, commit, "jobs/"+req.Name+".sh"); err != nil {
		return WorkItem{}, fmt.Errorf("runbook %s is not in the runbooks repository at %s",
			req.Name, commit[:8])
	}
	title := req.Title
	if title == "" {
		title = "Runbook: " + req.Name
	}
	wi, err := o.Create(NewWorkItem{Title: title, Kind: "runbook:" + req.Name, Env: req.Env,
		Trigger: req.Trigger, Actor: req.Actor})
	if err != nil {
		return WorkItem{}, err
	}
	unlock := o.lockEnv(req.Env)
	defer unlock()
	wi.Tier, wi.TierRule, wi.Commit = "T1", "standing-approval", commit
	wi.Summary = fmt.Sprintf("Runs the %s runbook under the standing approval that %s signed.",
		req.Name, approver)
	if err := o.setState(&wi, StateApproved, "orchestrator",
		"covered by the standing approval of "+approver); err != nil {
		return WorkItem{}, err
	}
	if err := o.setState(&wi, StateApplying, "orchestrator", "runbook started"); err != nil {
		return WorkItem{}, err
	}
	runErr := o.execRunbook(ctx, wi, req, scripts, "standing:"+approver)
	if runErr != nil {
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

func (o *Orchestrator) execRunbook(ctx context.Context, wi WorkItem, req RunbookRequest,
	scripts jobspec.Job, approver string) error {
	grant, err := o.Broker.Issue(broker.Request{WorkItem: wi.ID, Env: wi.Env, Layer: "runbook",
		Kind: broker.KindRunbook, Commit: wi.Commit, Tier: "T1", Approvers: []string{approver},
		Duration: 15 * time.Minute})
	if err != nil {
		return err
	}
	o.emit(audit.Event{Type: "grant.issued", Actor: "broker", Env: wi.Env, WorkItem: wi.ID,
		Summary: fmt.Sprintf("Grant %s: runbook %s until %s", grant.ID, req.Name, grant.Expires),
		Data: map[string]any{"grant": grant.ID, "identity": grant.Identity, "kind": grant.Kind,
			"tier": "T1", "approvers": grant.Approvers, "expires": grant.Expires,
			"not_before": grant.NotBefore}})
	job, logFile, err := o.startJob(wi, "", "runbook", grant.ID)
	if err != nil {
		return err
	}
	runErr := func() error {
		cred, err := o.Broker.Exchange(ctx, grant, broker.Presentation{})
		if err != nil {
			return fmt.Errorf("the broker refused the grant: %w", err)
		}
		fmt.Fprintf(logFile, "grant %s accepted: %s\n$ %s.sh\n", grant.ID, cred.Mechanism,
			req.Name)
		keys := make([]string, 0, len(req.Params))
		for k := range req.Params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(logFile, "param %s=%s\n", k, req.Params[k])
		}
		scripts.KubeConfig, scripts.Log = cred.KubeConfig, logFile
		_, err = o.run(ctx, &job, scripts)
		return err
	}()
	o.endJob(job, logFile, runErr)
	outcome := "used"
	if runErr != nil {
		outcome = "revoked"
	}
	if err := o.Broker.Revoke(ctx, grant, outcome); err != nil {
		o.emit(audit.Event{Type: "grant.revoke_failed", Actor: "broker", Env: wi.Env,
			WorkItem: wi.ID, Summary: err.Error()})
	} else {
		o.emit(audit.Event{Type: "grant.revoked", Actor: "broker", Env: wi.Env, WorkItem: wi.ID,
			Summary: fmt.Sprintf("Grant %s ended; the credential is gone", grant.ID),
			Data:    map[string]any{"grant": grant.ID, "outcome": outcome}})
	}
	eventType, summary := "runbook.succeeded", fmt.Sprintf("Runbook %s finished", req.Name)
	data := map[string]any{"job": job.ID, "grant": grant.ID, "runbook": req.Name,
		"params": req.Params, "commit": wi.Commit}
	if runErr != nil {
		eventType, summary = "runbook.failed", fmt.Sprintf("Runbook %s failed", req.Name)
		data["error"] = runErr.Error()
	}
	o.emit(audit.Event{Type: eventType, Actor: "runner", Env: wi.Env, WorkItem: wi.ID,
		Summary: summary, Data: data})
	return runErr
}

// diagnosticCommands are the only commands of a debug session. All of them read.
var diagnosticCommands = [][]string{
	{"get", "pods", "-n", "paperboat", "-o", "wide"},
	{"get", "deployments", "-n", "paperboat"},
	{"logs", "-n", "paperboat", "deployment/paperboat-api", "--tail=30"},
	{"get", "events", "-n", "paperboat", "--sort-by=.lastTimestamp"},
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

// DebugSession opens a time-boxed, read-only session with the debugger identity, runs the
// fixed diagnostic commands and records every command and its output. It returns the
// scrubbed transcript.
func (o *Orchestrator) DebugSession(ctx context.Context, wi WorkItem, actor, reason string) (
	string, error) {
	grant, err := o.Broker.Issue(broker.Request{WorkItem: wi.ID, Env: wi.Env, Layer: "debug",
		Kind: broker.KindDebug, Tier: "T1", Approvers: []string{actor},
		Duration: 15 * time.Minute})
	if err != nil {
		return "", err
	}
	o.emit(audit.Event{Type: "grant.debug_issued", Actor: "broker", Env: wi.Env,
		WorkItem: wi.ID,
		Summary: fmt.Sprintf("Debug grant %s for %s: read-only, namespace paperboat, until %s",
			grant.ID, actor, grant.Expires),
		Data: map[string]any{"grant": grant.ID, "identity": grant.Identity, "kind": grant.Kind,
			"reason": reason, "expires": grant.Expires, "not_before": grant.NotBefore,
			"tier": "T1", "approvers": grant.Approvers}})
	job, logFile, err := o.startJob(wi, "", "debug", grant.ID)
	if err != nil {
		return "", err
	}
	var transcript strings.Builder
	runErr := func() error {
		cred, err := o.Broker.Exchange(ctx, grant, broker.Presentation{})
		if err != nil {
			return fmt.Errorf("the broker refused the debug grant: %w", err)
		}
		result, err := o.run(ctx, &job, jobspec.Job{Spec: jobspec.Spec{Kind: jobspec.KindDiagnose,
			Env: wi.Env, KubeConfig: cred.KubeConfig, Commands: diagnosticCommands}})
		if err != nil {
			return err
		}
		if len(result.Commands) != len(diagnosticCommands) {
			return fmt.Errorf("the runner answered %d of %d diagnostic commands",
				len(result.Commands), len(diagnosticCommands))
		}
		out := io.MultiWriter(logFile, &transcript)
		for i, args := range diagnosticCommands {
			fmt.Fprintf(out, "$ kubectl %s\n", strings.Join(args, " "))
			fmt.Fprintln(out, lastPart(scrub.Text(strings.TrimSpace(result.Commands[i].Output))))
			if failure := result.Commands[i].Failure; failure != "" {
				fmt.Fprintf(out, "(command failed: %s)\n", failure)
			}
		}
		return nil
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
