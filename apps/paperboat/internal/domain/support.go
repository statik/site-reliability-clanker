package domain

import (
	"fmt"
	"time"
)

// Ticket status values.
const (
	TicketOpen     = "open"
	TicketWaiting  = "waiting"
	TicketResolved = "resolved"
)

// EscalateAfter is how long a ticket stays open at one priority before the worker raises it.
// The value is short so a demonstration shows escalation.
const EscalateAfter = 2 * time.Minute

// Priorities lists ticket priorities from lowest to highest.
var Priorities = []string{"low", "normal", "high", "urgent"}

// Channels lists the channels a support interaction can arrive on.
var Channels = []string{"phone", "email", "chat"}

// ValidatePriority reports whether p is a ticket priority.
func ValidatePriority(p string) error {
	return oneOf("priority", p, Priorities)
}

// ValidateChannel reports whether c is an interaction channel.
func ValidateChannel(c string) error {
	return oneOf("channel", c, Channels)
}

// ValidateTicketStatus reports whether s is a ticket status.
func ValidateTicketStatus(s string) error {
	return oneOf("status", s, []string{TicketOpen, TicketWaiting, TicketResolved})
}

func oneOf(field, value string, allowed []string) error {
	for _, a := range allowed {
		if a == value {
			return nil
		}
	}
	return fmt.Errorf("%w: %s must be one of %v, got %q", ErrInvalid, field, allowed, value)
}

// EscalatedPriority returns the next priority for a ticket when it is due for escalation.
// since is the time of the last escalation, or the creation time.
func EscalatedPriority(status, priority string, since, now time.Time) (string, bool) {
	if status != TicketOpen || now.Sub(since) < EscalateAfter {
		return "", false
	}
	for i, p := range Priorities {
		if p == priority && i < len(Priorities)-1 {
			return Priorities[i+1], true
		}
	}
	return "", false
}
