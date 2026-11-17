// Package sim contains the simulator: the virtual clock, release publisher and fault injection.
package sim

import (
	"database/sql"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/internal/store"
)

const clockKey = "sim.clock_offset_seconds"

// Clock is the time source for the control plane. Demo mode moves it forward so that a
// scripted month takes minutes. Outside demo mode the offset stays zero.
type Clock struct {
	mu     sync.RWMutex
	db     *sql.DB
	offset time.Duration
}

// NewClock loads the saved offset from the database.
func NewClock(db *sql.DB) (*Clock, error) {
	raw, err := store.GetKV(db, clockKey)
	if err != nil {
		return nil, err
	}
	c := &Clock{db: db}
	if raw != "" {
		secs, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse saved clock offset %q: %w", raw, err)
		}
		c.offset = time.Duration(secs) * time.Second
	}
	return c, nil
}

// Now returns the virtual time.
func (c *Clock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().UTC().Add(c.offset)
}

// Offset returns the difference between virtual time and wall time.
func (c *Clock) Offset() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.offset
}

// Advance moves the virtual time forward by d and saves the new offset.
func (c *Clock) Advance(d time.Duration) error {
	if d < 0 {
		return fmt.Errorf("advance clock by %s: the clock cannot go back", d)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset += d
	return store.SetKV(c.db, clockKey, strconv.FormatInt(int64(c.offset/time.Second), 10))
}
