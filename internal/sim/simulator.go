package sim

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/detect"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/notify"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// Simulator makes the lifecycle demonstrable in minutes: it moves the virtual clock,
// publishes releases, adds advisories and injects faults. It only produces inputs. Every
// reaction comes from the same detectors, agent and pipeline as outside demo mode.
type Simulator struct {
	DB       *sql.DB
	Clock    *Clock
	Audit    *audit.Log
	Forge    *forge.Forge
	Detector *detect.Detector
	Orch     *orchestrator.Orchestrator
	Monitor  *monitor.Monitor
	Notifier *notify.Notifier
	// App is the application pack. The scenes publish its releases.
	App pack.Pack
	// AdminToken is the fault-injection token of the application.
	AdminToken string
	// ManualChange changes the environment outside the pipeline, as a person would. The
	// target gives it. It is nil on a target that has none.
	ManualChange func(ctx context.Context, env string) error
	Env          string
}

// Scene is one step of the scripted month.
type Scene struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Detail  string `json:"detail"`
	UseCase string `json:"use_case"`
	Done    bool   `json:"done"`
	run     func(s *Simulator, ctx context.Context) error
}

func scenes(app pack.Pack) []Scene {
	return []Scene{
		{Name: "quiet-week", Title: "A quiet week", UseCase: "3, 13",
			Detail: "Seven days pass. The scheduled backup runs.", run: (*Simulator).quietWeek},
		{Name: "feature-release", Title: app.Title + " 1.1.0 is published", UseCase: "4",
			Detail: "A feature release with a schema migration.", run: (*Simulator).featureRelease},
		{Name: "cve", Title: "A base image CVE is announced", UseCase: "5",
			Detail: "Advisory PB-2026-014. The fix is release 1.1.1.", run: (*Simulator).cve},
		{Name: "bad-release", Title: app.Title + " 1.2.0 is published", UseCase: "6",
			Detail: "A faulty release. The canary check must catch it.", run: (*Simulator).badRelease},
		{Name: "kubernetes-eol", Title: "Kubernetes end-of-support forecast", UseCase: "7",
			Detail: "A forecast on the timeline. The local target cannot upgrade in place.",
			run:    (*Simulator).kubernetesEOL},
		{Name: "database-patch", Title: "Postgres 17.12 is pending", UseCase: "9",
			Detail: "A minor version patch of the database.", run: (*Simulator).databasePatch},
		{Name: "database-upkeep", Title: "Database maintenance is due", UseCase: "11",
			Detail: "Vacuum and analyze under the standing approval.", run: (*Simulator).upkeep},
		{Name: "restore-drill", Title: "Monthly restore drill", UseCase: "14",
			Detail: "Restore the newest backup into a scratch database.", run: (*Simulator).restoreDrill},
		{Name: "drift", Title: "Someone changes production by hand", UseCase: "16",
			Detail: "A manual kubectl change. The nightly drift check finds it.", run: (*Simulator).drift},
		{Name: "incident", Title: "Incident at 02:00", UseCase: "17, 18",
			Detail: "Inventory requests start to fail with no change in progress.",
			run:    (*Simulator).incident},
		{Name: "storage-growth", Title: "The database volume fills up", UseCase: "12",
			Detail: "Eight days of steady growth. The forecast proposes a larger volume.",
			run:    (*Simulator).storageGrowth},
	}
}

func sceneKey(name string) string { return "sim.scene_done." + name }

// State describes the simulator for the dashboard.
type State struct {
	Now           string  `json:"now"`
	OffsetSeconds int64   `json:"offset_seconds"`
	Scenes        []Scene `json:"scenes"`
	Next          string  `json:"next"`
}

// State returns the virtual time and the progress through the scripted month.
func (s *Simulator) State() (State, error) {
	state := State{Now: store.FormatTime(s.Clock.Now()),
		OffsetSeconds: int64(s.Clock.Offset() / time.Second), Scenes: scenes(s.App)}
	for i := range state.Scenes {
		done, err := store.GetKV(s.DB, sceneKey(state.Scenes[i].Name))
		if err != nil {
			return State{}, err
		}
		state.Scenes[i].Done = done != ""
		if done == "" && state.Next == "" {
			state.Next = state.Scenes[i].Name
		}
	}
	return state, nil
}

func (s *Simulator) emit(eventType, summary string, data map[string]any) {
	if _, err := s.Audit.Append(audit.Event{Type: eventType, Actor: "simulator", Env: s.Env,
		Summary: summary, Data: data}); err != nil {
		panic(fmt.Sprintf("audit log is not writable, stopping: %v", err))
	}
}

// RunScene runs one scene of the scripted month.
func (s *Simulator) RunScene(ctx context.Context, name string) error {
	for _, scene := range scenes(s.App) {
		if scene.Name != name {
			continue
		}
		s.emit("sim.scene_started", "Scene: "+scene.Title, map[string]any{"scene": name,
			"use_case": scene.UseCase})
		if err := scene.run(s, ctx); err != nil {
			return fmt.Errorf("scene %s: %w", name, err)
		}
		return store.SetKV(s.DB, sceneKey(name), store.FormatTime(s.Clock.Now()))
	}
	return fmt.Errorf("scene %q does not exist", name)
}

// Advance moves the virtual clock, sends the digests of the passed days and takes a snapshot.
func (s *Simulator) Advance(ctx context.Context, d time.Duration) error {
	if err := s.Clock.Advance(d); err != nil {
		return err
	}
	s.emit("sim.clock_advanced", fmt.Sprintf("Virtual clock moved forward by %s", d),
		map[string]any{"seconds": int64(d / time.Second)})
	if s.Notifier != nil {
		s.Notifier.FlushDigest(ctx)
	}
	_, err := s.Monitor.TakeSnapshot(ctx, s.Env, "clock advanced")
	return err
}

// commitRelease puts the release note of a version into the repository of the application.
func (s *Simulator) commitRelease(version string) error {
	path := s.App.ReleaseNote(version)
	note, err := s.App.ReadReleaseNote(version)
	if err != nil {
		return fmt.Errorf("read release note %s: %w", path, err)
	}
	_, err = s.Forge.Commit(forge.CommitRequest{Repo: s.App.Releases.Repository,
		Branch: "main", Base: "main", Author: s.App.Name + "-release-bot",
		Message: "Release " + version,
		Files:   map[string]string{s.App.ReleaseNote(version): string(note)}})
	return err
}

func (s *Simulator) publishRelease(ctx context.Context, version string) error {
	if err := s.commitRelease(version); err != nil {
		return err
	}
	s.emit("sim.release_published", s.App.Title+" "+version+" is published",
		map[string]any{"version": version})
	s.Detector.RunChecks(ctx, s.Env)
	return nil
}

func (s *Simulator) addAdvisory(ctx context.Context, adv detect.Advisory) error {
	advisories, err := s.Detector.Advisories()
	if err != nil {
		return err
	}
	adv.Published = store.FormatTime(s.Clock.Now())
	raw, err := json.Marshal(append(advisories, adv))
	if err != nil {
		return fmt.Errorf("encode advisory feed: %w", err)
	}
	if err := store.SetKV(s.DB, detect.AdvisoriesKey, string(raw)); err != nil {
		return err
	}
	s.emit("sim.advisory_published", "Advisory "+adv.ID+": "+adv.Title,
		map[string]any{"advisory": adv.ID, "kind": adv.Kind, "severity": adv.Severity,
			"due": adv.Due})
	s.Detector.RunChecks(ctx, s.Env)
	return nil
}

func (s *Simulator) runbook(ctx context.Context, name, title, trigger string) error {
	_, err := s.Orch.RunRunbook(ctx, orchestrator.RunbookRequest{Env: s.Env, Name: name,
		Actor: "scheduler", Trigger: trigger, Title: title})
	return err
}

func (s *Simulator) quietWeek(ctx context.Context) error {
	for range 7 {
		if err := s.Advance(ctx, 24*time.Hour); err != nil {
			return err
		}
	}
	return s.runbook(ctx, "db-backup", "Scheduled database backup", "Weekly backup schedule")
}

func (s *Simulator) featureRelease(ctx context.Context) error {
	if err := s.Advance(ctx, 26*time.Hour); err != nil {
		return err
	}
	return s.publishRelease(ctx, "1.1.0")
}

func (s *Simulator) cve(ctx context.Context) error {
	if err := s.Advance(ctx, 5*24*time.Hour); err != nil {
		return err
	}
	if err := s.commitRelease("1.1.1"); err != nil {
		return err
	}
	return s.addAdvisory(ctx, detect.Advisory{ID: "PB-2026-014", Kind: "base-image-cve",
		Severity: "high", Layer: s.App.Layer, Variable: s.App.VersionVariable, Value: "1.1.1",
		Title: "Advisory PB-2026-014: vulnerability in the base image of " + s.App.Title +
			" 1.1.0; " +
			"fixed in 1.1.1"})
}

func (s *Simulator) badRelease(ctx context.Context) error {
	if err := s.Advance(ctx, 6*24*time.Hour); err != nil {
		return err
	}
	return s.publishRelease(ctx, "1.2.0-bad")
}

func (s *Simulator) kubernetesEOL(ctx context.Context) error {
	if err := s.Advance(ctx, 2*24*time.Hour); err != nil {
		return err
	}
	due := store.FormatTime(s.Clock.Now().Add(45 * 24 * time.Hour))
	return s.addAdvisory(ctx, detect.Advisory{ID: "K8S-1.36-EOL", Kind: "kubernetes-eol",
		Severity: "medium", Due: due,
		Title: "Kubernetes 1.36 reaches end of support; plan the upgrade to 1.37"})
}

func (s *Simulator) databasePatch(ctx context.Context) error {
	if err := s.Advance(ctx, 3*24*time.Hour); err != nil {
		return err
	}
	return s.addAdvisory(ctx, detect.Advisory{ID: "PG-17.12", Kind: "postgres-minor",
		Severity: "medium", Layer: "30-data", Variable: "postgres_version", Value: "17.12",
		Title: "Advisory PG-17.12: Postgres minor release 17.12 is pending; this environment " +
			"runs 17.11"})
}

func (s *Simulator) upkeep(ctx context.Context) error {
	if err := s.Advance(ctx, 2*24*time.Hour); err != nil {
		return err
	}
	return s.runbook(ctx, "db-maintenance", "Database maintenance: vacuum and analyze",
		"Detector: dead rows are over the threshold")
}

func (s *Simulator) restoreDrill(ctx context.Context) error {
	if err := s.Advance(ctx, 24*time.Hour); err != nil {
		return err
	}
	if err := s.runbook(ctx, "db-backup", "Scheduled database backup",
		"Weekly backup schedule"); err != nil {
		return err
	}
	return s.runbook(ctx, "restore-drill", "Monthly restore drill",
		"Monthly restore drill schedule")
}

func (s *Simulator) drift(ctx context.Context) error {
	if err := s.Advance(ctx, 24*time.Hour); err != nil {
		return err
	}
	if s.ManualChange == nil {
		return fmt.Errorf("the target of %s has no way to simulate a manual change", s.Env)
	}
	if err := s.ManualChange(ctx, s.Env); err != nil {
		return fmt.Errorf("simulate a manual change: %w", err)
	}
	s.emit("sim.manual_change", "Someone set the backup retention to 1 day with kubectl",
		map[string]any{"resource": "configmap/backup-policy",
			"namespace": s.App.DataNamespace})
	_, err := s.Detector.CheckDrift(ctx, s.Env)
	return err
}

func (s *Simulator) incident(ctx context.Context) error {
	now := s.Clock.Now()
	next2am := time.Date(now.Year(), now.Month(), now.Day()+1, 2, 0, 0, 0, time.UTC)
	if err := s.Advance(ctx, next2am.Sub(now)); err != nil {
		return err
	}
	base := s.Monitor.AppURL(s.Env)
	if base == "" {
		return fmt.Errorf("the application is not deployed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/admin/fault",
		bytes.NewBufferString(`{"mode":"errors"}`))
	if err != nil {
		return fmt.Errorf("build fault request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.AdminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("inject fault: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("inject fault: the application answered %d; check the admin token",
			resp.StatusCode)
	}
	s.emit("sim.fault_injected", "Fault injected: inventory requests fail", nil)
	for range 3 {
		sample, err := s.Monitor.Probe(ctx, s.Env, 20)
		if err != nil {
			return err
		}
		if err := s.Monitor.Record(sample); err != nil {
			return err
		}
	}
	return s.Detector.CheckHealth(ctx, s.Env)
}
