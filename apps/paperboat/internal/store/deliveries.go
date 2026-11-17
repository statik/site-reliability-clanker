package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
)

// Delivery is one delivery or collection job with the names the UI shows.
type Delivery struct {
	ID           int64      `json:"id"`
	RentalID     int64      `json:"rental_id"`
	Kind         string     `json:"kind"`
	Status       string     `json:"status"`
	Driver       string     `json:"driver"`
	WindowStart  time.Time  `json:"window_start"`
	WindowEnd    time.Time  `json:"window_end"`
	ManifestPath string     `json:"manifest_path"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	Customer     string     `json:"customer"`
	Address      string     `json:"address"`
	FishID       int64      `json:"fish_id"`
	Fish         string     `json:"fish"`
	Species      string     `json:"species"`
	Tank         string     `json:"tank"`
}

const deliverySelect = `
	SELECT d.id, d.rental_id, d.kind, d.status, d.driver, d.window_start, d.window_end,
	       d.manifest_path, d.started_at, c.name, c.address, f.id, f.name, s.name, t.name
	FROM deliveries d
	JOIN rentals r ON r.id = d.rental_id
	JOIN customers c ON c.id = r.customer_id
	JOIN fish f ON f.id = r.fish_id
	JOIN species s ON s.id = f.species_id
	JOIN tanks t ON t.id = f.tank_id`

func scanDelivery(row pgx.CollectableRow) (Delivery, error) {
	var d Delivery
	err := row.Scan(&d.ID, &d.RentalID, &d.Kind, &d.Status, &d.Driver, &d.WindowStart,
		&d.WindowEnd, &d.ManifestPath, &d.StartedAt, &d.Customer, &d.Address, &d.FishID,
		&d.Fish, &d.Species, &d.Tank)
	return d, err
}

func (s *Store) deliveries(ctx context.Context, where string) ([]Delivery, error) {
	rows, err := s.pool.Query(ctx, deliverySelect+where)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	deliveries, err := pgx.CollectRows(rows, scanDelivery)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	return deliveries, nil
}

// Deliveries returns the newest delivery and collection jobs.
func (s *Store) Deliveries(ctx context.Context) ([]Delivery, error) {
	return s.deliveries(ctx, ` ORDER BY d.id DESC LIMIT 200`)
}

// PendingDeliveries returns the jobs that are scheduled or out.
func (s *Store) PendingDeliveries(ctx context.Context) ([]Delivery, error) {
	return s.deliveries(ctx, ` WHERE d.status IN ('scheduled','out') ORDER BY d.id`)
}

// Delivery returns one job.
func (s *Store) Delivery(ctx context.Context, id int64) (Delivery, error) {
	rows, err := s.pool.Query(ctx, deliverySelect+` WHERE d.id = $1`, id)
	if err != nil {
		return Delivery{}, fmt.Errorf("read delivery %d: %w", id, err)
	}
	delivery, err := pgx.CollectExactlyOneRow(rows, scanDelivery)
	if err != nil {
		return Delivery{}, notFound(err, "delivery", id)
	}
	return delivery, nil
}

// TransitionDelivery moves a job to a new status and applies the effect on the fish and the
// rental in one transaction. The row lock makes it safe when the worker and a person, or
// two workers, move the same job: the second one gets domain.ErrConflict.
func (s *Store) TransitionDelivery(ctx context.Context, id int64, to string, now time.Time,
) (Delivery, error) {
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var kind, from string
		var rentalID, fishID int64
		err := tx.QueryRow(ctx, `SELECT d.kind, d.status, d.rental_id, r.fish_id
			FROM deliveries d JOIN rentals r ON r.id = d.rental_id
			WHERE d.id = $1 FOR UPDATE OF d`, id).Scan(&kind, &from, &rentalID, &fishID)
		if err != nil {
			return notFound(err, "delivery", id)
		}
		if err := domain.ValidateDeliveryTransition(from, to); err != nil {
			return err
		}
		effect, err := domain.EffectOf(kind, to)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE deliveries SET status = $2,
			started_at = CASE WHEN $2 = 'out' THEN $3 ELSE started_at END
			WHERE id = $1`, id, to, now)
		if err != nil {
			return fmt.Errorf("update delivery %d: %w", id, err)
		}
		return applyDeliveryEffect(ctx, tx, fishID, rentalID, effect)
	})
	if err != nil {
		return Delivery{}, err
	}
	return s.Delivery(ctx, id)
}

func applyDeliveryEffect(ctx context.Context, tx pgx.Tx, fishID, rentalID int64,
	effect domain.DeliveryEffect,
) error {
	_, err := tx.Exec(ctx, `UPDATE fish SET status = $2 WHERE id = $1`, fishID, effect.Fish)
	if err != nil {
		return fmt.Errorf("update fish %d: %w", fishID, err)
	}
	if effect.Rental == "" {
		return nil
	}
	_, err = tx.Exec(ctx, `UPDATE rentals SET status = $2 WHERE id = $1`, rentalID, effect.Rental)
	if err != nil {
		return fmt.Errorf("update rental %d: %w", rentalID, err)
	}
	return nil
}

// SetManifestPath records where the manifest of a job is, relative to the data directory.
func (s *Store) SetManifestPath(ctx context.Context, id int64, path string) error {
	_, err := s.pool.Exec(ctx, `UPDATE deliveries SET manifest_path = $2 WHERE id = $1`, id, path)
	if err != nil {
		return fmt.Errorf("set manifest path of delivery %d: %w", id, err)
	}
	return nil
}
