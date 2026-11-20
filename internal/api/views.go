package api

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/detect"
	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/store"
)

func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func (s *Server) envNames() []string {
	names := []string{}
	for name := range s.Orch.Envs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// versions reads the running versions of an environment from a snapshot. The key of the
// application is the name of its pack.
func versions(snap monitor.Snapshot, app pack.Pack) map[string]string {
	return map[string]string{
		"kubernetes": str(snap.Layers["20-cluster"], "kubernetes_version"),
		"node_image": str(snap.Layers["20-cluster"], "node_image_version"),
		"postgres":   str(snap.Layers["30-data"], "postgres_version"),
		app.Name:     str(snap.Layers[app.Layer], app.VersionVariable),
	}
}

// liveSnapshot builds the current state without storing it.
func (s *Server) liveSnapshot(env string) (monitor.Snapshot, error) {
	snap := monitor.Snapshot{TS: store.FormatTime(s.Clock.Now()), Env: env, Reason: "live",
		Layers: map[string]map[string]any{}}
	for _, layer := range orchestrator.Layers {
		outputs, err := store.LayerOutputs(s.Orch.DB, env, layer)
		if err != nil {
			return monitor.Snapshot{}, err
		}
		if len(outputs) > 0 {
			snap.Layers[layer] = outputs
		}
	}
	samples, err := s.Monitor.History(env, "", "", 1)
	if err != nil {
		return monitor.Snapshot{}, err
	}
	if len(samples) == 1 {
		snap.Health = &samples[0]
	}
	return snap, nil
}

// snapshotFor returns the state of env at asOf, or the live state when asOf is empty.
func (s *Server) snapshotFor(env, asOf string) (monitor.Snapshot, error) {
	if asOf == "" {
		return s.liveSnapshot(env)
	}
	snap, found, err := s.Monitor.SnapshotAt(env, asOf)
	if err != nil {
		return monitor.Snapshot{}, err
	}
	if !found {
		return monitor.Snapshot{TS: asOf, Env: env, Reason: "before the first snapshot",
			Layers: map[string]map[string]any{}}, nil
	}
	return snap, nil
}

func isOpen(state string) bool {
	return !slices.Contains([]string{orchestrator.StateDone, orchestrator.StateFailed,
		orchestrator.StateRolledBack, orchestrator.StateRejected}, state)
}

func (s *Server) catchUp(user orchestrator.User, items []orchestrator.WorkItem) (map[string]any, error) {
	since := user.LastSeen
	if since == "" {
		since = store.FormatTime(s.Clock.Now().Add(-14 * 24 * time.Hour))
	}
	events, err := s.Audit.Query(audit.Filter{From: since, Limit: 100000, Types: []string{
		"workitem.state_changed", "incident.opened"}})
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, e := range events {
		switch {
		case e.Type == "incident.opened":
			counts["incidents"]++
		case e.Data["to"] == orchestrator.StateDone && e.Data["tier"] != "T0" &&
			e.Data["tier"] != "":
			counts["applied"]++
		case e.Data["to"] == orchestrator.StateFailed:
			counts["failed"]++
		case e.Data["to"] == orchestrator.StateRolledBack:
			counts["rolled_back"]++
		}
	}
	waiting := 0
	for _, wi := range items {
		if wi.State == orchestrator.StateAwaitingApproval {
			waiting++
		}
	}
	narrative := fmt.Sprintf("Since %s: %d changes applied, %d failed, %d rolled back, "+
		"%d incidents. %d decisions wait for an approver.", since[:10], counts["applied"],
		counts["failed"], counts["rolled_back"], counts["incidents"], waiting)
	return map[string]any{"since": since, "applied": counts["applied"],
		"failed": counts["failed"], "rolled_back": counts["rolled_back"],
		"incidents": counts["incidents"], "waiting": waiting, "narrative": narrative,
		"narrative_source": "counts from the audit log"}, nil
}

func (s *Server) getOverview(r *http.Request) (any, error) {
	user, err := s.currentUser(r)
	if err != nil {
		return nil, err
	}
	asOf := r.URL.Query().Get("as_of")
	items, err := s.itemsAsOf(asOf)
	if err != nil {
		return nil, err
	}
	environments := []map[string]any{}
	for _, name := range s.envNames() {
		snap, err := s.snapshotFor(name, asOf)
		if err != nil {
			return nil, err
		}
		layers := map[string]bool{}
		for _, layer := range orchestrator.Layers {
			_, layers[layer] = snap.Layers[layer]
		}
		open := 0
		for _, wi := range items {
			if wi.Env == name && isOpen(wi.State) {
				open++
			}
		}
		environments = append(environments, map[string]any{"name": name,
			"cloud": s.Orch.Envs[name].Cloud, "layers": layers, "versions": versions(snap, s.App),
			"health": snap.Health, "app_url": str(snap.Layers[s.App.Layer], s.App.URLOutput),
			"open_work_items": open, "deployed": layers[s.App.Layer], "snapshot_ts": snap.TS})
	}
	stateCounts := map[string]int{}
	waiting := []orchestrator.WorkItem{}
	for _, wi := range items {
		stateCounts[wi.State]++
		if wi.State == orchestrator.StateAwaitingApproval {
			waiting = append(waiting, wi)
		}
	}
	catchUp, err := s.catchUp(user, items)
	if err != nil {
		return nil, err
	}
	chain, err := s.Audit.Verify()
	if err != nil {
		return nil, err
	}
	return map[string]any{"now": store.FormatTime(s.Clock.Now()), "as_of": asOf,
		"environments": environments, "state_counts": stateCounts, "waiting": waiting,
		"catch_up": catchUp, "chain": chain, "agent_mode": s.Agent.LLM.Mode(),
		"planned_clouds": []string{"aws", "gcp", "azure"}}, nil
}

var markerCategory = map[string]string{
	"approval.recorded": "approval", "approval.requested": "approval",
	"incident.opened": "incident", "incident.resolved": "incident",
	"finding.opened": "finding", "grant.issued": "grant", "grant.debug_issued": "grant",
	"runbook.succeeded": "runbook", "runbook.failed": "runbook",
	"apply.succeeded": "apply", "apply.failed": "apply", "apply.refused": "apply",
	"verify.failed": "verify", "sim.release_published": "external",
	"sim.advisory_published": "external", "sim.manual_change": "external",
	"sim.fault_injected": "external", "standing_approval.signed": "approval",
}

func marker(e audit.Event) (map[string]any, bool) {
	category, ok := markerCategory[e.Type]
	outcome := ""
	if e.Type == "workitem.state_changed" {
		to, _ := e.Data["to"].(string)
		switch to {
		case orchestrator.StateDone, orchestrator.StateFailed, orchestrator.StateRolledBack,
			orchestrator.StateRejected:
			category, outcome, ok = "change", to, true
		}
	}
	if !ok {
		return nil, false
	}
	if strings.HasSuffix(e.Type, ".failed") || strings.HasSuffix(e.Type, ".refused") {
		outcome = "Failed"
	}
	return map[string]any{"seq": e.Seq, "ts": e.TS, "env": e.Env, "layer": e.Layer,
		"type": e.Type, "category": category, "outcome": outcome, "work_item": e.WorkItem,
		"summary": e.Summary, "tier": e.Data["tier"], "kind": e.Data["kind"],
		"actor": e.Actor}, true
}

func healthBuckets(samples []monitor.Sample, from, to time.Time, count int) []map[string]any {
	width := to.Sub(from) / time.Duration(count)
	if width <= 0 {
		return []map[string]any{}
	}
	type bucket struct{ ok, total int }
	buckets := make([]bucket, count)
	for _, s := range samples {
		index := int(store.ParseTime(s.TS).Sub(from) / width)
		if index < 0 || index >= count {
			continue
		}
		buckets[index].ok += s.OK
		buckets[index].total += s.Total
	}
	out := []map[string]any{}
	for i, b := range buckets {
		if b.total == 0 {
			continue
		}
		out = append(out, map[string]any{"ts": store.FormatTime(from.Add(width * time.Duration(i))),
			"end":        store.FormatTime(from.Add(width * time.Duration(i+1))),
			"attainment": float64(b.ok) / float64(b.total), "requests": b.total})
	}
	return out
}

func (s *Server) future(now string) ([]map[string]any, error) {
	future := []map[string]any{}
	items, err := s.Orch.List()
	if err != nil {
		return nil, err
	}
	for _, wi := range items {
		if wi.State == orchestrator.StateScheduled {
			future = append(future, map[string]any{"ts": wi.ScheduledFor, "env": wi.Env,
				"kind": "scheduled", "title": wi.Title, "work_item": wi.ID, "tier": wi.Tier})
		}
	}
	advisories, err := s.Detector.Advisories()
	if err != nil {
		return nil, err
	}
	for _, adv := range advisories {
		if adv.Due <= now {
			continue
		}
		for _, env := range s.envNames() {
			future = append(future, map[string]any{"ts": adv.Due, "env": env,
				"kind": "forecast", "title": adv.Title, "advisory": adv.ID})
		}
	}
	storage, err := s.storageForecasts(now)
	if err != nil {
		return nil, err
	}
	future = append(future, storage...)
	t := store.ParseTime(now)
	nextBackup := time.Date(t.Year(), t.Month(), t.Day()+7-int(t.Weekday()), 3, 0, 0, 0, time.UTC)
	nextDrill := time.Date(t.Year(), t.Month()+1, 1, 4, 0, 0, 0, time.UTC)
	for _, env := range s.envNames() {
		future = append(future,
			map[string]any{"ts": store.FormatTime(nextBackup), "env": env, "kind": "window",
				"title": "Weekly database backup"},
			map[string]any{"ts": store.FormatTime(nextDrill), "env": env, "kind": "window",
				"title": "Monthly restore drill"})
	}
	return future, nil
}

func (s *Server) getTimeline(r *http.Request) (any, error) {
	q := r.URL.Query()
	now := s.Clock.Now()
	from, to := q.Get("from"), q.Get("to")
	if from == "" {
		from = store.FormatTime(now.Add(-45 * 24 * time.Hour))
	}
	if to == "" {
		to = store.FormatTime(now.Add(60 * 24 * time.Hour))
	}
	events, err := s.Audit.Query(audit.Filter{From: from, To: to, Env: q.Get("env"),
		Limit: 100000})
	if err != nil {
		return nil, err
	}
	markers := []map[string]any{}
	for _, e := range events {
		if m, ok := marker(e); ok {
			markers = append(markers, m)
		}
	}
	health := map[string]any{}
	healthTo := min(to, store.FormatTime(now))
	for _, env := range s.envNames() {
		samples, err := s.Monitor.History(env, from, healthTo, 200000)
		if err != nil {
			return nil, err
		}
		health[env] = healthBuckets(samples, store.ParseTime(from), store.ParseTime(healthTo),
			intParam(r, "buckets", 240))
	}
	future, err := s.future(store.FormatTime(now))
	if err != nil {
		return nil, err
	}
	lanes := []map[string]any{}
	for _, env := range s.envNames() {
		lanes = append(lanes, map[string]any{"env": env, "cloud": s.Orch.Envs[env].Cloud,
			"layers": orchestrator.Layers})
	}
	return map[string]any{"now": store.FormatTime(now), "from": from, "to": to, "lanes": lanes,
		"markers": markers, "future": future, "health": health}, nil
}

type topoNode struct {
	ID      string   `json:"id"`
	Layer   string   `json:"layer"`
	Kind    string   `json:"kind"`
	Label   string   `json:"label"`
	Detail  string   `json:"detail"`
	Version string   `json:"version"`
	Status  string   `json:"status"`
	Badges  []string `json:"badges"`
}

func topologyNodes(snap monitor.Snapshot, app pack.Pack) []topoNode {
	nodes := []topoNode{}
	add := func(layer, id, kind, label, detail, version string) {
		if _, ok := snap.Layers[layer]; !ok {
			return
		}
		nodes = append(nodes, topoNode{ID: id, Layer: layer, Kind: kind, Label: label,
			Detail: detail, Version: version, Status: "ok", Badges: []string{}})
	}
	l := snap.Layers
	add("10-network", "network", "network", "Network", str(l["10-network"], "network_id"), "")
	add("10-network", "registry", "registry", "Image registry",
		str(l["10-network"], "image_registry"), "")
	add("20-cluster", "cluster", "cluster", "Kubernetes cluster",
		str(l["20-cluster"], "cluster_name"), str(l["20-cluster"], "kubernetes_version"))
	add("20-cluster", "nodes", "nodes", "Node pool", "node image",
		str(l["20-cluster"], "node_image_version"))
	add("30-data", "postgres", "database", "Postgres", str(l["30-data"], "postgres_host"),
		str(l["30-data"], "postgres_version"))
	add("30-data", "sharedfs", "filesystem", "Shared file system",
		str(l["30-data"], "rwx_storage_class"), "")
	add("30-data", "backups", "bucket", "Backups", str(l["30-data"], "backup_bucket"), "")
	add("40-platform", "lb", "loadbalancer", "Load balancer",
		str(l["40-platform"], "public_address"), "")
	add("40-platform", "secrets", "secret", "Application secret",
		str(l["40-platform"], "app_secret_name"), "")
	running := str(l[app.Layer], app.VersionVariable)
	for _, svc := range app.TopologyNodes() {
		add(app.Layer, svc, "service", svc, app.Title+" "+svc, running)
	}
	return nodes
}

// platformEdges join the nodes of the platform. The pack has the edges of the application.
var platformEdges = [][2]string{{"postgres", "backups"}, {"cluster", "network"},
	{"nodes", "cluster"}, {"nodes", "registry"}}

func (s *Server) getTopology(r *http.Request) (any, error) {
	q := r.URL.Query()
	env, asOf := q.Get("env"), q.Get("as_of")
	if _, ok := s.Orch.Envs[env]; !ok {
		return nil, badRequest("environment %q is not configured", env)
	}
	snap, err := s.snapshotFor(env, asOf)
	if err != nil {
		return nil, err
	}
	nodes := topologyNodes(snap, s.App)
	items, err := s.itemsAsOf(asOf)
	if err != nil {
		return nil, err
	}
	pending := map[string]string{}
	incident := false
	for _, wi := range items {
		if wi.Env != env || !isOpen(wi.State) {
			continue
		}
		if wi.Kind == "incident" {
			incident = true
		}
		badge := "pending change"
		if wi.Kind == "drift" {
			badge = "drift"
		}
		for _, layer := range wi.Layers {
			pending[layer] = badge
		}
	}
	unhealthy := snap.Health != nil && snap.Health.Total > 0 &&
		float64(snap.Health.OK)/float64(snap.Health.Total) < 0.9
	present := map[string]bool{}
	for i := range nodes {
		present[nodes[i].ID] = true
		if badge, ok := pending[nodes[i].Layer]; ok {
			nodes[i].Badges = append(nodes[i].Badges, badge)
		}
		if nodes[i].ID == "api" && (unhealthy || incident) {
			nodes[i].Status = "degraded"
			if incident {
				nodes[i].Badges = append(nodes[i].Badges, "incident")
			}
		}
	}
	edges := []map[string]string{}
	for _, e := range slices.Concat(s.App.Topology.Edges, platformEdges) {
		if present[e[0]] && present[e[1]] {
			edges = append(edges, map[string]string{"from": e[0], "to": e[1]})
		}
	}
	return map[string]any{"env": env, "as_of": asOf, "snapshot_ts": snap.TS,
		"snapshot_reason": snap.Reason, "layers": orchestrator.Layers, "nodes": nodes,
		"edges": edges, "health": snap.Health}, nil
}

func minorsBehind(current, latest string) int {
	if current == "" || latest == "" || current == latest {
		return 0
	}
	c, l := strings.Split(current, "."), strings.Split(latest, ".")
	for i := 0; i < len(c) && i < len(l); i++ {
		if c[i] != l[i] {
			var cv, lv int
			fmt.Sscanf(c[i], "%d", &cv)
			fmt.Sscanf(l[i], "%d", &lv)
			return max(lv-cv, 0)
		}
	}
	return 0
}

func (s *Server) latestVersions() (map[string]string, error) {
	app := s.App
	latest := map[string]string{"kubernetes": "1.37", "node_image": "", "postgres": "",
		app.Name: ""}
	advisories, err := s.Detector.Advisories()
	if err != nil {
		return nil, err
	}
	for _, adv := range advisories {
		if adv.Variable == "postgres_version" {
			latest["postgres"] = adv.Value
		}
	}
	if s.Forge.Exists(app.Releases.Repository) {
		entries, err := s.Forge.Tree(app.Releases.Repository, "main", app.Releases.Directory)
		if err != nil {
			return nil, err
		}
		withdrawn, err := s.rolledBackVersions()
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			v := app.ReleaseOf(e.Name)
			if withdrawn[v] {
				continue
			}
			if latest[app.Name] == "" || detect.Newer(v, latest[app.Name]) {
				latest[app.Name] = v
			}
		}
	}
	return latest, nil
}

// rolledBackVersions lists the application versions whose upgrade was rolled back. Such a
// version is not a target, so the posture matrix does not count it as the latest.
func (s *Server) rolledBackVersions() (map[string]bool, error) {
	findings, err := s.Detector.Findings()
	if err != nil {
		return nil, err
	}
	withdrawn := map[string]bool{}
	for _, f := range findings {
		version, _ := f.Detail["value"].(string)
		if f.State == "rolled back" && f.Detail["variable"] == s.App.VersionVariable {
			withdrawn[version] = true
		}
	}
	return withdrawn, nil
}

func (s *Server) getPosture(r *http.Request) (any, error) {
	asOf := r.URL.Query().Get("as_of")
	latest, err := s.latestVersions()
	if err != nil {
		return nil, err
	}
	components := []string{"kubernetes", "node_image", "postgres", s.App.Name}
	rows := []map[string]any{}
	for _, env := range s.envNames() {
		snap, err := s.snapshotFor(env, asOf)
		if err != nil {
			return nil, err
		}
		current := versions(snap, s.App)
		cells := map[string]any{}
		for _, c := range components {
			newest := latest[c]
			if newest == "" {
				newest = current[c]
			}
			status := "current"
			switch {
			case current[c] == "":
				status = "not deployed"
			case current[c] != newest:
				status = "behind"
			}
			cells[c] = map[string]any{"current": current[c], "latest": newest,
				"behind": minorsBehind(current[c], newest), "status": status}
		}
		rows = append(rows, map[string]any{"env": env, "cells": cells})
	}
	return map[string]any{"components": components, "rows": rows, "as_of": asOf}, nil
}

func (s *Server) getAccess(*http.Request) (any, error) {
	grants, err := s.Broker.List(false)
	if err != nil {
		return nil, err
	}
	active := []any{}
	for _, g := range grants {
		if g.Status == "issued" || g.Status == "active" {
			active = append(active, g)
		}
	}
	identities := []map[string]string{
		{"name": "observer", "standing": "Read-only: Kubernetes view. No secret values",
			"elevation": "None"},
		{"name": "planner", "standing": "None. A token exists only during a plan job",
			"elevation": "Read-only token, 15 minutes, for each plan"},
		{"name": "runbook", "standing": "None",
			"elevation": "Standing approval for one named runbook, token for one job"},
		{"name": "debugger", "standing": "None",
			"elevation": "Time-boxed, read-only, namespace-scoped, session recorded"},
		{"name": "broker", "standing": "Issues tokens for the identities above. Holds the " +
			"administrator credential of the local cluster", "elevation": "None"},
	}
	for _, layer := range orchestrator.Layers[1:] {
		identities = append(identities, map[string]string{"name": "applier-" + layer,
			"standing":  "None",
			"elevation": "A grant bound to one commit and one plan hash, for layer " + layer})
	}
	return map[string]any{"identities": identities, "active": active, "history": grants}, nil
}
