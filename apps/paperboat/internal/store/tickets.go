package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
)

// Ticket is one support ticket.
type Ticket struct {
	ID           int64     `json:"id"`
	CustomerID   int64     `json:"customer_id"`
	Customer     string    `json:"customer"`
	RentalID     *int64    `json:"rental_id,omitempty"`
	DeliveryID   *int64    `json:"delivery_id,omitempty"`
	Subject      string    `json:"subject"`
	Status       string    `json:"status"`
	Priority     string    `json:"priority"`
	CreatedAt    time.Time `json:"created_at"`
	EscalatedAt  time.Time `json:"escalated_at"`
	Interactions int       `json:"interactions"`
}

// NewTicket is the request for a new ticket. A Body becomes the first interaction.
type NewTicket struct {
	CustomerID int64  `json:"customer_id"`
	RentalID   *int64 `json:"rental_id"`
	DeliveryID *int64 `json:"delivery_id"`
	Subject    string `json:"subject"`
	Priority   string `json:"priority"`
	Channel    string `json:"channel"`
	Body       string `json:"body"`
}

// Interaction is one message or call on a ticket.
type Interaction struct {
	ID        int64     `json:"id"`
	TicketID  int64     `json:"ticket_id"`
	Author    string    `json:"author"`
	Channel   string    `json:"channel"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// NewInteraction is the request for a new interaction. A Status also moves the ticket.
type NewInteraction struct {
	Author  string `json:"author"`
	Channel string `json:"channel"`
	Body    string `json:"body"`
	Status  string `json:"status"`
}

const ticketSelect = `
	SELECT k.id, k.customer_id, c.name, k.rental_id, k.delivery_id, k.subject, k.status,
	       k.priority, k.created_at, k.escalated_at,
	       (SELECT COUNT(*) FROM interactions i WHERE i.ticket_id = k.id)
	FROM tickets k JOIN customers c ON c.id = k.customer_id`

func scanTicket(row pgx.CollectableRow) (Ticket, error) {
	var t Ticket
	err := row.Scan(&t.ID, &t.CustomerID, &t.Customer, &t.RentalID, &t.DeliveryID, &t.Subject,
		&t.Status, &t.Priority, &t.CreatedAt, &t.EscalatedAt, &t.Interactions)
	return t, err
}

// Tickets returns the newest tickets, unresolved ones first.
func (s *Store) Tickets(ctx context.Context) ([]Ticket, error) {
	rows, err := s.pool.Query(ctx, ticketSelect+`
		ORDER BY (k.status = 'resolved'), k.id DESC LIMIT 200`)
	if err != nil {
		return nil, fmt.Errorf("list tickets: %w", err)
	}
	tickets, err := pgx.CollectRows(rows, scanTicket)
	if err != nil {
		return nil, fmt.Errorf("list tickets: %w", err)
	}
	return tickets, nil
}

// Ticket returns one ticket.
func (s *Store) Ticket(ctx context.Context, id int64) (Ticket, error) {
	rows, err := s.pool.Query(ctx, ticketSelect+` WHERE k.id = $1`, id)
	if err != nil {
		return Ticket{}, fmt.Errorf("read ticket %d: %w", id, err)
	}
	ticket, err := pgx.CollectExactlyOneRow(rows, scanTicket)
	if err != nil {
		return Ticket{}, notFound(err, "ticket", id)
	}
	return ticket, nil
}

// CreateTicket opens a ticket. The caller validates the fields first.
func (s *Store) CreateTicket(ctx context.Context, in NewTicket) (Ticket, error) {
	var id int64
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var customer string
		err := tx.QueryRow(ctx, `SELECT name FROM customers WHERE id = $1`, in.CustomerID).
			Scan(&customer)
		if err != nil {
			return notFound(err, "customer", in.CustomerID)
		}
		err = tx.QueryRow(ctx, `INSERT INTO tickets
			(customer_id, rental_id, delivery_id, subject, status, priority)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			in.CustomerID, in.RentalID, in.DeliveryID, in.Subject, domain.TicketOpen,
			in.Priority).Scan(&id)
		if err != nil {
			return invalidReference(err, "ticket")
		}
		if in.Body == "" {
			return nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO interactions (ticket_id, author, channel, body)
			VALUES ($1, $2, $3, $4)`, id, customer, in.Channel, in.Body)
		if err != nil {
			return fmt.Errorf("insert first interaction: %w", err)
		}
		return nil
	})
	if err != nil {
		return Ticket{}, err
	}
	return s.Ticket(ctx, id)
}

// Interactions returns the interactions of one ticket, oldest first.
func (s *Store) Interactions(ctx context.Context, ticketID int64) ([]Interaction, error) {
	if _, err := s.Ticket(ctx, ticketID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id, ticket_id, author, channel, body, created_at
		FROM interactions WHERE ticket_id = $1 ORDER BY id`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("list interactions of ticket %d: %w", ticketID, err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Interaction, error) {
		var i Interaction
		err := row.Scan(&i.ID, &i.TicketID, &i.Author, &i.Channel, &i.Body, &i.CreatedAt)
		return i, err
	})
	if err != nil {
		return nil, fmt.Errorf("list interactions of ticket %d: %w", ticketID, err)
	}
	return list, nil
}

// AddInteraction adds an interaction and, when in.Status is set, moves the ticket.
func (s *Store) AddInteraction(ctx context.Context, ticketID int64, in NewInteraction,
) (Interaction, error) {
	out := Interaction{TicketID: ticketID, Author: in.Author, Channel: in.Channel, Body: in.Body}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var current string
		err := tx.QueryRow(ctx, `SELECT status FROM tickets WHERE id = $1 FOR UPDATE`, ticketID).
			Scan(&current)
		if err != nil {
			return notFound(err, "ticket", ticketID)
		}
		err = tx.QueryRow(ctx, `INSERT INTO interactions (ticket_id, author, channel, body)
			VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
			ticketID, in.Author, in.Channel, in.Body).Scan(&out.ID, &out.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert interaction on ticket %d: %w", ticketID, err)
		}
		if in.Status == "" || in.Status == current {
			return nil
		}
		_, err = tx.Exec(ctx, `UPDATE tickets SET status = $2, escalated_at = now()
			WHERE id = $1`, ticketID, in.Status)
		if err != nil {
			return fmt.Errorf("set status of ticket %d: %w", ticketID, err)
		}
		return nil
	})
	return out, err
}

// OpenTickets returns the open tickets for the escalation job.
func (s *Store) OpenTickets(ctx context.Context) ([]Ticket, error) {
	rows, err := s.pool.Query(ctx, ticketSelect+` WHERE k.status = 'open' ORDER BY k.id`)
	if err != nil {
		return nil, fmt.Errorf("list open tickets: %w", err)
	}
	tickets, err := pgx.CollectRows(rows, scanTicket)
	if err != nil {
		return nil, fmt.Errorf("list open tickets: %w", err)
	}
	return tickets, nil
}

// Escalate raises the priority of an open ticket. It does nothing when another process
// changed the ticket first.
func (s *Store) Escalate(ctx context.Context, id int64, from, to string) error {
	_, err := s.pool.Exec(ctx, `UPDATE tickets SET priority = $3, escalated_at = now()
		WHERE id = $1 AND priority = $2 AND status = 'open'`, id, from, to)
	if err != nil {
		return fmt.Errorf("escalate ticket %d: %w", id, err)
	}
	return nil
}
