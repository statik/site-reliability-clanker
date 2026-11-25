package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	// MapFS is a complete file system in memory. The standard library has no other one.
	"testing/fstest"
	"time"

	"github.com/statik/site-reliability-clanker/internal/agent"
	"github.com/statik/site-reliability-clanker/internal/api"
	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/awstarget"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/detect"
	"github.com/statik/site-reliability-clanker/internal/dispatch"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/localtarget"
	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/notify"
	"github.com/statik/site-reliability-clanker/internal/ollama"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/policy"
	"github.com/statik/site-reliability-clanker/internal/runbook"
	"github.com/statik/site-reliability-clanker/internal/runner"
	"github.com/statik/site-reliability-clanker/internal/scrub"
	"github.com/statik/site-reliability-clanker/internal/sim"
	"github.com/statik/site-reliability-clanker/internal/sink"
	"github.com/statik/site-reliability-clanker/internal/store"
)

const (
	mailpitImage     = "axllent/mailpit:v1.33.1"
	mailpitContainer = "clanker-mailpit"
	demoAdminToken   = "local-demo-token"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func runSink(ctx context.Context, addr string) error {
	server := &http.Server{Addr: addr, Handler: (&sink.Sink{}).Handler(),
		ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("stop webhook sink", "error", err)
		}
	}()
	slog.Info("webhook sink listens", "url", "http://"+addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("webhook sink on %s: %w", addr, err)
	}
	return nil
}

// ensureMailpit starts the local mail catcher when nothing listens on the SMTP port.
func ensureMailpit(smtpAddr string) error {
	if conn, err := net.DialTimeout("tcp", smtpAddr, time.Second); err == nil {
		conn.Close()
		return nil
	}
	if err := exec.Command("docker", "start", mailpitContainer).Run(); err == nil {
		return nil
	}
	out, err := exec.Command("docker", "run", "-d", "--name", mailpitContainer,
		"-p", "127.0.0.1:8025:8025", "-p", "127.0.0.1:1025:1025", mailpitImage).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start Mailpit for local email: %w: %s", err,
			strings.TrimSpace(string(out)))
	}
	return nil
}

var demoUsers = []orchestrator.User{
	{Name: "alice", Display: "Alice Ng", Role: "approver", Email: "alice@example.com"},
	{Name: "bob", Display: "Bob Okafor", Role: "approver", Email: "bob@example.com"},
	{Name: "carol", Display: "Carol Reyes", Role: "admin", Email: "carol@example.com"},
	{Name: "dave", Display: "Dave Lindqvist", Role: "viewer", Email: "dave@example.com"},
}

// demoApprover is the demo user whose place the person who ran the wizard takes.
const demoApprover = "alice"

// localUsers returns the users of a control plane with no sign-in: the demo users, or,
// when the wizard gave the name of a person, that person in the place of the first
// demo approver.
func localUsers(operator string) []orchestrator.User {
	if operator == "" {
		return demoUsers
	}
	name := operatorUserName(operator)
	users := []orchestrator.User{{Name: name, Display: operator, Role: "approver",
		Email: name + "@example.com", SelfDeclared: true}}
	for _, u := range demoUsers {
		if u.Name != demoApprover {
			users = append(users, u)
		}
	}
	return users
}

// operatorUserName makes the short name of a user from the name of a person: the
// ASCII letters and digits of the first word that has one, in lower case. The
// dashboard sends this name in a header, which carries only ASCII. A name that a
// demo user keeps gets a digit, so the two stay different users.
func operatorUserName(operator string) string {
	name := "operator"
	for _, word := range strings.Fields(strings.ToLower(operator)) {
		letters := strings.Map(asciiLetterOrDigit, word)
		if letters != "" {
			name = letters
			break
		}
	}
	for _, u := range demoUsers {
		if u.Name == name && u.Name != demoApprover {
			return name + "1"
		}
	}
	return name
}

func asciiLetterOrDigit(r rune) rune {
	if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
		return r
	}
	return -1
}

func seedUsers(db *sql.DB, users []orchestrator.User) error {
	for _, u := range users {
		if _, err := db.Exec(`INSERT INTO users (name, display, role, email, self_declared)
			VALUES ($1,$2,$3,$4,$5) ON CONFLICT (name) DO NOTHING`, u.Name, u.Display, u.Role,
			u.Email, u.SelfDeclared); err != nil {
			return fmt.Errorf("seed user %s: %w", u.Name, err)
		}
	}
	return nil
}

// subtrees returns the directories of source with the given paths.
func subtrees(source fs.FS, paths ...string) ([]fs.FS, error) {
	trees := []fs.FS{}
	for _, path := range paths {
		tree, err := fs.Sub(source, path)
		if err != nil {
			return nil, fmt.Errorf("open %s of the source files: %w", path, err)
		}
		trees = append(trees, tree)
	}
	return trees, nil
}

// seedForge makes the three repositories of the forge from the source files, when they do
// not exist yet. The chart of the application pack goes into the platform repository at
// the path that the stacks read it from.
func seedForge(f *forge.Forge, source fs.FS, app pack.Pack) error {
	trees, err := subtrees(source, "platform", "runbooks")
	if err != nil {
		return err
	}
	platform, runbooks := trees[0], map[string]fs.FS{".": trees[1]}
	chart, err := app.ChartFiles()
	if err != nil {
		return fmt.Errorf("open the chart of the application pack: %w", err)
	}
	// The runbooks of the application come from the pack. They can go only into the
	// directory of the jobs, so a pack cannot bring a policy or a notification route.
	jobs, err := app.RunbookFiles()
	if err != nil {
		return fmt.Errorf("open the runbooks of the application pack: %w", err)
	}
	if jobs != nil {
		runbooks[orchestrator.RunbookJobsDir] = jobs
	}
	if err := f.Seed(orchestrator.PlatformRepo, map[string]fs.FS{
		"platform": platform, app.ChartMount(): chart,
	}, "Seed platform code and the "+app.Title+" chart"); err != nil {
		return err
	}
	if err := f.Seed(orchestrator.RunbooksRepo, runbooks,
		"Seed runbooks, policies and notification routes"); err != nil {
		return err
	}
	first := app.Releases.First
	note, err := app.ReadReleaseNote(first)
	if err != nil {
		return fmt.Errorf("read the %s release note: %w", first, err)
	}
	// The repository of the application starts with the first release only. The simulator
	// publishes the later ones.
	firstRelease := fstest.MapFS{first + ".md": {Data: note, Mode: 0o644}}
	return f.Seed(app.Releases.Repository, map[string]fs.FS{
		app.Releases.Directory: firstRelease, app.Deploy.Chart: chart}, "Release "+first)
}

// adapters returns the channels of the notifier. It also builds the chat apps, which are
// the adapters of their platforms.
func adapters(c *components, o options) ([]notify.Adapter, error) {
	db := c.db
	var err error
	if c.chat, err = buildChat(c, o); err != nil {
		return nil, err
	}
	smtpAddr := envOr("CLANKER_SMTP_ADDR", "127.0.0.1:1025")
	sinkURL := "http://" + o.sinkAddr
	recipients := func(audience string) []string {
		role := map[string]string{"approvers": "approver", "admins": "admin"}[audience]
		query := `SELECT email FROM users WHERE role IN ('approver', 'admin')`
		args := []any{}
		if role != "" {
			query, args = `SELECT email FROM users WHERE role = $1`, []any{role}
		}
		rows, err := db.Query(query, args...)
		if err != nil {
			return nil
		}
		defer rows.Close()
		emails := []string{}
		for rows.Next() {
			var email string
			if err := rows.Scan(&email); err == nil {
				emails = append(emails, email)
			}
		}
		return emails
	}
	if o.demo {
		if err := ensureMailpit(smtpAddr); err != nil {
			return nil, err
		}
	}
	slack, teams := c.chat.adapters(
		notify.Slack{WebhookURL: envOr("CLANKER_SLACK_WEBHOOK", sinkURL+"/slack")},
		notify.Teams{WebhookURL: envOr("CLANKER_TEAMS_WEBHOOK", sinkURL+"/teams")})
	return []notify.Adapter{
		notify.Email{Addr: smtpAddr, From: "clanker@example.com", Recipients: recipients},
		slack, teams,
		notify.Discord{WebhookURL: envOr("CLANKER_DISCORD_WEBHOOK", sinkURL+"/discord")},
	}, nil
}

// components holds everything that `up` starts.
type components struct {
	// writer is the hold on the database. Only one control plane has it at a time.
	writer *store.Writer
	db     *sql.DB
	clock  *sim.Clock
	log    *audit.Log
	forge  *forge.Forge
	// runner reads the files of the runner directory. Only the local target uses it, for
	// the administrator kubeconfig and the image registry.
	runner     *runner.Runner
	dispatcher *dispatch.Dispatcher
	broker     *broker.Broker
	orch       *orchestrator.Orchestrator
	monitor    *monitor.Monitor
	notifier   *notify.Notifier
	agent      *agent.Agent
	detector   *detect.Detector
	sim        *sim.Simulator
	// chat is the apps for approval in Slack and Teams. Nil when none is configured.
	chat *chatApps
	// localModel starts and stops the model server on this machine. Only the local
	// target has it.
	localModel *ollama.Control
}

// target holds the parts that only one kind of target has. On a cloud manualChange is nil:
// the drift scene reports that it cannot run. On AWS the issuer is nil without the outputs
// of layer 00-bootstrap: the broker then exchanges no grant. On a cloud localModel is nil: the
// core starts no program, so it cannot start a model server.
type target struct {
	issuer       broker.Issuer
	manualChange func(context.Context, string) error
	localModel   *ollama.Control
}

// localTarget makes the data directory and the parts of the local target, which all use
// files of that directory and start programs.
func localTarget(c *components, o options) (target, error) {
	var err error
	if c.runner, err = runner.New(o.dataDir); err != nil {
		return target{}, err
	}
	adminKubeconfig := func(env string) (string, error) {
		path := filepath.Join(c.runner.StateDir(env), "kubeconfig")
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("environment %s has no cluster yet: apply layer 20-cluster "+
				"first", env)
		}
		return path, nil
	}
	// The targets are known after the first apply of the layer of the platform.
	targets := func(env string) (pack.Targets, bool) {
		outputs, err := store.LayerOutputs(c.db, env, pack.TargetsLayer)
		if err != nil {
			return pack.Targets{}, false
		}
		found, err := pack.ParseTargets(outputs)
		return found, err == nil
	}
	return target{issuer: localtarget.Credentials{AdminKubeconfig: adminKubeconfig,
		Targets: targets},
		manualChange: localtarget.ManualChange(adminKubeconfig, targets),
		localModel:   localModelControl(os.Getenv, o.dataDir)}, nil
}

func build(ctx context.Context, o options) (*components, error) {
	c := &components{}
	var on target
	if o.cloud == "local" {
		if err := os.MkdirAll(o.dataDir, 0o755); err != nil {
			return nil, fmt.Errorf("create data directory %s: %w", o.dataDir, err)
		}
		var err error
		if on, err = localTarget(c, o); err != nil {
			return nil, err
		}
		c.localModel = on.localModel
	}
	secret, err := loadSecrets(o)
	if err != nil {
		return nil, err
	}
	registerSecrets(secret, os.Getenv)
	c.writer, err = store.HoldWriter(ctx, secret.databaseURL, func() {
		slog.Warn("another control plane writes to this database; waiting until it stops")
	})
	if err != nil {
		return nil, err
	}
	if c.db, err = store.Open(secret.databaseURL); err != nil {
		return nil, err
	}
	if c.clock, err = sim.NewClock(c.db); err != nil {
		return nil, err
	}
	c.log = audit.New(c.db, c.clock)
	c.forge = forge.New(c.db)
	env := orchestrator.EnvConfig{Name: o.env, Cloud: o.cloud}
	if o.cloud == "aws" {
		if env, on.issuer, err = awsEnvironment(c, env, awstarget.NewSTS); err != nil {
			return nil, err
		}
	}
	if c.dispatcher, err = newDispatcher(ctx, o); err != nil {
		return nil, err
	}
	c.broker, err = broker.New(c.db, c.clock, secret.signingKey, on.issuer,
		func() (policy.Approvals, error) { return orchestrator.ApprovalSettings(c.forge) })
	if err != nil {
		return nil, err
	}
	c.orch = &orchestrator.Orchestrator{DB: c.db, Audit: c.log, Forge: c.forge,
		Runners: c.dispatcher, Broker: c.broker, Clock: c.clock, DataDir: o.dataDir,
		Envs: map[string]orchestrator.EnvConfig{o.env: env}, App: o.app}
	if o.demo {
		c.orch.JobVariables = map[string]string{"TF_VAR_admin_token": demoAdminToken}
		scrub.Add(demoAdminToken)
	}
	if err := addCollectorVariables(c.orch, o.cloud, o.addr, o.env); err != nil {
		return nil, err
	}
	c.monitor = monitor.New(c.db, c.clock)
	c.monitor.App = o.app
	// The demo users have no way to sign in. They exist only for the user switcher.
	if o.demo {
		if err := seedUsers(c.db, localUsers(o.operator)); err != nil {
			return nil, err
		}
	}
	if err := seedForge(c.forge, o.source, o.app); err != nil {
		return nil, err
	}
	channelAdapters, err := adapters(c, o)
	if err != nil {
		return nil, err
	}
	c.notifier = notify.New(c.db, c.log, c.clock, "http://"+o.addr, channelAdapters,
		func() (notify.Rules, error) {
			raw, err := c.forge.ReadFile(orchestrator.RunbooksRepo, "main",
				"notifications/routes.json")
			if err != nil {
				return notify.Rules{}, err
			}
			return notify.ParseRules([]byte(raw))
		})
	llm, models, err := chooseLLM(os.Getenv)
	if err != nil {
		return nil, err
	}
	c.agent = &agent.Agent{DB: c.db, Audit: c.log, Orch: c.orch, Forge: c.forge,
		Monitor: c.monitor, Clock: c.clock, LLM: llm, Models: models,
		DailyBudgetUSD: 5, DailyBudgetTokens: agent.DefaultDailyTokens, App: o.app}
	c.detector = &detect.Detector{DB: c.db, Audit: c.log, Orch: c.orch, Forge: c.forge,
		Monitor: c.monitor, Clock: c.clock, OnFinding: c.agent.HandleFinding, App: o.app}
	c.sim = &sim.Simulator{DB: c.db, Clock: c.clock, Audit: c.log, Forge: c.forge,
		Detector: c.detector, Orch: c.orch, Monitor: c.monitor, Notifier: c.notifier,
		AdminToken: demoAdminToken, ManualChange: on.manualChange,
		Env: o.env, App: o.app}
	c.orch.Hooks = orchestratorHooks(c, o)
	return c, nil
}

// orchestratorHooks connects the agent, the monitor and the image build to the pipeline.
func orchestratorHooks(c *components, o options) orchestrator.Hooks {
	return orchestrator.Hooks{Explain: c.agent.Explain, Review: c.agent.Review,
		Verify: c.monitor.Verify,
		LayerApplied: func(ctx context.Context, wi orchestrator.WorkItem, layer string) error {
			if !o.demo || o.cloud != "local" || layer != "10-network" {
				return nil
			}
			return publishImages(ctx, c, o)
		},
		Applied: func(ctx context.Context, wi orchestrator.WorkItem) {
			if _, err := c.monitor.TakeSnapshot(ctx, wi.Env, "after "+wi.ID); err != nil {
				slog.Error("take snapshot", "work_item", wi.ID, "error", err)
			}
		}}
}

// publishImages builds the releases of the application and pushes them to the registry of
// the local
// target. It stands in for the release pipeline of the application, which the prototype
// does not have. It runs when the registry exists, before the application layer needs it.
func publishImages(ctx context.Context, c *components, o options) error {
	// A pack with no build program has its images in a registry already.
	if o.app.Deploy.Build == "" {
		return nil
	}
	outputs, err := store.LayerOutputs(c.db, o.env, "10-network")
	if err != nil {
		return err
	}
	registry, _ := outputs["image_registry"].(string)
	if registry == "" {
		return fmt.Errorf("layer 10-network of %s has no image_registry output", o.env)
	}
	logPath := filepath.Join(o.dataDir, "logs", "image-build.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("create image build log: %w", err)
	}
	defer logFile.Close()
	build := filepath.Join(o.packDisk, filepath.FromSlash(o.app.Deploy.Build))
	cmd := exec.CommandContext(ctx, build, registry)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build and push the %s images to %s: %w; see %s", o.app.Title,
			registry, err, logPath)
	}
	_, err = c.log.Append(audit.Event{Type: "sim.images_published", Actor: "simulator",
		Env:     o.env,
		Summary: o.app.Title + " images for all releases are in the registry " + registry,
		Data:    map[string]any{"registry": registry}})
	return err
}

// signDemoApprovals signs, as the demo admin, a standing approval for each runbook of the
// runbooks repository that a standing approval can cover.
func signDemoApprovals(orch *orchestrator.Orchestrator) error {
	definitions, err := orch.Runbooks()
	if err != nil {
		return err
	}
	for _, definition := range definitions {
		if definition.Tier != runbook.StandingTier {
			continue
		}
		if err := orch.SignStandingApproval(definition.Name, "carol"); err != nil {
			return err
		}
	}
	return nil
}

// firstRun signs the demo standing approvals and opens the blank-slate work item when the
// control plane has no work item yet.
func firstRun(ctx context.Context, c *components, o options) error {
	items, err := c.orch.List()
	if err != nil || len(items) > 0 {
		return err
	}
	if o.demo {
		if err := signDemoApprovals(c.orch); err != nil {
			return err
		}
	}
	wi, err := c.orch.Create(orchestrator.NewWorkItem{
		Title: "Blank-slate deployment of " + o.env, Kind: orchestrator.KindBlankSlate, Env: o.env,
		Layers:  c.orch.Envs[o.env].BlankSlateLayers(),
		Trigger: "First run: no layer of this environment exists",
		Actor:   "orchestrator"})
	if err != nil {
		return err
	}
	go func() {
		if _, err := c.orch.PlanWorkItem(context.WithoutCancel(ctx), wi.ID); err != nil {
			slog.Error("plan the blank-slate work item", "error", err)
		}
	}()
	return nil
}

func schedulerLoop(ctx context.Context, c *components) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.orch.RunDue(); err != nil {
				slog.Error("start scheduled work", "error", err)
			}
			expired, err := c.broker.ExpireOverdue()
			if err != nil {
				slog.Error("expire grants", "error", err)
			}
			for _, g := range expired {
				if _, err := c.log.Append(audit.Event{Type: "grant.expired", Actor: "broker",
					Env: g.Env, Layer: g.Layer, WorkItem: g.WorkItem,
					Summary: fmt.Sprintf("Grant %s expired", g.ID),
					Data:    map[string]any{"grant": g.ID}}); err != nil {
					slog.Error("record grant expiry", "error", err)
				}
			}
		}
	}
}

// reconcile ends the work that the last stop of the control plane interrupted, and plans
// again what was not yet in the apply.
func reconcile(ctx context.Context, c *components) error {
	ended, err := c.orch.Reconcile(ctx)
	if err != nil {
		return err
	}
	if ended.Jobs+ended.Grants+len(ended.Failed)+len(ended.Replan) > 0 {
		slog.Warn("ended the work that the last stop interrupted", "jobs", ended.Jobs,
			"grants", ended.Grants, "failed", strings.Join(ended.Failed, ","),
			"planned_again", strings.Join(ended.Replan, ","))
	}
	// The plans run one after the other, as they did before the stop.
	go func() {
		for _, id := range ended.Replan {
			if _, err := c.orch.PlanWorkItem(context.WithoutCancel(ctx), id); err != nil {
				slog.Error("plan a work item again after a restart", "work_item", id, "error", err)
			}
		}
	}()
	return nil
}

// stopModelServer stops the model server that this control plane started. After a
// restart the control plane does not know that it started one, so the server must not
// stay.
func stopModelServer(control *ollama.Control) {
	if control == nil || control.Server == nil {
		return
	}
	if err := control.Server.Stop(); err != nil {
		slog.Error("stop the model server", "error", err)
	}
}

func up(ctx context.Context, o options) error {
	c, err := build(ctx, o)
	if err != nil {
		return err
	}
	defer c.db.Close()
	defer c.writer.Close()
	// A control plane that lost its hold on the database must stop: another one can write.
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	go func() {
		select {
		case <-c.writer.Lost():
			stop(errors.New("the connection that holds the writer lock of the database broke"))
		case <-ctx.Done():
		}
	}()
	if err := reconcile(ctx, c); err != nil {
		return err
	}
	if err := recordPack(c.db, c.log, o); err != nil {
		return err
	}
	server := &api.Server{App: o.app, Orch: c.orch, Audit: c.log, Forge: c.forge,
		Broker:  c.broker,
		Monitor: c.monitor, Notifier: c.notifier, Agent: c.agent, Detector: c.detector,
		Sim: c.sim, Clock: c.clock, Runners: c.dispatcher.Handler(), LocalModel: c.localModel}
	defer stopModelServer(c.localModel)
	if o.demo && o.operator != "" {
		server.DefaultUser = operatorUserName(o.operator)
	}
	if _, err := fs.Stat(o.source, "web/dist/index.html"); err == nil {
		if server.Web, err = fs.Sub(o.source, "web/dist"); err != nil {
			return fmt.Errorf("open the built dashboard: %w", err)
		}
	} else {
		slog.Warn("the dashboard is not built; only the API is served. Run `make web`")
	}
	if err := withSignIn(ctx, server, c, o); err != nil {
		return err
	}
	handler, err := server.Handler()
	if err != nil {
		return err
	}
	if o.demo {
		go func() {
			if err := runSink(ctx, o.sinkAddr); err != nil {
				slog.Error("webhook sink", "error", err)
			}
		}()
	}
	go c.notifier.Run(ctx)
	go c.chat.run(ctx)
	go schedulerLoop(ctx, c)
	if o.cloud == "local" {
		go c.monitor.Run(ctx, []string{o.env}, 10*time.Second)
		go c.detector.Run(ctx, []string{o.env}, 20*time.Second)
		if err := firstRun(ctx, c, o); err != nil {
			return err
		}
	} else {
		// The runner and the broker can apply only the local target today. On a cloud the
		// control plane starts and serves the dashboard, and proposes no deployment.
		slog.Warn("the control plane does not install an environment on this cloud yet",
			"cloud", o.cloud, "env", o.env)
	}
	httpServer := &http.Server{Addr: o.addr, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("stop control plane", "error", err)
		}
	}()
	slog.Info("control plane listens", "dashboard", "http://"+o.addr,
		"agent_mode", c.agent.LLM.Mode(), "mail", "http://127.0.0.1:8025",
		"webhook_sink", "http://"+o.sinkAddr)
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("control plane on %s: %w", o.addr, err)
	}
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return nil
}
