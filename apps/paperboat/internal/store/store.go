// Package store is the database boundary. It holds SQL only; rules live in package domain.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
)

// FishStatusChannel is the Postgres notification channel for fish status changes.
const FishStatusChannel = "fish_status"

const foreignKeyViolation = "23503"

// Store runs queries for one feature level. Level 1 never names a column that the level 2
// migration adds, so an old release works before and after a newer release migrates.
type Store struct {
	pool  *pgxpool.Pool
	level int
}

// Open connects to Postgres and waits up to wait for the database to answer.
func Open(ctx context.Context, dsn string, level int, wait time.Duration) (*Store, error) {
	if dsn == "" {
		return nil, errors.New("DATABASE_URL is not set; set it to a postgres:// URL")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	deadline := time.Now().Add(wait)
	for {
		err = pool.Ping(ctx)
		if err == nil {
			return &Store{pool: pool, level: level}, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			pool.Close()
			return nil, fmt.Errorf("database did not answer in %s: %w", wait, err)
		}
		time.Sleep(time.Second)
	}
}

// Close releases the connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Ping reports whether the database answers.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// SchemaVersion returns the newest migration that the database has.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).
		Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

// Counts are the business numbers in /api/stats.
type Counts struct {
	FishAvailable int
	RentalsActive int
	DeliveriesOut int
	TicketsOpen   int
}

// Counts returns the business numbers in /api/stats.
func (s *Store) Counts(ctx context.Context) (Counts, error) {
	var c Counts
	err := s.pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM fish WHERE status = 'available'),
		       (SELECT COUNT(*) FROM rentals WHERE status = 'active'),
		       (SELECT COUNT(*) FROM deliveries WHERE status = 'out'),
		       (SELECT COUNT(*) FROM tickets WHERE status = 'open')`).
		Scan(&c.FishAvailable, &c.RentalsActive, &c.DeliveriesOut, &c.TicketsOpen)
	if err != nil {
		return Counts{}, fmt.Errorf("count stats: %w", err)
	}
	return c, nil
}

// Listen calls handle for each notification on channel until ctx ends or the connection
// fails. It uses its own connection, outside the pool.
func Listen(ctx context.Context, dsn, channel string, handle func(payload string)) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect for LISTEN %s: %w", channel, err)
	}
	defer conn.Close(context.WithoutCancel(ctx))
	if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
		return fmt.Errorf("LISTEN %s: %w", channel, err)
	}
	for {
		note, err := conn.WaitForNotification(ctx)
		if err != nil {
			return fmt.Errorf("wait for %s notification: %w", channel, err)
		}
		handle(note.Payload)
	}
}

// inTx runs fn in a transaction and commits when fn returns nil.
func (s *Store) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// notFound turns "no rows" into a domain.ErrNotFound that names the missing thing.
func notFound(err error, what string, id int64) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s %d", domain.ErrNotFound, what, id)
	}
	return fmt.Errorf("read %s %d: %w", what, id, err)
}

// invalidReference turns a foreign key violation into a domain.ErrInvalid.
func invalidReference(err error, what string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation {
		return fmt.Errorf("%w: %s refers to a record that does not exist", domain.ErrInvalid, what)
	}
	return fmt.Errorf("write %s: %w", what, err)
}
