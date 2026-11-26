package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/runbook"
	"github.com/statik/site-reliability-clanker/internal/store"
)

func (s *Server) getMe(r *http.Request) (any, error) { return s.currentUser(r) }

func (s *Server) postSeen(r *http.Request) (any, error) {
	user, err := s.currentUser(r)
	if err != nil {
		return nil, err
	}
	_, err = s.Orch.DB.Exec(`UPDATE users SET last_seen = $1 WHERE name = $2`,
		store.FormatTime(s.Clock.Now()), user.Name)
	return map[string]bool{"ok": true}, err
}

func (s *Server) getUsers(*http.Request) (any, error) {
	rows, err := s.Orch.DB.Query(`SELECT name, display, role, email, last_seen, self_declared
		FROM users ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := []orchestrator.User{}
	for rows.Next() {
		var u orchestrator.User
		if err := rows.Scan(&u.Name, &u.Display, &u.Role, &u.Email, &u.LastSeen,
			&u.SelfDeclared); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// itemsAsOf returns the work items as they were at asOf. It replays the state changes from
// the audit log, which is the ordered record of every transition.
func (s *Server) itemsAsOf(asOf string) ([]orchestrator.WorkItem, error) {
	items, err := s.Orch.List()
	if err != nil || asOf == "" {
		return items, err
	}
	events, err := s.Audit.Query(audit.Filter{To: asOf,
		Types: []string{"workitem.state_changed"}, Limit: 100000})
	if err != nil {
		return nil, err
	}
	stateAt := map[string]string{}
	for _, e := range events {
		stateAt[e.WorkItem], _ = e.Data["to"].(string)
	}
	past := []orchestrator.WorkItem{}
	for _, wi := range items {
		if wi.CreatedAt > asOf {
			continue
		}
		wi.State = orchestrator.StateDetected
		if state, ok := stateAt[wi.ID]; ok {
			wi.State = state
		}
		past = append(past, wi)
	}
	return past, nil
}

func (s *Server) getWorkItems(r *http.Request) (any, error) {
	return s.itemsAsOf(r.URL.Query().Get("as_of"))
}

func (s *Server) getWorkItem(r *http.Request) (any, error) {
	id := r.PathValue("id")
	wi, err := s.Orch.Get(id)
	if err != nil {
		return nil, notFound(err)
	}
	plans, err := s.Orch.Plans(id, true)
	if err != nil {
		return nil, err
	}
	approvals, err := s.Orch.Approvals(id)
	if err != nil {
		return nil, err
	}
	jobs, err := s.Orch.Jobs(id)
	if err != nil {
		return nil, err
	}
	events, err := s.Audit.Query(audit.Filter{WorkItem: id})
	if err != nil {
		return nil, err
	}
	valid, required, err := s.approvalProgress(wi)
	if err != nil {
		return nil, err
	}
	detail := map[string]any{"work_item": wi, "plans": plans, "approvals": approvals,
		"valid_approvers": valid,
		"jobs":            jobs, "events": events,
		"approvers_required": required,
		"deferred_layers":    deferredLayers(wi, plans)}
	if wi.PRID != 0 {
		pr, err := s.Forge.Pull(wi.PRID)
		if err != nil {
			return nil, err
		}
		diff, err := s.Forge.Diff(pr.Repo, pr.BaseCommit, pr.HeadCommit)
		if err != nil {
			return nil, err
		}
		detail["pull"], detail["diff"] = pr, diff
	}
	grants, err := s.Broker.List(false)
	if err != nil {
		return nil, err
	}
	detail["grants"] = filterGrants(grants, id)
	return detail, nil
}

func deferredLayers(wi orchestrator.WorkItem, plans []orchestrator.Plan) []string {
	planned := map[string]bool{}
	for _, p := range plans {
		planned[p.Layer] = true
	}
	deferred := []string{}
	for _, layer := range wi.Layers {
		if !planned[layer] {
			deferred = append(deferred, layer)
		}
	}
	return deferred
}

func filterGrants(grants []broker.Grant, id string) []broker.Grant {
	out := []broker.Grant{}
	for _, g := range grants {
		if g.WorkItem == id {
			out = append(out, g)
		}
	}
	return out
}

func (s *Server) postDecision(r *http.Request) (any, error) {
	user, err := s.currentUser(r)
	if err != nil {
		return nil, err
	}
	var body struct {
		Decision     string `json:"decision"`
		Comment      string `json:"comment"`
		ScheduledFor string `json:"scheduled_for"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, err
	}
	wi, err := s.Orch.Decide(context.WithoutCancel(r.Context()), r.PathValue("id"),
		orchestrator.Decision{User: user.Name, Decision: body.Decision, Comment: body.Comment,
			ScheduledFor: body.ScheduledFor})
	if err != nil {
		return nil, err
	}
	return wi, nil
}

func (s *Server) getPlanJSON(r *http.Request) (any, error) {
	plans, err := s.Orch.Plans(r.PathValue("id"), false)
	if err != nil {
		return nil, err
	}
	for _, p := range plans {
		if p.Layer != r.PathValue("layer") {
			continue
		}
		raw, err := s.Orch.PlanJSON(p.ID)
		if err != nil {
			return nil, err
		}
		var plan map[string]any
		if err := json.Unmarshal(raw, &plan); err != nil {
			return nil, fmt.Errorf("decode plan JSON of %s %s: %w", p.WorkItem, p.Layer, err)
		}
		// State values can hold secrets. The page needs only the changes.
		delete(plan, "prior_state")
		delete(plan, "planned_values")
		delete(plan, "configuration")
		return map[string]any{"plan_sha256": p.SHA256, "plan": plan}, nil
	}
	return nil, notFound(fmt.Errorf("work item %s has no plan for layer %s", r.PathValue("id"),
		r.PathValue("layer")))
}

func (s *Server) getJobLog(r *http.Request) (any, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, badRequest("job ID %q is not a number", r.PathValue("id"))
	}
	job, err := s.Orch.Job(id)
	if err != nil {
		return nil, notFound(err)
	}
	log, err := s.Orch.JobLog(id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"job": job, "log": s.hidePaths(log)}, nil
}

// hidePaths replaces the data directory and the home directory in a log with placeholders,
// so a job log does not show where the control plane runs on its host.
func (s *Server) hidePaths(text string) string {
	// A core on a cloud has no data directory, and an empty text to replace would match
	// between every two characters.
	if s.Orch.DataDir != "" {
		text = strings.ReplaceAll(text, s.Orch.DataDir, "<data>")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		text = strings.ReplaceAll(text, home, "~")
	}
	return text
}

func (s *Server) getEvents(r *http.Request) (any, error) {
	q := r.URL.Query()
	filter := audit.Filter{From: q.Get("from"), To: q.Get("to"), Env: q.Get("env"),
		WorkItem: q.Get("work_item"), Search: q.Get("q"), TypePrefix: q.Get("type_prefix"),
		Limit: intParam(r, "limit", 500), Descending: q.Get("order") != "asc"}
	if types := q.Get("types"); types != "" {
		filter.Types = strings.Split(types, ",")
	}
	filter.AfterSeq = int64(intParam(r, "after", 0))
	return s.Audit.Query(filter)
}

func (s *Server) getAuditVerify(*http.Request) (any, error) { return s.Audit.Verify() }

// getEvidence returns the evidence pack: the events of a period, the chain verification, and
// the approvals and grants of each change, so that every change reconciles with who approved
// it and which credential ran it.
func (s *Server) getEvidence(r *http.Request) (any, error) {
	q := r.URL.Query()
	events, err := s.Audit.Query(audit.Filter{From: q.Get("from"), To: q.Get("to"),
		Limit: 100000})
	if err != nil {
		return nil, err
	}
	verification, err := s.Audit.Verify()
	if err != nil {
		return nil, err
	}
	items, err := s.Orch.List()
	if err != nil {
		return nil, err
	}
	grants, err := s.Broker.List(false)
	if err != nil {
		return nil, err
	}
	changes := []map[string]any{}
	for _, wi := range items {
		if wi.Tier == "" || wi.Tier == "T0" {
			continue
		}
		approvals, err := s.Orch.Approvals(wi.ID)
		if err != nil {
			return nil, err
		}
		changes = append(changes, map[string]any{"work_item": wi, "approvals": approvals,
			"grants": filterGrants(grants, wi.ID)})
	}
	return map[string]any{"generated_at": store.FormatTime(s.Clock.Now()),
		"period": map[string]string{"from": q.Get("from"), "to": q.Get("to")},
		"chain":  verification, "changes": changes, "events": events,
		"note": "The local target has no cloud audit trail. On a cloud target this pack is " +
			"cross-checked against CloudTrail, Cloud Audit Logs or the Activity Log."}, nil
}

func (s *Server) getHealth(r *http.Request) (any, error) {
	q := r.URL.Query()
	return s.Monitor.History(q.Get("env"), q.Get("from"), q.Get("to"),
		intParam(r, "limit", 2000))
}

func (s *Server) getFindings(*http.Request) (any, error) { return s.Detector.Findings() }

func (s *Server) getNotifications(*http.Request) (any, error) {
	deliveries, err := s.Notifier.Deliveries(200)
	if err != nil {
		return nil, err
	}
	routes, err := s.Forge.ReadFile(orchestrator.RunbooksRepo, "main",
		"notifications/routes.json")
	if err != nil {
		return nil, err
	}
	var rules any
	if err := json.Unmarshal([]byte(routes), &rules); err != nil {
		return nil, fmt.Errorf("decode notification routes: %w", err)
	}
	return map[string]any{"channels": s.Notifier.Channels(), "rules": rules,
		"deliveries":   deliveries,
		"rules_source": "runbooks:notifications/routes.json"}, nil
}

func (s *Server) postDigest(r *http.Request) (any, error) {
	if err := s.roleWhenSignedIn(r, "admin"); err != nil {
		return nil, err
	}
	return map[string]int{"events": s.Notifier.FlushDigest(r.Context())}, nil
}

func (s *Server) getRepos(*http.Request) (any, error) {
	names, err := s.Forge.Repos()
	if err != nil {
		return nil, err
	}
	repos := []map[string]any{}
	for _, name := range names {
		head, err := s.Forge.Head(name, "main")
		if err != nil {
			return nil, err
		}
		branches, err := s.Forge.Branches(name)
		if err != nil {
			return nil, err
		}
		repos = append(repos, map[string]any{"name": name, "head": head, "branches": branches,
			"clone_url": "/git/" + name + ".git"})
	}
	return repos, nil
}

func refParam(r *http.Request) string {
	if ref := r.URL.Query().Get("ref"); ref != "" {
		return ref
	}
	return "main"
}

func (s *Server) getTree(r *http.Request) (any, error) {
	entries, err := s.Forge.Tree(r.PathValue("repo"), refParam(r), r.URL.Query().Get("path"))
	if err != nil {
		return nil, notFound(err)
	}
	return entries, nil
}

func (s *Server) getBlob(r *http.Request) (any, error) {
	path := r.URL.Query().Get("path")
	content, err := s.Forge.ReadFile(r.PathValue("repo"), refParam(r), path)
	if err != nil {
		return nil, notFound(err)
	}
	return map[string]string{"path": path, "ref": refParam(r), "content": content}, nil
}

func (s *Server) getLog(r *http.Request) (any, error) {
	return s.Forge.Log(r.PathValue("repo"), refParam(r), intParam(r, "limit", 50))
}

func (s *Server) getPulls(r *http.Request) (any, error) {
	return s.Forge.Pulls(r.URL.Query().Get("repo"))
}

func (s *Server) getPull(r *http.Request) (any, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, badRequest("pull request ID %q is not a number", r.PathValue("id"))
	}
	pr, err := s.Forge.Pull(id)
	if err != nil {
		return nil, notFound(err)
	}
	diff, err := s.Forge.Diff(pr.Repo, pr.BaseCommit, pr.HeadCommit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"pull": pr, "diff": diff}, nil
}

// postPull opens a pull request for a branch that a human pushed, and puts it through the
// same pipeline as a change from the agent.
func (s *Server) postPull(r *http.Request) (any, error) {
	if err := s.roleWhenSignedIn(r, "approver", "admin"); err != nil {
		return nil, err
	}
	user, err := s.currentUser(r)
	if err != nil {
		return nil, err
	}
	var body struct {
		Branch string `json:"branch"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Env    string `json:"env"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, err
	}
	pr, err := s.Forge.OpenPull(forge.PullRequest{Repo: orchestrator.PlatformRepo,
		Branch: body.Branch, Title: body.Title, Body: body.Body, Author: user.Name},
		store.FormatTime(s.Clock.Now()))
	if err != nil {
		return nil, badRequest("%v", err)
	}
	wi, err := s.Orch.Create(orchestrator.NewWorkItem{Title: body.Title, Kind: "change",
		Env: body.Env, Trigger: "Pull request by " + user.Display, Actor: user.Name})
	if err != nil {
		return nil, badRequest("%v", err)
	}
	if _, err := s.Orch.AttachPull(wi.ID, pr.ID, user.Name); err != nil {
		return nil, badRequest("%v", err)
	}
	go s.background("plan "+wi.ID, func(ctx context.Context) error {
		_, err := s.Orch.PlanWorkItem(ctx, wi.ID)
		return err
	})
	return map[string]any{"work_item": wi.ID, "pull": pr}, nil
}

// background runs a long task that outlives the request and records a failure.
func (s *Server) background(name string, task func(ctx context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	if err := task(ctx); err != nil {
		if _, auditErr := s.Audit.Append(audit.Event{Type: "task.failed", Actor: "orchestrator",
			Summary: fmt.Sprintf("Background task %q failed: %v", name, err)}); auditErr != nil {
			panic(fmt.Sprintf("audit log is not writable, stopping: %v", auditErr))
		}
	}
}

func (s *Server) getChat(r *http.Request) (any, error) {
	return s.Agent.History(r.URL.Query().Get("work_item"))
}

func (s *Server) postChat(r *http.Request) (any, error) {
	user, err := s.currentUser(r)
	if err != nil {
		return nil, err
	}
	var body struct {
		WorkItem string `json:"work_item"`
		Question string `json:"question"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, err
	}
	// The answer is stored even when the client goes away, so no question stays unanswered.
	return s.Agent.Ask(context.WithoutCancel(r.Context()), user.Name, body.WorkItem,
		body.Question)
}

func (s *Server) getAgentCalls(r *http.Request) (any, error) {
	return s.Agent.Calls(r.URL.Query().Get("work_item"), intParam(r, "limit", 100))
}

func (s *Server) getSim(*http.Request) (any, error) { return s.Sim.State() }

func (s *Server) postSimAdvance(r *http.Request) (any, error) {
	if err := s.roleWhenSignedIn(r, "admin"); err != nil {
		return nil, err
	}
	var body struct {
		Hours int `json:"hours"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, err
	}
	if body.Hours < 1 || body.Hours > 24*60 {
		return nil, badRequest("hours must be from 1 to 1440, got %d", body.Hours)
	}
	if err := s.Sim.Advance(r.Context(), time.Duration(body.Hours)*time.Hour); err != nil {
		return nil, err
	}
	return s.Sim.State()
}

func (s *Server) postSimScene(r *http.Request) (any, error) {
	if err := s.roleWhenSignedIn(r, "admin"); err != nil {
		return nil, err
	}
	name := r.PathValue("name")
	go s.background("scene "+name, func(ctx context.Context) error {
		return s.Sim.RunScene(ctx, name)
	})
	return map[string]string{"started": name}, nil
}

func (s *Server) requireRole(r *http.Request, roles ...string) (orchestrator.User, error) {
	user, err := s.currentUser(r)
	if err != nil {
		return orchestrator.User{}, err
	}
	if !slices.Contains(roles, user.Role) {
		return orchestrator.User{}, forbidden(fmt.Errorf("%s has role %s; this action needs "+
			"one of: %s", user.Name, user.Role, strings.Join(roles, ", ")))
	}
	return user, nil
}

// roleWhenSignedIn guards an action that the demo leaves open to every user of its user
// switcher. With a sign-in, the action needs one of the roles.
func (s *Server) roleWhenSignedIn(r *http.Request, roles ...string) error {
	if s.Identify == nil {
		return nil
	}
	_, err := s.requireRole(r, roles...)
	return err
}

// getRunbooks lists the runbooks with their definitions and who signed the standing
// approval of each one.
func (s *Server) getRunbooks(*http.Request) (any, error) {
	definitions, err := s.Orch.Runbooks()
	if err != nil {
		return nil, err
	}
	type row struct {
		runbook.Definition
		StandingApprover string `json:"standing_approver"`
		StandingSignedAt string `json:"standing_signed_at"`
	}
	rows := make([]row, 0, len(definitions))
	for _, definition := range definitions {
		standing, err := s.Orch.StandingApproval(definition.Name)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row{Definition: definition, StandingApprover: standing.Signer,
			StandingSignedAt: standing.SignedAt})
	}
	return rows, nil
}

// postStandingApproval signs the standing approval of a runbook as the person behind the
// request, and answers with the approval.
func (s *Server) postStandingApproval(r *http.Request) (any, error) {
	user, err := s.requireRole(r, "approver", "admin")
	if err != nil {
		return nil, err
	}
	name := r.PathValue("name")
	if err := s.Orch.SignStandingApproval(name, user.Name); err != nil {
		return nil, err
	}
	return s.Orch.StandingApproval(name)
}

// deleteStandingApproval revokes the standing approval of a runbook, and answers with the
// approval, which has no signer now.
func (s *Server) deleteStandingApproval(r *http.Request) (any, error) {
	user, err := s.requireRole(r, "approver", "admin")
	if err != nil {
		return nil, err
	}
	name := r.PathValue("name")
	if err := s.Orch.RevokeStandingApproval(name, user.Name); err != nil {
		return nil, err
	}
	return s.Orch.StandingApproval(name)
}

// postRunbook starts a runbook. It answers with the reason when the request does not fit
// the definition of the runbook, or when nobody signed its standing approval.
func (s *Server) postRunbook(r *http.Request) (any, error) {
	user, err := s.requireRole(r, "approver", "admin")
	if err != nil {
		return nil, err
	}
	var body struct {
		Env    string            `json:"env"`
		Params map[string]string `json:"params"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, err
	}
	name := r.PathValue("name")
	if _, err := s.Orch.CheckRunbook(name, body.Params); err != nil {
		return nil, err
	}
	approver, err := s.Orch.StandingApprover(name)
	if err != nil {
		return nil, err
	}
	if approver == "" {
		return nil, fmt.Errorf("%w: runbook %s has no standing approval: an approver must "+
			"sign one first", orchestrator.ErrConflict, name)
	}
	go s.background("runbook "+name, func(ctx context.Context) error {
		_, err := s.Orch.RunRunbook(ctx, orchestrator.RunbookRequest{Env: body.Env, Name: name,
			Params: body.Params, Actor: user.Name, Trigger: "Started by " + user.Display})
		return err
	})
	return map[string]string{"started": name}, nil
}

// postDebugSession opens a debug session and answers with its work item at once. The
// session runs in the background and not on the context of the request, so a client that
// goes away, or a load balancer that closes an idle connection, does not stop it. The work
// item ends Done or Failed, and the transcript is the log of its debug job.
func (s *Server) postDebugSession(r *http.Request) (any, error) {
	user, err := s.requireRole(r, "approver", "admin")
	if err != nil {
		return nil, err
	}
	var body struct {
		Env    string `json:"env"`
		Reason string `json:"reason"`
	}
	if err := decodeBody(r, &body); err != nil {
		return nil, err
	}
	wi, err := s.Orch.Create(orchestrator.NewWorkItem{Title: "Debug session: " + body.Reason,
		Kind: "debug-session", Env: body.Env, Trigger: "Requested by " + user.Display,
		Actor: user.Name})
	if err != nil {
		return nil, badRequest("%v", err)
	}
	go s.background("debug session "+wi.ID, func(ctx context.Context) error {
		_, err := s.Orch.DebugSession(ctx, wi, user.Name, body.Reason)
		state, reason := orchestrator.StateDone, "session ended"
		if err != nil {
			state, reason = orchestrator.StateFailed, err.Error()
		}
		if _, stateErr := s.Orch.Transition(wi.ID, state, user.Name, reason); stateErr != nil {
			return errors.Join(err, stateErr)
		}
		return err
	})
	return wi, nil
}

// approvalProgress returns the approvers whose approval covers the current plans of the
// work item, and the number of approvers that its tier needs.
func (s *Server) approvalProgress(wi orchestrator.WorkItem) ([]string, int, error) {
	valid, err := s.Orch.ValidApprovers(wi)
	if err != nil {
		return nil, 0, err
	}
	required, err := s.Orch.ApproversRequired(wi.Tier)
	return valid, required, err
}

func (s *Server) startEnvironmentWork(r *http.Request, kind, title, trigger string) (any, error) {
	user, err := s.requireRole(r, "admin")
	if err != nil {
		return nil, err
	}
	env := r.PathValue("env")
	wi, err := s.Orch.Create(orchestrator.NewWorkItem{Title: title, Kind: kind, Env: env,
		Layers: s.Orch.Envs[env].BlankSlateLayers(), Trigger: trigger, Actor: user.Name})
	if err != nil {
		return nil, badRequest("%v", err)
	}
	go s.background("plan "+wi.ID, func(ctx context.Context) error {
		_, err := s.Orch.PlanWorkItem(ctx, wi.ID)
		return err
	})
	return wi, nil
}

func (s *Server) postBlankSlate(r *http.Request) (any, error) {
	return s.startEnvironmentWork(r, orchestrator.KindBlankSlate,
		"Blank-slate deployment of "+r.PathValue("env"), "First run: no layer exists")
}

func (s *Server) postTeardown(*http.Request) (any, error) {
	return nil, statusError{http.StatusNotImplemented, fmt.Errorf("teardown through the " +
		"pipeline is not implemented; run `clanker destroy --env <name>` from the CLI")}
}
