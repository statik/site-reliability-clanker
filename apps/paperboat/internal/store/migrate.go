package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
)

// migrationLock is the advisory lock that stops two migrate jobs from running together.
const migrationLock = 7271001

const schemaV1 = `
CREATE TABLE IF NOT EXISTS species (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	daily_price_cents INT NOT NULL CHECK (daily_price_cents >= 0)
);
CREATE TABLE IF NOT EXISTS tanks (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	location TEXT NOT NULL,
	capacity INT NOT NULL CHECK (capacity > 0),
	water_temp_c NUMERIC(4,1) NOT NULL
);
CREATE TABLE IF NOT EXISTS fish (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	species_id BIGINT NOT NULL REFERENCES species(id),
	tank_id BIGINT NOT NULL REFERENCES tanks(id),
	hatch_date DATE NOT NULL,
	status TEXT NOT NULL DEFAULT 'available' CHECK (status IN
		('available','reserved','rented','in_transit','quarantined','retired')),
	status_changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS customers (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	email TEXT NOT NULL UNIQUE,
	address TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS rentals (
	id BIGSERIAL PRIMARY KEY,
	customer_id BIGINT NOT NULL REFERENCES customers(id),
	fish_id BIGINT NOT NULL REFERENCES fish(id),
	starts_at TIMESTAMPTZ NOT NULL,
	ends_at TIMESTAMPTZ NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('reserved','active','returned','cancelled')),
	price_cents INT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS deliveries (
	id BIGSERIAL PRIMARY KEY,
	rental_id BIGINT NOT NULL REFERENCES rentals(id),
	kind TEXT NOT NULL CHECK (kind IN ('delivery','collection')),
	status TEXT NOT NULL CHECK (status IN ('scheduled','out','done','failed')),
	driver TEXT NOT NULL,
	window_start TIMESTAMPTZ NOT NULL,
	window_end TIMESTAMPTZ NOT NULL,
	manifest_path TEXT NOT NULL DEFAULT '',
	started_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS tickets (
	id BIGSERIAL PRIMARY KEY,
	customer_id BIGINT NOT NULL REFERENCES customers(id),
	rental_id BIGINT REFERENCES rentals(id),
	delivery_id BIGINT REFERENCES deliveries(id),
	subject TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('open','waiting','resolved')),
	priority TEXT NOT NULL CHECK (priority IN ('low','normal','high','urgent')),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	escalated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS interactions (
	id BIGSERIAL PRIMARY KEY,
	ticket_id BIGINT NOT NULL REFERENCES tickets(id),
	author TEXT NOT NULL,
	channel TEXT NOT NULL CHECK (channel IN ('phone','email','chat')),
	body TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS fish_status_idx ON fish (status);
CREATE INDEX IF NOT EXISTS rentals_status_idx ON rentals (status);
CREATE INDEX IF NOT EXISTS deliveries_status_idx ON deliveries (status);
CREATE INDEX IF NOT EXISTS interactions_ticket_idx ON interactions (ticket_id);

CREATE OR REPLACE FUNCTION notify_fish_status() RETURNS trigger AS $$
BEGIN
	IF NEW.status IS DISTINCT FROM OLD.status THEN
		NEW.status_changed_at := now();
		PERFORM pg_notify('fish_status', json_build_object(
			'fish_id', NEW.id, 'name', NEW.name, 'tank_id', NEW.tank_id,
			'from', OLD.status, 'status', NEW.status, 'at', now())::text);
	END IF;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS fish_status_notify ON fish;
CREATE TRIGGER fish_status_notify BEFORE UPDATE ON fish
	FOR EACH ROW EXECUTE FUNCTION notify_fish_status();
`

// schemaV2 is additive only. Release 1.0.0 keeps working on it during an upgrade or after
// a rollback.
const schemaV2 = `
ALTER TABLE species ADD COLUMN IF NOT EXISTS retire_after_days INT NOT NULL DEFAULT 730
	CHECK (retire_after_days > 0);
ALTER TABLE fish ADD COLUMN IF NOT EXISTS condition INT NOT NULL DEFAULT 100
	CHECK (condition BETWEEN 0 AND 100);
`

type migration struct {
	version int
	sql     string
	// after runs in the same transaction as sql.
	after func(ctx context.Context, tx pgx.Tx, now time.Time) error
}

func migrations() []migration {
	return []migration{
		{version: 1, sql: schemaV1, after: seedIfEmpty},
		{version: 2, sql: schemaV2, after: backfillAging},
	}
}

// MigrationResult says what one migrate run did.
type MigrationResult struct {
	Before  int
	After   int
	Applied []int
}

// Migrate applies each migration up to the feature level of this build that the database
// does not have. It is forward-only: a database with a newer schema is left as it is.
func (s *Store) Migrate(ctx context.Context, now time.Time) (MigrationResult, error) {
	var result MigrationResult
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return result, fmt.Errorf("acquire connection for migration: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLock); err != nil {
		return result, fmt.Errorf("take migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLock)
	}()
	_, err = conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`)
	if err != nil {
		return result, fmt.Errorf("create schema_migrations: %w", err)
	}
	if result.Before, err = s.SchemaVersion(ctx); err != nil {
		return result, err
	}
	result.After = result.Before
	for _, m := range pendingMigrations(result.Before, s.level) {
		if err := applyMigration(ctx, conn.Conn(), m, now); err != nil {
			return result, err
		}
		result.Applied = append(result.Applied, m.version)
		result.After = m.version
	}
	return result, nil
}

// pendingMigrations returns the migrations newer than the database and not newer than the
// feature level of this build.
func pendingMigrations(current, level int) []migration {
	var pending []migration
	for _, m := range migrations() {
		if m.version > current && m.version <= level {
			pending = append(pending, m)
		}
	}
	return pending
}

func applyMigration(ctx context.Context, conn *pgx.Conn, m migration, now time.Time) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migration %d: begin: %w", m.version, err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, m.sql); err != nil {
		return fmt.Errorf("migration %d: %w", m.version, err)
	}
	if m.after != nil {
		if err := m.after(ctx, tx, now); err != nil {
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, m.version)
	if err != nil {
		return fmt.Errorf("migration %d: record version: %w", m.version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("migration %d: commit: %w", m.version, err)
	}
	return nil
}

// backfillAging gives each seeded species its retirement age and each fish its condition.
func backfillAging(ctx context.Context, tx pgx.Tx, now time.Time) error {
	for _, sp := range seedSpecies {
		_, err := tx.Exec(ctx, `UPDATE species SET retire_after_days = $2 WHERE name = $1`,
			sp.name, sp.retireAfterDays)
		if err != nil {
			return fmt.Errorf("set retirement age of %s: %w", sp.name, err)
		}
	}
	conditions, err := fishConditions(ctx, tx, now)
	if err != nil {
		return err
	}
	for id, condition := range conditions {
		_, err := tx.Exec(ctx, `UPDATE fish SET condition = $2 WHERE id = $1`, id, condition)
		if err != nil {
			return fmt.Errorf("set condition of fish %d: %w", id, err)
		}
	}
	return nil
}

// fishConditions computes the condition of each fish from its age.
func fishConditions(ctx context.Context, tx pgx.Tx, now time.Time) (map[int64]int, error) {
	rows, err := tx.Query(ctx, `SELECT f.id, f.hatch_date, s.retire_after_days
		FROM fish f JOIN species s ON s.id = f.species_id`)
	if err != nil {
		return nil, fmt.Errorf("read fish for condition backfill: %w", err)
	}
	defer rows.Close()
	conditions := map[int64]int{}
	for rows.Next() {
		var id int64
		var hatch time.Time
		var retire int
		if err := rows.Scan(&id, &hatch, &retire); err != nil {
			return nil, fmt.Errorf("scan fish for condition backfill: %w", err)
		}
		conditions[id] = domain.Condition(domain.AgeDays(hatch, now), retire)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read fish for condition backfill: %w", err)
	}
	return conditions, nil
}
