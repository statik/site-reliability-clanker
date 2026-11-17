package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
)

// Rental is one rental with the names the UI shows.
type Rental struct {
	ID         int64     `json:"id"`
	CustomerID int64     `json:"customer_id"`
	Customer   string    `json:"customer"`
	FishID     int64     `json:"fish_id"`
	Fish       string    `json:"fish"`
	Species    string    `json:"species"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	Status     string    `json:"status"`
	PriceCents int       `json:"price_cents"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewRental is the request for a new rental.
type NewRental struct {
	CustomerID int64 `json:"customer_id"`
	FishID     int64 `json:"fish_id"`
	Days       int   `json:"days"`
}

const rentalSelect = `
	SELECT r.id, r.customer_id, c.name, r.fish_id, f.name, s.name, r.starts_at, r.ends_at,
	       r.status, r.price_cents, r.created_at
	FROM rentals r
	JOIN customers c ON c.id = r.customer_id
	JOIN fish f ON f.id = r.fish_id
	JOIN species s ON s.id = f.species_id`

func scanRental(row pgx.CollectableRow) (Rental, error) {
	var r Rental
	err := row.Scan(&r.ID, &r.CustomerID, &r.Customer, &r.FishID, &r.Fish, &r.Species,
		&r.StartsAt, &r.EndsAt, &r.Status, &r.PriceCents, &r.CreatedAt)
	return r, err
}

// Rentals returns the newest rentals.
func (s *Store) Rentals(ctx context.Context) ([]Rental, error) {
	rows, err := s.pool.Query(ctx, rentalSelect+` ORDER BY r.id DESC LIMIT 200`)
	if err != nil {
		return nil, fmt.Errorf("list rentals: %w", err)
	}
	rentals, err := pgx.CollectRows(rows, scanRental)
	if err != nil {
		return nil, fmt.Errorf("list rentals: %w", err)
	}
	return rentals, nil
}

func rentalByID(ctx context.Context, tx pgx.Tx, id int64) (Rental, error) {
	rows, err := tx.Query(ctx, rentalSelect+` WHERE r.id = $1`, id)
	if err != nil {
		return Rental{}, fmt.Errorf("read rental %d: %w", id, err)
	}
	rental, err := pgx.CollectExactlyOneRow(rows, scanRental)
	if err != nil {
		return Rental{}, notFound(err, "rental", id)
	}
	return rental, nil
}

// CreateRental reserves the fish, creates the rental and schedules the delivery in one
// transaction. The row lock on the fish makes two requests for one fish safe.
func (s *Store) CreateRental(ctx context.Context, in NewRental, now time.Time) (Rental, error) {
	var rental Rental
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var fishStatus string
		var dailyPrice int
		err := tx.QueryRow(ctx, `SELECT f.status, s.daily_price_cents
			FROM fish f JOIN species s ON s.id = f.species_id
			WHERE f.id = $1 FOR UPDATE OF f`, in.FishID).Scan(&fishStatus, &dailyPrice)
		if err != nil {
			return notFound(err, "fish", in.FishID)
		}
		price, err := domain.RentalPrice(dailyPrice, in.Days)
		if err != nil {
			return err
		}
		if err := domain.CanReserve(fishStatus); err != nil {
			return err
		}
		var customerID int64
		err = tx.QueryRow(ctx, `SELECT id FROM customers WHERE id = $1`, in.CustomerID).
			Scan(&customerID)
		if err != nil {
			return notFound(err, "customer", in.CustomerID)
		}
		_, err = tx.Exec(ctx, `UPDATE fish SET status = $2 WHERE id = $1`,
			in.FishID, domain.FishReserved)
		if err != nil {
			return fmt.Errorf("reserve fish %d: %w", in.FishID, err)
		}
		var id int64
		err = tx.QueryRow(ctx, `INSERT INTO rentals
			(customer_id, fish_id, starts_at, ends_at, status, price_cents)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			in.CustomerID, in.FishID, now, now.AddDate(0, 0, in.Days),
			domain.RentalReserved, price).Scan(&id)
		if err != nil {
			return fmt.Errorf("insert rental: %w", err)
		}
		if err := scheduleDelivery(ctx, tx, id, domain.KindDelivery, now); err != nil {
			return err
		}
		rental, err = rentalByID(ctx, tx, id)
		return err
	})
	return rental, err
}

// ReturnRental schedules the collection of an active rental. The rental stays active until
// the collection is done.
func (s *Store) ReturnRental(ctx context.Context, id int64, now time.Time) (Rental, error) {
	var rental Rental
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM rentals WHERE id = $1 FOR UPDATE`, id).
			Scan(&status)
		if err != nil {
			return notFound(err, "rental", id)
		}
		var pending bool
		err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM deliveries
			WHERE rental_id = $1 AND kind = $2 AND status IN ('scheduled','out'))`,
			id, domain.KindCollection).Scan(&pending)
		if err != nil {
			return fmt.Errorf("check collections of rental %d: %w", id, err)
		}
		if err := domain.CanReturn(status, pending); err != nil {
			return err
		}
		if err := scheduleDelivery(ctx, tx, id, domain.KindCollection, now); err != nil {
			return err
		}
		rental, err = rentalByID(ctx, tx, id)
		return err
	})
	return rental, err
}

func scheduleDelivery(ctx context.Context, tx pgx.Tx, rentalID int64, kind string,
	now time.Time,
) error {
	start := now.Add(domain.DispatchDelay)
	_, err := tx.Exec(ctx, `INSERT INTO deliveries
		(rental_id, kind, status, driver, window_start, window_end)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		rentalID, kind, domain.DeliveryScheduled, domain.DriverFor(rentalID),
		start, start.Add(domain.WindowLength))
	if err != nil {
		return fmt.Errorf("schedule %s for rental %d: %w", kind, rentalID, err)
	}
	return nil
}
