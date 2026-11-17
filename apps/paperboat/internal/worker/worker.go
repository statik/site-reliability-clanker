// Package worker runs the background jobs: aging, delivery progress, support escalation
// and health certificates.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/files"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/store"
)

// Job intervals.
const (
	AgingEvery       = 30 * time.Second
	DeliveryEvery    = 10 * time.Second
	EscalationEvery  = 30 * time.Second
	CertificateEvery = 60 * time.Second
)

// Store is the database boundary that the jobs use. Each write is conditional on the state
// that the job read, so more than one worker replica can run.
type Store interface {
	AgingRows(ctx context.Context, now time.Time) ([]store.AgingRow, error)
	ApplyAging(ctx context.Context, id int64, from string, r domain.AgingResult) error
	PendingDeliveries(ctx context.Context) ([]store.Delivery, error)
	TransitionDelivery(ctx context.Context, id int64, to string, now time.Time,
	) (store.Delivery, error)
	SetManifestPath(ctx context.Context, id int64, path string) error
	OpenTickets(ctx context.Context) ([]store.Ticket, error)
	Escalate(ctx context.Context, id int64, from, to string) error
	Inventory(ctx context.Context, now time.Time) ([]store.Fish, error)
}

// Worker runs the jobs for one feature level.
type Worker struct {
	DB      Store
	Level   int
	DataDir string
	Logger  *slog.Logger
	Now     func() time.Time
}

type job struct {
	name  string
	every time.Duration
	run   func(ctx context.Context) (int, error)
}

// Run runs each job now and then at its interval, until ctx ends. A job that fails is
// logged and runs again at the next interval.
func (w *Worker) Run(ctx context.Context) {
	jobs := []job{
		{"aging", AgingEvery, w.Aging},
		{"delivery_progress", DeliveryEvery, w.DeliveryProgress},
		{"support_escalation", EscalationEvery, w.Escalation},
		{"health_certificates", CertificateEvery, w.Certificates},
	}
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Go(func() { w.loop(ctx, j) })
	}
	wg.Wait()
}

func (w *Worker) loop(ctx context.Context, j job) {
	ticker := time.NewTicker(j.every)
	defer ticker.Stop()
	for {
		start := time.Now()
		changed, err := j.run(ctx)
		duration := float64(time.Since(start).Microseconds()) / 1000
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			w.Logger.Error("job failed", "job", j.name, "duration_ms", duration,
				"changed", changed, "error", err.Error())
		default:
			w.Logger.Info("job done", "job", j.name, "duration_ms", duration, "changed", changed)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Aging applies the aging rules to each fish and returns the number of fish it changed.
func (w *Worker) Aging(ctx context.Context) (int, error) {
	now := w.Now()
	rows, err := w.DB.AgingRows(ctx, now)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, row := range rows {
		result := domain.AgeFish(w.Level, row.Input, now)
		if !result.Changed {
			continue
		}
		if err := w.DB.ApplyAging(ctx, row.ID, row.Input.Status, result); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// DeliveryProgress moves each job that is due to its next status and writes the manifest
// when a van leaves. It returns the number of jobs it moved.
func (w *Worker) DeliveryProgress(ctx context.Context) (int, error) {
	now := w.Now()
	pending, err := w.DB.PendingDeliveries(ctx)
	if err != nil {
		return 0, err
	}
	moved := 0
	var failures []error
	for _, d := range pending {
		didMove, err := w.advance(ctx, d, now)
		if didMove {
			moved++
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return moved, errors.Join(failures...)
}

// advance moves one job when it is due. A job that another process moved first is skipped.
func (w *Worker) advance(ctx context.Context, d store.Delivery, now time.Time) (bool, error) {
	var startedAt time.Time
	if d.StartedAt != nil {
		startedAt = *d.StartedAt
	}
	next, due := domain.NextDeliveryStatus(d.Status, d.WindowStart, startedAt, now)
	if !due {
		return false, nil
	}
	updated, err := w.DB.TransitionDelivery(ctx, d.ID, next, now)
	if errors.Is(err, domain.ErrConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if updated.Status != domain.DeliveryOut {
		return true, nil
	}
	return true, files.IssueManifest(ctx, w.DataDir, w.DB, updated, now)
}

// Escalation raises the priority of tickets that stayed open too long and returns the
// number of tickets it raised.
func (w *Worker) Escalation(ctx context.Context) (int, error) {
	now := w.Now()
	tickets, err := w.DB.OpenTickets(ctx)
	if err != nil {
		return 0, err
	}
	raised := 0
	for _, t := range tickets {
		next, due := domain.EscalatedPriority(t.Status, t.Priority, t.EscalatedAt, now)
		if !due {
			continue
		}
		if err := w.DB.Escalate(ctx, t.ID, t.Priority, next); err != nil {
			return raised, err
		}
		raised++
	}
	return raised, nil
}

// Certificates writes a health certificate for each fish and returns the number written.
func (w *Worker) Certificates(ctx context.Context) (int, error) {
	now := w.Now()
	fish, err := w.DB.Inventory(ctx, now)
	if err != nil {
		return 0, err
	}
	written := 0
	for _, f := range fish {
		content := domain.Certificate(f.Name, f.Species, f.Status, f.AgeDays, f.Condition, now)
		if err := files.WriteCertificate(w.DataDir, f.ID, content); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}
