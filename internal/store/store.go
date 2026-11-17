// Package store opens the embedded SQLite database of the control plane.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

const schema = `
CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  ts TEXT NOT NULL, wall_ts TEXT NOT NULL, type TEXT NOT NULL, actor TEXT NOT NULL,
  env TEXT NOT NULL DEFAULT '', layer TEXT NOT NULL DEFAULT '',
  work_item TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
  data TEXT NOT NULL DEFAULT '{}', prev_hash TEXT NOT NULL, hash TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS events_ts ON events(ts);
CREATE INDEX IF NOT EXISTS events_wi ON events(work_item);

CREATE TABLE IF NOT EXISTS work_items (
  id TEXT PRIMARY KEY, title TEXT NOT NULL, kind TEXT NOT NULL, env TEXT NOT NULL,
  layers TEXT NOT NULL DEFAULT '[]', state TEXT NOT NULL,
  tier TEXT NOT NULL DEFAULT '', tier_rule TEXT NOT NULL DEFAULT '',
  trigger TEXT NOT NULL DEFAULT '', pr_id INTEGER NOT NULL DEFAULT 0,
  commit_sha TEXT NOT NULL DEFAULT '', base_commit TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '', explanation TEXT NOT NULL DEFAULT '{}',
  scheduled_for TEXT NOT NULL DEFAULT '', rollback_of TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS plans (
  id INTEGER PRIMARY KEY AUTOINCREMENT, work_item TEXT NOT NULL, layer TEXT NOT NULL,
  commit_sha TEXT NOT NULL, plan_sha256 TEXT NOT NULL, plan_path TEXT NOT NULL,
  json_path TEXT NOT NULL, facts TEXT NOT NULL DEFAULT '{}', policy TEXT NOT NULL DEFAULT '{}',
  superseded INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS approvals (
  id INTEGER PRIMARY KEY AUTOINCREMENT, work_item TEXT NOT NULL, approver TEXT NOT NULL,
  decision TEXT NOT NULL, comment TEXT NOT NULL DEFAULT '', commit_sha TEXT NOT NULL,
  plan_hashes TEXT NOT NULL DEFAULT '{}', at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS grants (
  id TEXT PRIMARY KEY, work_item TEXT NOT NULL, env TEXT NOT NULL, layer TEXT NOT NULL,
  identity TEXT NOT NULL, kind TEXT NOT NULL, commit_sha TEXT NOT NULL DEFAULT '',
  plan_sha256 TEXT NOT NULL DEFAULT '', tier TEXT NOT NULL, approvers TEXT NOT NULL DEFAULT '[]',
  not_before TEXT NOT NULL, expires TEXT NOT NULL, signature TEXT NOT NULL,
  status TEXT NOT NULL, issued_at TEXT NOT NULL, ended_at TEXT NOT NULL DEFAULT '');

CREATE TABLE IF NOT EXISTS jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT, work_item TEXT NOT NULL, layer TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL, status TEXT NOT NULL, log_path TEXT NOT NULL DEFAULT '',
  grant_id TEXT NOT NULL DEFAULT '', tool_digest TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL, ended_at TEXT NOT NULL DEFAULT '');

CREATE TABLE IF NOT EXISTS prs (
  id INTEGER PRIMARY KEY AUTOINCREMENT, repo TEXT NOT NULL, title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '', branch TEXT NOT NULL, base TEXT NOT NULL DEFAULT 'main',
  head_commit TEXT NOT NULL, base_commit TEXT NOT NULL, state TEXT NOT NULL,
  author TEXT NOT NULL, work_item TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
  merged_at TEXT NOT NULL DEFAULT '');

CREATE TABLE IF NOT EXISTS findings (
  id INTEGER PRIMARY KEY AUTOINCREMENT, detector TEXT NOT NULL, env TEXT NOT NULL,
  key TEXT NOT NULL UNIQUE, title TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '{}',
  state TEXT NOT NULL, work_item TEXT NOT NULL DEFAULT '', at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT, ts TEXT NOT NULL, env TEXT NOT NULL,
  reason TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS snapshots_ts ON snapshots(env, ts);

CREATE TABLE IF NOT EXISTS health (
  ts TEXT NOT NULL, env TEXT NOT NULL, ok INTEGER NOT NULL, total INTEGER NOT NULL,
  p95_ms REAL NOT NULL, version TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS health_ts ON health(env, ts);

CREATE TABLE IF NOT EXISTS deliveries (
  id INTEGER PRIMARY KEY AUTOINCREMENT, event_seq INTEGER NOT NULL, channel TEXT NOT NULL,
  target TEXT NOT NULL, status TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '', subject TEXT NOT NULL DEFAULT '', at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS users (
  name TEXT PRIMARY KEY, display TEXT NOT NULL, role TEXT NOT NULL, email TEXT NOT NULL,
  last_seen TEXT NOT NULL DEFAULT '');

CREATE TABLE IF NOT EXISTS chat (
  id INTEGER PRIMARY KEY AUTOINCREMENT, work_item TEXT NOT NULL DEFAULT '',
  user TEXT NOT NULL, role TEXT NOT NULL, body TEXT NOT NULL,
  citations TEXT NOT NULL DEFAULT '[]', at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS agent_calls (
  id INTEGER PRIMARY KEY AUTOINCREMENT, role TEXT NOT NULL, work_item TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL, mode TEXT NOT NULL, prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0, cost_usd REAL NOT NULL DEFAULT 0,
  transcript TEXT NOT NULL DEFAULT '', at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`

// TimeFormat is the layout for every timestamp column. It sorts as text.
const TimeFormat = "2006-01-02T15:04:05.000Z"

// FormatTime renders t in UTC with TimeFormat.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeFormat) }

// ParseTime reads a timestamp column. An empty string gives the zero time.
func ParseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(TimeFormat, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

// Open opens the database file at path and applies the schema.
func Open(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)" +
		"&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("apply schema to %s: %w", path, err)
	}
	return db, nil
}

// GetKV reads a value from the kv table. A missing key gives "".
func GetKV(db *sql.DB, key string) (string, error) {
	var v string
	err := db.QueryRow(`SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read kv %q: %w", key, err)
	}
	return v, nil
}

// SetKV writes a value to the kv table.
func SetKV(db *sql.DB, key, value string) error {
	_, err := db.Exec(`INSERT INTO kv(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("write kv %q: %w", key, err)
	}
	return nil
}
