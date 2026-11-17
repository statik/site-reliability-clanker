package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// writerLock is the key of the advisory lock that one control plane holds on a database
// while it runs. The pool of one connection keeps the order of the writes of one process.
// It does not stop a second process. This lock does.
const writerLock = 7070002

// Writer is the hold of one control plane on its database.
type Writer struct {
	db   *sql.DB
	conn *sql.Conn
	lost chan struct{}
	stop context.CancelFunc
}

// HoldWriter takes the writer lock of the database at url and keeps it on a connection of
// its own. When another control plane holds the lock, HoldWriter calls waiting once and
// waits until the lock is free or ctx ends.
//
// PostgreSQL frees the lock when the connection ends, so a control plane that stops or
// loses the database lets the next one in. The holder must then stop to write: see Lost.
func HoldWriter(ctx context.Context, url string, waiting func()) (*Writer, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("open the database for the writer lock: %w", err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to the database for the writer lock: %w", err)
	}
	var free bool
	err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, writerLock).Scan(&free)
	if err == nil && !free {
		waiting()
		_, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, writerLock)
	}
	if err != nil {
		conn.Close()
		db.Close()
		return nil, fmt.Errorf("take the writer lock: %w", err)
	}
	watch, stop := context.WithCancel(context.Background())
	w := &Writer{db: db, conn: conn, lost: make(chan struct{}), stop: stop}
	go w.watch(watch, 5*time.Second)
	return w, nil
}

// watch closes lost when the connection that holds the lock no longer answers.
func (w *Writer) watch(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ping, cancel := context.WithTimeout(ctx, every)
			err := w.conn.PingContext(ping)
			cancel()
			if err != nil && ctx.Err() == nil {
				close(w.lost)
				return
			}
		}
	}
}

// Lost is closed when the connection that holds the lock broke. Another control plane can
// then take the lock, so the holder must stop. Between the break and the stop, the holder
// can still write for a few seconds: the lock is not a fence around each write.
func (w *Writer) Lost() <-chan struct{} { return w.lost }

// Close gives the lock back.
func (w *Writer) Close() error {
	w.stop()
	err := w.conn.Close()
	if closeErr := w.db.Close(); err == nil {
		err = closeErr
	}
	return err
}
