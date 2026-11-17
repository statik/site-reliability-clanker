package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
)

// Fish is one fish in the inventory. Condition and RetireAfterDays are nil before 1.1.0.
type Fish struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	SpeciesID       int64  `json:"species_id"`
	Species         string `json:"species"`
	TankID          int64  `json:"tank_id"`
	Tank            string `json:"tank"`
	Status          string `json:"status"`
	HatchDate       string `json:"hatch_date"`
	AgeDays         int    `json:"age_days"`
	Condition       *int   `json:"condition,omitempty"`
	RetireAfterDays *int   `json:"retire_after_days,omitempty"`
}

// Tank is one tank with the number of fish that are in it now.
type Tank struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Location   string  `json:"location"`
	Capacity   int     `json:"capacity"`
	WaterTempC float64 `json:"water_temp_c"`
	Occupancy  int     `json:"occupancy"`
}

// Customer is one customer.
type Customer struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Address string `json:"address"`
}

// agingColumns returns the select list for the columns that exist from schema version 2.
func (s *Store) agingColumns() string {
	if s.level >= domain.LevelAging {
		return "f.condition, s.retire_after_days"
	}
	return "NULL::int, NULL::int"
}

// Inventory returns every fish with its species, tank and age.
func (s *Store) Inventory(ctx context.Context, now time.Time) ([]Fish, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.name, s.id, s.name, t.id, t.name, f.status, f.hatch_date, `+
		s.agingColumns()+`
		FROM fish f
		JOIN species s ON s.id = f.species_id
		JOIN tanks t ON t.id = f.tank_id
		ORDER BY t.id, f.name`)
	if err != nil {
		return nil, fmt.Errorf("list inventory: %w", err)
	}
	fish, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Fish, error) {
		var f Fish
		var hatch time.Time
		err := row.Scan(&f.ID, &f.Name, &f.SpeciesID, &f.Species, &f.TankID, &f.Tank,
			&f.Status, &hatch, &f.Condition, &f.RetireAfterDays)
		f.HatchDate = hatch.Format(time.DateOnly)
		f.AgeDays = domain.AgeDays(hatch, now)
		return f, err
	})
	if err != nil {
		return nil, fmt.Errorf("list inventory: %w", err)
	}
	return fish, nil
}

// Tanks returns every tank. Occupancy counts the fish that are in the building.
func (s *Store) Tanks(ctx context.Context) ([]Tank, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.name, t.location, t.capacity, t.water_temp_c::float8,
		       COUNT(f.id) FILTER (WHERE f.status IN ('available','reserved','quarantined'))
		FROM tanks t LEFT JOIN fish f ON f.tank_id = t.id
		GROUP BY t.id ORDER BY t.id`)
	if err != nil {
		return nil, fmt.Errorf("list tanks: %w", err)
	}
	tanks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Tank, error) {
		var t Tank
		err := row.Scan(&t.ID, &t.Name, &t.Location, &t.Capacity, &t.WaterTempC, &t.Occupancy)
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("list tanks: %w", err)
	}
	return tanks, nil
}

// Customers returns every customer.
func (s *Store) Customers(ctx context.Context) ([]Customer, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, email, address FROM customers ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list customers: %w", err)
	}
	customers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Customer, error) {
		var c Customer
		err := row.Scan(&c.ID, &c.Name, &c.Email, &c.Address)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("list customers: %w", err)
	}
	return customers, nil
}

// AgingRow is one fish as the aging job reads it.
type AgingRow struct {
	ID    int64
	Hatch time.Time
	Input domain.AgingInput
}

// AgingRows returns the fish that are not retired. Before 1.1.0 the condition and the
// retirement age are zero and the aging rules do not read them.
func (s *Store) AgingRows(ctx context.Context, now time.Time) ([]AgingRow, error) {
	columns := "0, 0"
	if s.level >= domain.LevelAging {
		columns = "f.condition, s.retire_after_days"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.hatch_date, f.status, f.status_changed_at, `+columns+`
		FROM fish f JOIN species s ON s.id = f.species_id
		WHERE f.status <> 'retired' ORDER BY f.id`)
	if err != nil {
		return nil, fmt.Errorf("list fish for aging: %w", err)
	}
	result, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AgingRow, error) {
		var r AgingRow
		err := row.Scan(&r.ID, &r.Hatch, &r.Input.Status, &r.Input.StatusChangedAt,
			&r.Input.Condition, &r.Input.RetireAfterDays)
		r.Input.AgeDays = domain.AgeDays(r.Hatch, now)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("list fish for aging: %w", err)
	}
	return result, nil
}

// ApplyAging writes one aging result. The update does nothing when another process changed
// the status of the fish after it was read.
func (s *Store) ApplyAging(ctx context.Context, id int64, from string, r domain.AgingResult) error {
	var err error
	if s.level >= domain.LevelAging {
		_, err = s.pool.Exec(ctx, `UPDATE fish SET status = $2, condition = $3
			WHERE id = $1 AND status = $4`, id, r.Status, r.Condition, from)
	} else {
		_, err = s.pool.Exec(ctx, `UPDATE fish SET status = $2 WHERE id = $1 AND status = $3`,
			id, r.Status, from)
	}
	if err != nil {
		return fmt.Errorf("apply aging to fish %d: %w", id, err)
	}
	return nil
}
