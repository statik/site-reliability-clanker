// Package audit is the append-only, hash-chained event log. Every other component reports
// what it did by appending an event here. The timeline, the notifier and the evidence export
// read the same stream.
package audit

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/internal/store"
)

// Event is one record in the log.
type Event struct {
	Seq      int64          `json:"seq"`
	TS       string         `json:"ts"`
	WallTS   string         `json:"wall_ts"`
	Type     string         `json:"type"`
	Actor    string         `json:"actor"`
	Env      string         `json:"env,omitempty"`
	Layer    string         `json:"layer,omitempty"`
	WorkItem string         `json:"work_item,omitempty"`
	Summary  string         `json:"summary"`
	Data     map[string]any `json:"data"`
	PrevHash string         `json:"prev_hash"`
	Hash     string         `json:"hash"`
}

// Clock gives the virtual time for new events.
type Clock interface{ Now() time.Time }

// Log appends and reads events.
type Log struct {
	mu    sync.Mutex
	db    *sql.DB
	clock Clock
	subs  map[chan Event]struct{}
}

// New returns a log on db.
func New(db *sql.DB, clock Clock) *Log {
	return &Log{db: db, clock: clock, subs: map[chan Event]struct{}{}}
}

const genesis = "0000000000000000000000000000000000000000000000000000000000000000"

func hashEvent(e Event, dataJSON string) string {
	parts := []string{e.PrevHash, e.TS, e.WallTS, e.Type, e.Actor, e.Env, e.Layer, e.WorkItem,
		e.Summary, dataJSON}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// Append adds an event to the chain. The caller sets Type, Actor, Summary and the optional
// fields. Append sets the sequence number, the times and the hashes.
func (l *Log) Append(e Event) (Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.Type == "" || e.Actor == "" {
		return Event{}, fmt.Errorf("append audit event: type and actor are required, got %q %q",
			e.Type, e.Actor)
	}
	if e.Data == nil {
		e.Data = map[string]any{}
	}
	data, err := json.Marshal(e.Data)
	if err != nil {
		return Event{}, fmt.Errorf("encode data of audit event %s: %w", e.Type, err)
	}
	prev := genesis
	err = l.db.QueryRow(`SELECT hash FROM events ORDER BY seq DESC LIMIT 1`).Scan(&prev)
	if err != nil && err != sql.ErrNoRows {
		return Event{}, fmt.Errorf("read last audit hash: %w", err)
	}
	if e.TS == "" {
		e.TS = store.FormatTime(l.clock.Now())
	}
	e.WallTS = store.FormatTime(time.Now())
	e.PrevHash = prev
	e.Hash = hashEvent(e, string(data))
	err = l.db.QueryRow(`INSERT INTO events
		(ts, wall_ts, type, actor, env, layer, work_item, summary, data, prev_hash, hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING seq`,
		e.TS, e.WallTS, e.Type, e.Actor, e.Env, e.Layer, e.WorkItem, e.Summary, string(data),
		e.PrevHash, e.Hash).Scan(&e.Seq)
	if err != nil {
		return Event{}, fmt.Errorf("insert audit event %s: %w", e.Type, err)
	}
	for ch := range l.subs {
		select {
		case ch <- e:
		default:
		}
	}
	return e, nil
}

// Subscribe returns a channel that receives each new event, and a function to stop.
// A slow subscriber loses events instead of blocking the log.
func (l *Log) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 256)
	l.mu.Lock()
	l.subs[ch] = struct{}{}
	l.mu.Unlock()
	return ch, func() {
		l.mu.Lock()
		delete(l.subs, ch)
		l.mu.Unlock()
	}
}

// Filter selects events for Query. Empty fields match everything.
type Filter struct {
	AfterSeq   int64
	From, To   string
	Types      []string
	TypePrefix string
	Env        string
	WorkItem   string
	Search     string
	Limit      int
	Descending bool
}

func (f Filter) where() (string, []any) {
	var conds []string
	var args []any
	// mark adds an argument and returns its placeholder, such as $3.
	mark := func(arg any) string {
		args = append(args, arg)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.AfterSeq > 0 {
		conds = append(conds, "seq > "+mark(f.AfterSeq))
	}
	if f.From != "" {
		conds = append(conds, "ts >= "+mark(f.From))
	}
	if f.To != "" {
		conds = append(conds, "ts <= "+mark(f.To))
	}
	if f.Env != "" {
		conds = append(conds, "env = "+mark(f.Env))
	}
	if f.WorkItem != "" {
		conds = append(conds, "work_item = "+mark(f.WorkItem))
	}
	if f.TypePrefix != "" {
		conds = append(conds, "type LIKE "+mark(f.TypePrefix+"%"))
	}
	if f.Search != "" {
		// The search does not depend on the case of the letters.
		like := mark("%" + f.Search + "%")
		conds = append(conds, "(summary ILIKE "+like+" OR data ILIKE "+like+" OR actor ILIKE "+
			like+" OR type ILIKE "+like+")")
	}
	if len(f.Types) > 0 {
		marks := make([]string, 0, len(f.Types))
		for _, t := range f.Types {
			marks = append(marks, mark(t))
		}
		conds = append(conds, "type IN ("+strings.Join(marks, ",")+")")
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// Query returns the events that match f, in sequence order.
func (l *Log) Query(f Filter) ([]Event, error) {
	where, args := f.where()
	order := " ORDER BY seq ASC"
	if f.Descending {
		order = " ORDER BY seq DESC"
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 5000
	}
	rows, err := l.db.Query(`SELECT seq, ts, wall_ts, type, actor, env, layer, work_item,
		summary, data, prev_hash, hash FROM events`+where+order+
		fmt.Sprintf(` LIMIT $%d`, len(args)+1), append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("query audit events: %w", err)
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var e Event
		var data string
		if err := rows.Scan(&e.Seq, &e.TS, &e.WallTS, &e.Type, &e.Actor, &e.Env, &e.Layer,
			&e.WorkItem, &e.Summary, &data, &e.PrevHash, &e.Hash); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		if err := json.Unmarshal([]byte(data), &e.Data); err != nil {
			return nil, fmt.Errorf("decode data of audit event %d: %w", e.Seq, err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// Verification is the result of a check of the whole chain.
type Verification struct {
	OK        bool   `json:"ok"`
	Count     int    `json:"count"`
	HeadHash  string `json:"head_hash"`
	BrokenSeq int64  `json:"broken_seq,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// Verify recomputes every hash and checks every link.
func (l *Log) Verify() (Verification, error) {
	rows, err := l.db.Query(`SELECT seq, ts, wall_ts, type, actor, env, layer, work_item,
		summary, data, prev_hash, hash FROM events ORDER BY seq ASC`)
	if err != nil {
		return Verification{}, fmt.Errorf("read audit chain: %w", err)
	}
	defer rows.Close()
	result := Verification{OK: true, HeadHash: genesis}
	prev := genesis
	for rows.Next() {
		var e Event
		var data string
		if err := rows.Scan(&e.Seq, &e.TS, &e.WallTS, &e.Type, &e.Actor, &e.Env, &e.Layer,
			&e.WorkItem, &e.Summary, &data, &e.PrevHash, &e.Hash); err != nil {
			return Verification{}, fmt.Errorf("scan audit event: %w", err)
		}
		result.Count++
		if e.PrevHash != prev {
			return Verification{Count: result.Count, BrokenSeq: e.Seq,
				Reason: "prev_hash does not match the hash of the event before"}, nil
		}
		if hashEvent(e, data) != e.Hash {
			return Verification{Count: result.Count, BrokenSeq: e.Seq,
				Reason: "the stored hash does not match the event content"}, nil
		}
		prev = e.Hash
	}
	result.HeadHash = prev
	return result, rows.Err()
}
