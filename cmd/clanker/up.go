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
	"time"

	"github.com/statik/site-reliability-clanker/internal/agent"
	"github.com/statik/site-reliability-clanker/internal/api"
	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/detect"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/notify"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/runner"
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

var standingRunbooks = []string{"restart-deployment", "db-backup", "restore-drill",
	"db-maintenance"}

func seedUsers(db *sql.DB) error {
	for _, u := range demoUsers {
		if _, err := db.Exec(`INSERT OR IGNORE INTO users (name, display, role, email)
			VALUES (?,?,?,?)`, u.Name, u.Display, u.Role, u.Email); err != nil {
			return fmt.Errorf("seed user %s: %w", u.Name, err)
		}
	}
	return nil
}

func seedForge(f *forge.Forge, source string) error {
	chart := filepath.Join(source, "apps", "paperboat", "chart")
	if err := f.Seed(orchestrator.PlatformRepo, map[string]string{
		"platform":             filepath.Join(source, "platform"),
		"apps/paperboat/chart": chart,
	}, "Seed platform code and the Paperboat chart"); err != nil {
		return err
	}
	if err := f.Seed(orchestrator.RunbooksRepo, map[string]string{
		".": filepath.Join(source, "runbooks"),
	}, "Seed runbooks, policies and notification routes"); err != nil {
		return err
	}
	if f.Exists(detect.PaperboatRepo) {
		return nil
	}
	firstRelease, err := os.MkdirTemp("", "clanker-release-")
	if err != nil {
		return fmt.Errorf("create temporary directory for the first release: %w", err)
	}
	defer os.RemoveAll(firstRelease)
	note, err := os.ReadFile(filepath.Join(source, "apps", "paperboat", "releases", "1.0.0.md"))
	if err != nil {
		return fmt.Errorf("read the 1.0.0 release note: %w", err)
	}
	if err := os.WriteFile(filepath.Join(firstRelease, "1.0.0.md"), note, 0o644); err != nil {
		return fmt.Errorf("stage the 1.0.0 release note: %w", err)
	}
	return f.Seed(detect.PaperboatRepo, map[string]string{"releases": firstRelease,
		"chart": chart}, "Release 1.0.0")
}

func adapters(db *sql.DB, o options) ([]notify.Adapter, error) {
	smtpAddr := envOr("CLANKER_SMTP_ADDR", "127.0.0.1:1025")
	sinkURL := "http://" + o.sinkAddr
	recipients := func(audience string) []string {
		role := map[string]string{"approvers": "approver", "admins": "admin"}[audience]
		query := `SELECT email FROM users WHERE role IN ('approver', 'admin')`
		args := []any{}
		if role != "" {
			query, args = `SELECT email FROM users WHERE role = ?`, []any{role}
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
	return []notify.Adapter{
		notify.Email{Addr: smtpAddr, From: "clanker@example.com", Recipients: recipients},
		notify.Slack{WebhookURL: envOr("CLANKER_SLACK_WEBHOOK", sinkURL+"/slack")},
		notify.Teams{WebhookURL: envOr("CLANKER_TEAMS_WEBHOOK", sinkURL+"/teams")},
		notify.Discord{WebhookURL: envOr("CLANKER_DISCORD_WEBHOOK", sinkURL+"/discord")},
	}, nil
}

// components holds everything that `up` starts.
type components struct {
	db       *sql.DB
	clock    *sim.Clock
	log      *audit.Log
	forge    *forge.Forge
	runner   *runner.Runner
	broker   *broker.Broker
	orch     *orchestrator.Orchestrator
	monitor  *monitor.Monitor
	notifier *notify.Notifier
	agent    *agent.Agent
	detector *detect.Detector
	sim      *sim.Simulator
}

func build(o options) (*components, error) {
	if err := os.MkdirAll(o.dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory %s: %w", o.dataDir, err)
	}
	c := &components{}
	var err error
	if c.db, err = store.Open(filepath.Join(o.dataDir, "clanker.db")); err != nil {
		return nil, err
	}
	if c.clock, err = sim.NewClock(c.db); err != nil {
		return nil, err
	}
	c.log = audit.New(c.db, c.clock)
	if c.forge, err = forge.New(filepath.Join(o.dataDir, "repos"), c.db); err != nil {
		return nil, err
	}
	if c.runner, err = runner.New(o.dataDir); err != nil {
		return nil, err
	}
	if o.demo {
		c.runner.ExtraEnv = []string{"TF_VAR_admin_token=" + demoAdminToken}
	}
	adminKubeconfig := func(env string) (string, error) {
		path := filepath.Join(c.runner.StateDir(env), "kubeconfig")
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("environment %s has no cluster yet: apply layer 20-cluster "+
				"first", env)
		}
		return path, nil
	}
	if c.broker, err = broker.New(c.db, c.clock, o.dataDir, adminKubeconfig); err != nil {
		return nil, err
	}
	c.orch = &orchestrator.Orchestrator{DB: c.db, Audit: c.log, Forge: c.forge,
		Runner: c.runner, Broker: c.broker, Clock: c.clock, DataDir: o.dataDir,
		Envs: map[string]orchestrator.EnvConfig{o.env: {Name: o.env, Cloud: o.cloud}}}
	c.monitor = monitor.New(c.db, c.runner, c.clock)
	if err := seedUsers(c.db); err != nil {
		return nil, err
	}
	if err := seedForge(c.forge, o.sourceDir); err != nil {
		return nil, err
	}
	channelAdapters, err := adapters(c.db, o)
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
	var llm agent.LLM = agent.Replay{}
	// CLANKER_AGENT_MODE=replay forces recorded responses, for a repeatable demo.
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" &&
		os.Getenv("CLANKER_AGENT_MODE") != "replay" {
		llm = agent.NewOpenRouter(key)
	}
	c.agent = &agent.Agent{DB: c.db, Audit: c.log, Orch: c.orch, Forge: c.forge,
		Monitor: c.monitor, Clock: c.clock, LLM: llm, Models: agent.DefaultModels(),
		DailyBudgetUSD: 5}
	c.detector = &detect.Detector{DB: c.db, Audit: c.log, Orch: c.orch, Forge: c.forge,
		Monitor: c.monitor, Clock: c.clock, OnFinding: c.agent.HandleFinding}
	c.sim = &sim.Simulator{DB: c.db, Clock: c.clock, Audit: c.log, Forge: c.forge,
		Detector: c.detector, Orch: c.orch, Monitor: c.monitor, Notifier: c.notifier,
		SourceDir: o.sourceDir, AdminToken: demoAdminToken, AdminKubeconfig: adminKubeconfig,
		Env: o.env}
	c.orch.Hooks = orchestrator.Hooks{Explain: c.agent.Explain, Verify: c.monitor.Verify,
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
	return c, nil
}

// publishImages builds the Paperboat releases and pushes them to the registry of the local
// target. It stands in for the release pipeline of the application, which the prototype
// does not have. It runs when the registry exists, before the application layer needs it.
func publishImages(ctx context.Context, c *components, o options) error {
	outputs, err := c.runner.Outputs(o.env, "10-network")
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
	cmd := exec.CommandContext(ctx, filepath.Join(o.sourceDir, "apps", "paperboat", "build.sh"),
		registry)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build and push the Paperboat images to %s: %w; see %s", registry,
			err, logPath)
	}
	_, err = c.log.Append(audit.Event{Type: "sim.images_published", Actor: "simulator",
		Env: o.env, Summary: "Paperboat images for all releases are in the registry " + registry,
		Data: map[string]any{"registry": registry}})
	return err
}

// firstRun signs the demo standing approvals and opens the blank-slate work item when the
// control plane has no work item yet.
func firstRun(ctx context.Context, c *components, o options) error {
	items, err := c.orch.List()
	if err != nil || len(items) > 0 {
		return err
	}
	if o.demo {
		for _, name := range standingRunbooks {
			if err := c.orch.SignStandingApproval(name, "carol"); err != nil {
				return err
			}
		}
	}
	wi, err := c.orch.Create(orchestrator.NewWorkItem{
		Title: "Blank-slate deployment of " + o.env, Kind: "blank-slate", Env: o.env,
		Layers: orchestrator.Layers, Trigger: "First run: no layer of this environment exists",
		Actor: "orchestrator"})
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

func up(ctx context.Context, o options) error {
	c, err := build(o)
	if err != nil {
		return err
	}
	defer c.db.Close()
	server := &api.Server{Orch: c.orch, Audit: c.log, Forge: c.forge, Broker: c.broker,
		Monitor: c.monitor, Notifier: c.notifier, Agent: c.agent, Detector: c.detector,
		Sim: c.sim, Clock: c.clock}
	webDir := filepath.Join(o.sourceDir, "web", "dist")
	if _, err := os.Stat(filepath.Join(webDir, "index.html")); err == nil {
		server.Web = os.DirFS(webDir).(fs.FS)
	} else {
		slog.Warn("the dashboard is not built; only the API is served. Run `make web`")
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
	return nil
}
