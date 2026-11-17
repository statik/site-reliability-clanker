// Package detect contains the detectors: scheduled, read-only checks in plain code that open
// findings. No detector uses a model.
package detect

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// PaperboatRepo is the forge repository with the application releases.
const PaperboatRepo = "paperboat"

// AdvisoriesKey is the kv key of the advisory feed.
const AdvisoriesKey = "feed.advisories"

// Finding is something a detector noticed.
type Finding struct {
	ID       int64          `json:"id"`
	Detector string         `json:"detector"`
	Env      string         `json:"env"`
	Key      string         `json:"key"`
	Title    string         `json:"title"`
	Detail   map[string]any `json:"detail"`
	State    string         `json:"state"`
	WorkItem string         `json:"work_item"`
	At       string         `json:"at"`
}

// Advisory is one entry of the advisory feed: a CVE, a pending patch or an end-of-support date.
type Advisory struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Severity string `json:"severity"`
	// Layer and Variable name the tfvars setting that fixes it. Value is the fixed value.
	Layer     string `json:"layer"`
	Variable  string `json:"variable"`
	Value     string `json:"value"`
	Due       string `json:"due"`
	Published string `json:"published"`
}

// Clock gives the virtual time.
type Clock interface{ Now() time.Time }

// Detector runs the checks.
type Detector struct {
	DB      *sql.DB
	Audit   *audit.Log
	Orch    *orchestrator.Orchestrator
	Forge   *forge.Forge
	Monitor *monitor.Monitor
	Clock   Clock
	// OnFinding receives each new finding. The agent takes it from there.
	OnFinding func(ctx context.Context, f Finding)
}

func (d *Detector) open(ctx context.Context, f Finding) error {
	detail, _ := json.Marshal(f.Detail)
	f.At, f.State = store.FormatTime(d.Clock.Now()), "open"
	err := d.DB.QueryRow(`INSERT INTO findings (detector, env, key, title, detail, state, at)
		VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (key) DO NOTHING RETURNING id`, f.Detector,
		f.Env, f.Key, f.Title, string(detail), f.State, f.At).Scan(&f.ID)
	// No row comes back when a finding with this key exists already.
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("record finding %s: %w", f.Key, err)
	}
	if _, err := d.Audit.Append(audit.Event{Type: "finding.opened", Actor: "detector:" + f.Detector,
		Env: f.Env, Summary: f.Title, Data: map[string]any{"key": f.Key, "detail": f.Detail}}); err != nil {
		return err
	}
	if d.OnFinding != nil {
		go d.OnFinding(context.WithoutCancel(ctx), f)
	}
	return nil
}

// SetWorkItem links a finding to the work item that handles it.
func (d *Detector) SetWorkItem(key, workItem, state string) error {
	_, err := d.DB.Exec(`UPDATE findings SET work_item = $1, state = $2 WHERE key = $3`, workItem,
		state, key)
	if err != nil {
		return fmt.Errorf("link finding %s to %s: %w", key, workItem, err)
	}
	return nil
}

// Findings lists findings, newest first.
func (d *Detector) Findings() ([]Finding, error) {
	rows, err := d.DB.Query(`SELECT id, detector, env, key, title, detail, state, work_item, at
		FROM findings ORDER BY id DESC LIMIT 500`)
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	defer rows.Close()
	findings := []Finding{}
	for rows.Next() {
		var f Finding
		var detail string
		if err := rows.Scan(&f.ID, &f.Detector, &f.Env, &f.Key, &f.Title, &detail, &f.State,
			&f.WorkItem, &f.At); err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		if err := json.Unmarshal([]byte(detail), &f.Detail); err != nil {
			return nil, fmt.Errorf("decode detail of finding %d: %w", f.ID, err)
		}
		findings = append(findings, f)
	}
	return findings, rows.Err()
}

// versionOrder turns "1.2.0-bad" into comparable numbers. A suffix does not change the order.
func versionOrder(v string) [3]int {
	var out [3]int
	core, _, _ := strings.Cut(v, "-")
	for i, part := range strings.SplitN(core, ".", 3) {
		out[i], _ = strconv.Atoi(part)
	}
	return out
}

// Newer reports whether version a is newer than version b.
func Newer(a, b string) bool {
	va, vb := versionOrder(a), versionOrder(b)
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

func (d *Detector) runningVersion(env, layer, output string) string {
	outputs, err := d.Orch.Runner.Outputs(env, layer)
	if err != nil {
		return ""
	}
	v, _ := outputs[output].(string)
	return v
}

// CheckReleases opens a finding when the paperboat repository has a release newer than the
// one that runs.
func (d *Detector) CheckReleases(ctx context.Context, env string) error {
	running := d.runningVersion(env, "50-app", "paperboat_version")
	if running == "" || !d.Forge.Exists(PaperboatRepo) {
		return nil
	}
	entries, err := d.Forge.Tree(PaperboatRepo, "main", "releases")
	if err != nil {
		return err
	}
	versions := []string{}
	for _, e := range entries {
		v := strings.TrimSuffix(e.Name, ".md")
		if Newer(v, running) {
			versions = append(versions, v)
		}
	}
	if len(versions) == 0 {
		return nil
	}
	sort.Slice(versions, func(i, j int) bool { return Newer(versions[j], versions[i]) })
	target := versions[len(versions)-1]
	covered, err := d.hasFindingFor(env, "paperboat_version", target)
	if err != nil || covered {
		return err
	}
	return d.open(ctx, Finding{Detector: "release", Env: env,
		Key:   fmt.Sprintf("release:%s:%s", env, target),
		Title: fmt.Sprintf("Paperboat %s is published; %s runs %s", target, env, running),
		Detail: map[string]any{"kind": "app-upgrade", "layer": "50-app",
			"variable": "paperboat_version", "value": target, "running": running,
			"release_note": "releases/" + target + ".md"}})
}

// hasFindingFor reports whether a finding of env already asks for this variable and value.
func (d *Detector) hasFindingFor(env, variable, value string) (bool, error) {
	findings, err := d.Findings()
	if err != nil {
		return false, err
	}
	for _, f := range findings {
		if f.Env == env && f.Detail["variable"] == variable && f.Detail["value"] == value {
			return true, nil
		}
	}
	return false, nil
}

// Advisories reads the advisory feed.
func (d *Detector) Advisories() ([]Advisory, error) {
	raw, err := store.GetKV(d.DB, AdvisoriesKey)
	if err != nil || raw == "" {
		return nil, err
	}
	var advisories []Advisory
	if err := json.Unmarshal([]byte(raw), &advisories); err != nil {
		return nil, fmt.Errorf("decode advisory feed: %w", err)
	}
	return advisories, nil
}

// CheckAdvisories opens a finding for each advisory whose fix is not applied.
func (d *Detector) CheckAdvisories(ctx context.Context, env string) error {
	advisories, err := d.Advisories()
	if err != nil {
		return err
	}
	for _, adv := range advisories {
		if adv.Variable == "" {
			continue
		}
		running := d.runningVersion(env, adv.Layer, adv.Variable)
		if running == "" || running == adv.Value || Newer(running, adv.Value) {
			continue
		}
		kind := map[string]string{"base-image-cve": "app-upgrade",
			"postgres-minor": "postgres-patch", "kubernetes-eol": "kubernetes-upgrade",
			"node-image": "node-patch"}[adv.Kind]
		if err := d.open(ctx, Finding{Detector: "advisory", Env: env,
			Key:   fmt.Sprintf("advisory:%s:%s", env, adv.ID),
			Title: adv.Title,
			Detail: map[string]any{"kind": kind, "advisory": adv.ID, "severity": adv.Severity,
				"layer": adv.Layer, "variable": adv.Variable, "value": adv.Value,
				"running": running, "due": adv.Due}}); err != nil {
			return err
		}
	}
	return nil
}

// CheckDrift plans every applied layer at the head of main. A plan with changes means that
// the environment differs from the code. The work item of the check becomes the
// reconciliation and waits for approval; without changes it closes as a tier T0 check.
func (d *Detector) CheckDrift(ctx context.Context, env string) (orchestrator.WorkItem, error) {
	layers := []string{}
	for _, layer := range []string{"30-data", "40-platform", "50-app"} {
		if d.runningLayer(env, layer) {
			layers = append(layers, layer)
		}
	}
	if len(layers) == 0 {
		return orchestrator.WorkItem{}, fmt.Errorf("drift check of %s: no layer is applied", env)
	}
	wi, err := d.Orch.Create(orchestrator.NewWorkItem{Title: "Drift check and reconciliation",
		Kind: "drift", Env: env, Layers: layers, Actor: "detector:drift",
		Trigger: "Nightly plan of every layer against the code on main"})
	if err != nil {
		return orchestrator.WorkItem{}, err
	}
	wi, err = d.Orch.PlanWorkItem(ctx, wi.ID)
	if err != nil {
		return wi, err
	}
	if wi.State != orchestrator.StateAwaitingApproval {
		return wi, nil
	}
	plans, err := d.Orch.Plans(wi.ID, false)
	if err != nil {
		return wi, err
	}
	drifted := []string{}
	for _, p := range plans {
		for _, rc := range p.Facts.Resources {
			drifted = append(drifted, rc.Address)
		}
	}
	err = d.open(ctx, Finding{Detector: "drift", Env: env,
		Key:    fmt.Sprintf("drift:%s:%s", env, wi.ID),
		Title:  fmt.Sprintf("Drift in %s: %s differs from the code", env, strings.Join(drifted, ", ")),
		Detail: map[string]any{"kind": "drift", "resources": drifted, "work_item": wi.ID}})
	if err != nil {
		return wi, err
	}
	return wi, d.SetWorkItem(fmt.Sprintf("drift:%s:%s", env, wi.ID), wi.ID, "drafted")
}

func (d *Detector) runningLayer(env, layer string) bool {
	outputs, err := d.Orch.Runner.Outputs(env, layer)
	return err == nil && len(outputs) > 0
}

// CheckHealth opens an incident finding when the last three samples each have an error rate
// over twenty percent and no change is in progress.
func (d *Detector) CheckHealth(ctx context.Context, env string) error {
	samples, err := d.Monitor.History(env, "", "", 3)
	if err != nil || len(samples) < 3 {
		return err
	}
	for _, s := range samples {
		if s.Total == 0 || float64(s.Total-s.OK)/float64(s.Total) <= 0.2 {
			return nil
		}
	}
	items, err := d.Orch.List()
	if err != nil {
		return err
	}
	for _, wi := range items {
		busy := wi.State == orchestrator.StateApplying || wi.State == orchestrator.StateVerifying
		openIncident := wi.Kind == "incident" && wi.State != orchestrator.StateDone &&
			wi.State != orchestrator.StateFailed
		if wi.Env == env && (busy || openIncident) {
			return nil
		}
	}
	last := samples[len(samples)-1]
	return d.open(ctx, Finding{Detector: "health", Env: env,
		// One incident for each environment and hour, so an incident that waits for a human
		// does not open again on every check.
		Key: fmt.Sprintf("incident:%s:%s", env, last.TS[:13]),
		Title: fmt.Sprintf("Paperboat in %s fails %d of %d inventory requests", env,
			last.Total-last.OK, last.Total),
		Detail: map[string]any{"kind": "incident", "ok": last.OK, "total": last.Total,
			"p95_ms": last.P95MS, "version": last.Version}})
}

// Run executes the cheap checks on an interval until ctx ends. The drift check is not part of
// the loop: it runs on its nightly schedule.
func (d *Detector) Run(ctx context.Context, envs []string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, env := range envs {
				d.RunChecks(ctx, env)
			}
		}
	}
}

// RunChecks runs the release, advisory and health checks once.
func (d *Detector) RunChecks(ctx context.Context, env string) {
	type namedCheck struct {
		name string
		run  func(context.Context, string) error
	}
	checks := []namedCheck{{"advisory", d.CheckAdvisories}, {"release", d.CheckReleases},
		{"health", d.CheckHealth}}
	for _, check := range checks {
		name := check.name
		if err := check.run(ctx, env); err != nil {
			if _, auditErr := d.Audit.Append(audit.Event{Type: "detector.failed",
				Actor: "detector:" + name, Env: env, Summary: err.Error()}); auditErr != nil {
				panic(fmt.Sprintf("audit log is not writable, stopping: %v", auditErr))
			}
		}
	}
}
