// Package monitor probes the managed application, keeps the health history, takes
// point-in-time snapshots for the history store, and verifies an environment after an apply.
package monitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// Clock gives the virtual time.
type Clock interface{ Now() time.Time }

// Monitor probes environments.
type Monitor struct {
	DB    *sql.DB
	Clock Clock
	// App is the application pack. It names the layer, the outputs and the checks.
	App    pack.Pack
	client *http.Client
}

// New returns a monitor.
func New(db *sql.DB, clock Clock) *Monitor {
	return &Monitor{DB: db, Clock: clock, client: &http.Client{Timeout: 4 * time.Second}}
}

// AppURL returns the URL of the application in env, or "" when it is not deployed.
func (m *Monitor) AppURL(env string) string {
	outputs, err := store.LayerOutputs(m.DB, env, m.App.Layer)
	if err != nil {
		return ""
	}
	url, _ := outputs[m.App.URLOutput].(string)
	return url
}

func (m *Monitor) getJSON(ctx context.Context, url string, into any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("build request for %s: %w", url, err)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("get %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("read %s: %w", url, err)
	}
	if into != nil && resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, into); err != nil {
			return resp.StatusCode, fmt.Errorf("decode %s: %w", url, err)
		}
	}
	return resp.StatusCode, nil
}

// Sample is the result of one round of probes.
type Sample struct {
	TS      string  `json:"ts"`
	Env     string  `json:"env"`
	OK      int     `json:"ok"`
	Total   int     `json:"total"`
	P95MS   float64 `json:"p95_ms"`
	Version string  `json:"version"`
}

// Probe sends count requests to the probe path of the application and reads the version.
func (m *Monitor) Probe(ctx context.Context, env string, count int) (Sample, error) {
	base := m.AppURL(env)
	if base == "" {
		return Sample{}, fmt.Errorf("probe %s: the application is not deployed", env)
	}
	sample := Sample{TS: store.FormatTime(m.Clock.Now()), Env: env, Total: count}
	var version struct {
		Version string `json:"version"`
	}
	if _, err := m.getJSON(ctx, base+m.App.Health.VersionPath, &version); err == nil {
		sample.Version = version.Version
	}
	durations := []float64{}
	for range count {
		start := time.Now()
		status, err := m.getJSON(ctx, base+m.App.Health.ProbePath, nil)
		durations = append(durations, float64(time.Since(start).Microseconds())/1000)
		if err == nil && status == http.StatusOK {
			sample.OK++
		}
	}
	sort.Float64s(durations)
	if len(durations) > 0 {
		sample.P95MS = durations[min(len(durations)-1, len(durations)*95/100)]
	}
	return sample, nil
}

// Record stores a sample in the health history.
func (m *Monitor) Record(s Sample) error {
	_, err := m.DB.Exec(`INSERT INTO health (ts, env, ok, total, p95_ms, version)
		VALUES ($1,$2,$3,$4,$5,$6)`, s.TS, s.Env, s.OK, s.Total, s.P95MS, s.Version)
	if err != nil {
		return fmt.Errorf("record health sample for %s: %w", s.Env, err)
	}
	return nil
}

// Run probes every environment on an interval until ctx ends.
func (m *Monitor) Run(ctx context.Context, envs []string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, env := range envs {
				sample, err := m.Probe(ctx, env, 8)
				if err != nil {
					continue
				}
				if err := m.Record(sample); err != nil {
					panic(fmt.Sprintf("health table is not writable, stopping: %v", err))
				}
			}
		}
	}
}

// History returns the health samples of env between from and to, oldest first.
func (m *Monitor) History(env, from, to string, limit int) ([]Sample, error) {
	if to == "" {
		to = "9999"
	}
	rows, err := m.DB.Query(`SELECT ts, env, ok, total, p95_ms, version FROM (
		SELECT * FROM health WHERE env = $1 AND ts >= $2 AND ts <= $3 ORDER BY ts DESC LIMIT $4)
		ORDER BY ts ASC`, env, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("read health history of %s: %w", env, err)
	}
	defer rows.Close()
	samples := []Sample{}
	for rows.Next() {
		var s Sample
		if err := rows.Scan(&s.TS, &s.Env, &s.OK, &s.Total, &s.P95MS, &s.Version); err != nil {
			return nil, fmt.Errorf("scan health sample: %w", err)
		}
		samples = append(samples, s)
	}
	return samples, rows.Err()
}

// Verify is the check after an apply. When the application is deployed it must answer the
// health check, report the version that the application layer declares, and answer the
// probe with an error rate under ten percent.
func (m *Monitor) Verify(ctx context.Context, wi orchestrator.WorkItem) (orchestrator.VerifyResult, error) {
	outputs, err := store.LayerOutputs(m.DB, wi.Env, m.App.Layer)
	if err != nil {
		return orchestrator.VerifyResult{}, err
	}
	base, _ := outputs[m.App.URLOutput].(string)
	want, _ := outputs[m.App.VersionVariable].(string)
	if base == "" {
		return orchestrator.VerifyResult{OK: true,
			Checks: []string{"the application layer is not applied; no application check"}}, nil
	}
	result := orchestrator.VerifyResult{}
	deadline := time.Now().Add(90 * time.Second)
	for {
		status, err := m.getJSON(ctx, base+m.App.Health.ReadyPath, nil)
		var version struct {
			Version string `json:"version"`
		}
		_, versionErr := m.getJSON(ctx, base+m.App.Health.VersionPath, &version)
		if err == nil && status == http.StatusOK && versionErr == nil && version.Version == want {
			result.Checks = append(result.Checks, "health check answers 200",
				"running version is "+want)
			break
		}
		if time.Now().After(deadline) {
			result.Reason = fmt.Sprintf("after 90 s the application does not report healthy "+
				"version %s (health status %d, version %q)", want, status, version.Version)
			return result, nil
		}
		time.Sleep(3 * time.Second)
	}
	sample, err := m.Probe(ctx, wi.Env, 40)
	if err != nil {
		return orchestrator.VerifyResult{}, err
	}
	if err := m.Record(sample); err != nil {
		return orchestrator.VerifyResult{}, err
	}
	errorRate := 100 * float64(sample.Total-sample.OK) / float64(sample.Total)
	check := fmt.Sprintf("canary: %d of %d %s requests succeeded (error rate %.0f%%)",
		sample.OK, sample.Total, m.App.Health.ProbeName, errorRate)
	result.Checks = append(result.Checks, check)
	if errorRate >= 10 {
		result.Reason = fmt.Sprintf("canary failed: error rate %.0f%% is over the 10%% limit "+
			"on version %s", errorRate, want)
		return result, nil
	}
	result.OK = true
	return result, nil
}

// Snapshot is the state of one environment at one time.
type Snapshot struct {
	ID     int64                     `json:"id"`
	TS     string                    `json:"ts"`
	Env    string                    `json:"env"`
	Reason string                    `json:"reason"`
	Layers map[string]map[string]any `json:"layers"`
	Health *Sample                   `json:"health,omitempty"`
}

// TakeSnapshot records the outputs of every layer and the current health.
func (m *Monitor) TakeSnapshot(ctx context.Context, env, reason string) (Snapshot, error) {
	snap := Snapshot{TS: store.FormatTime(m.Clock.Now()), Env: env, Reason: reason,
		Layers: map[string]map[string]any{}}
	for _, layer := range orchestrator.Layers {
		outputs, err := store.LayerOutputs(m.DB, env, layer)
		if err != nil {
			return Snapshot{}, err
		}
		if len(outputs) > 0 {
			snap.Layers[layer] = outputs
		}
	}
	if sample, err := m.Probe(ctx, env, 5); err == nil {
		snap.Health = &sample
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return Snapshot{}, fmt.Errorf("encode snapshot of %s: %w", env, err)
	}
	err = m.DB.QueryRow(`INSERT INTO snapshots (ts, env, reason, data) VALUES ($1,$2,$3,$4) RETURNING id`,
		snap.TS, env, reason, string(data)).Scan(&snap.ID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("store snapshot of %s: %w", env, err)
	}
	return snap, nil
}

// SnapshotAt returns the newest snapshot of env at or before asOf. An empty asOf means now.
// The second result is false when no snapshot exists.
func (m *Monitor) SnapshotAt(env, asOf string) (Snapshot, bool, error) {
	if asOf == "" {
		asOf = "9999"
	}
	var id int64
	var data string
	err := m.DB.QueryRow(`SELECT id, data FROM snapshots WHERE env = $1 AND ts <= $2
		ORDER BY ts DESC, id DESC LIMIT 1`, env, asOf).Scan(&id, &data)
	if err == sql.ErrNoRows {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("read snapshot of %s: %w", env, err)
	}
	var snap Snapshot
	if err := json.Unmarshal([]byte(data), &snap); err != nil {
		return Snapshot{}, false, fmt.Errorf("decode snapshot %d: %w", id, err)
	}
	snap.ID = id
	return snap, true, nil
}
