// Package store opens the PostgreSQL database of the control plane.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
)

const schema = `
CREATE TABLE IF NOT EXISTS events (
  seq BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
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
  trigger TEXT NOT NULL DEFAULT '', pr_id BIGINT NOT NULL DEFAULT 0,
  commit_sha TEXT NOT NULL DEFAULT '', base_commit TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '', explanation TEXT NOT NULL DEFAULT '{}',
  scheduled_for TEXT NOT NULL DEFAULT '', rollback_of TEXT NOT NULL DEFAULT '',
  maintenance_window TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
-- A database from before 2026-11-18 has the table without this column.
ALTER TABLE work_items ADD COLUMN IF NOT EXISTS maintenance_window TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS plans (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, work_item TEXT NOT NULL, layer TEXT NOT NULL,
  commit_sha TEXT NOT NULL, plan_sha256 TEXT NOT NULL, plan BYTEA NOT NULL,
  plan_json BYTEA NOT NULL, facts TEXT NOT NULL DEFAULT '{}', policy TEXT NOT NULL DEFAULT '{}',
  superseded BIGINT NOT NULL DEFAULT 0, created_at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS approvals (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, work_item TEXT NOT NULL, approver TEXT NOT NULL,
  decision TEXT NOT NULL, comment TEXT NOT NULL DEFAULT '', commit_sha TEXT NOT NULL,
  plan_hashes TEXT NOT NULL DEFAULT '{}', at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS grants (
  id TEXT PRIMARY KEY, work_item TEXT NOT NULL, env TEXT NOT NULL, layer TEXT NOT NULL,
  identity TEXT NOT NULL, kind TEXT NOT NULL, commit_sha TEXT NOT NULL DEFAULT '',
  plan_sha256 TEXT NOT NULL DEFAULT '', tier TEXT NOT NULL, approvers TEXT NOT NULL DEFAULT '[]',
  not_before TEXT NOT NULL, expires TEXT NOT NULL, signature TEXT NOT NULL,
  status TEXT NOT NULL, issued_at TEXT NOT NULL, ended_at TEXT NOT NULL DEFAULT '');

CREATE TABLE IF NOT EXISTS jobs (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, work_item TEXT NOT NULL, layer TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL, status TEXT NOT NULL, log TEXT NOT NULL DEFAULT '',
  grant_id TEXT NOT NULL DEFAULT '', tool_digest TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL, ended_at TEXT NOT NULL DEFAULT '');

CREATE TABLE IF NOT EXISTS prs (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, repo TEXT NOT NULL, title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '', branch TEXT NOT NULL, base TEXT NOT NULL DEFAULT 'main',
  head_commit TEXT NOT NULL, base_commit TEXT NOT NULL, state TEXT NOT NULL,
  author TEXT NOT NULL, work_item TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
  merged_at TEXT NOT NULL DEFAULT '');

CREATE TABLE IF NOT EXISTS findings (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, detector TEXT NOT NULL, env TEXT NOT NULL,
  key TEXT NOT NULL UNIQUE, title TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '{}',
  state TEXT NOT NULL, work_item TEXT NOT NULL DEFAULT '', at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS snapshots (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, ts TEXT NOT NULL, env TEXT NOT NULL,
  reason TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS snapshots_ts ON snapshots(env, ts);

CREATE TABLE IF NOT EXISTS health (
  ts TEXT NOT NULL, env TEXT NOT NULL, ok BIGINT NOT NULL, total BIGINT NOT NULL,
  p95_ms DOUBLE PRECISION NOT NULL, version TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS health_ts ON health(env, ts);

CREATE TABLE IF NOT EXISTS volume_samples (
  ts TEXT NOT NULL, env TEXT NOT NULL, volume TEXT NOT NULL, used_bytes BIGINT NOT NULL,
  total_bytes BIGINT NOT NULL);
CREATE INDEX IF NOT EXISTS volume_samples_ts ON volume_samples(env, volume, ts);

CREATE TABLE IF NOT EXISTS deliveries (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, event_seq BIGINT NOT NULL, channel TEXT NOT NULL,
  target TEXT NOT NULL, status TEXT NOT NULL, attempts BIGINT NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '', subject TEXT NOT NULL DEFAULT '', at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS users (
  name TEXT PRIMARY KEY, display TEXT NOT NULL, role TEXT NOT NULL, email TEXT NOT NULL,
  last_seen TEXT NOT NULL DEFAULT '');

CREATE UNIQUE INDEX IF NOT EXISTS users_email ON users(lower(email));

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY, email TEXT NOT NULL, expires TEXT NOT NULL, created_at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS device_codes (
  id TEXT PRIMARY KEY, user_code TEXT NOT NULL UNIQUE, email TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL, expires TEXT NOT NULL, created_at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS tokens (
  id TEXT PRIMARY KEY, email TEXT NOT NULL, expires TEXT NOT NULL, created_at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS chat_links (
  platform TEXT NOT NULL, workspace TEXT NOT NULL, account TEXT NOT NULL,
  account_name TEXT NOT NULL DEFAULT '', email TEXT NOT NULL, created_at TEXT NOT NULL,
  PRIMARY KEY (platform, workspace, account));

CREATE TABLE IF NOT EXISTS chat_link_codes (
  id TEXT PRIMARY KEY, platform TEXT NOT NULL, workspace TEXT NOT NULL, account TEXT NOT NULL,
  account_name TEXT NOT NULL DEFAULT '', expires TEXT NOT NULL, created_at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS chat_messages (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, platform TEXT NOT NULL,
  work_item TEXT NOT NULL, binding TEXT NOT NULL, place TEXT NOT NULL, message TEXT NOT NULL,
  content TEXT NOT NULL, open BIGINT NOT NULL DEFAULT 1, created_at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS chat (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, work_item TEXT NOT NULL DEFAULT '',
  username TEXT NOT NULL, role TEXT NOT NULL, body TEXT NOT NULL,
  citations TEXT NOT NULL DEFAULT '[]', at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS agent_calls (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, role TEXT NOT NULL, work_item TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL, mode TEXT NOT NULL, prompt_tokens BIGINT NOT NULL DEFAULT 0,
  completion_tokens BIGINT NOT NULL DEFAULT 0, cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
  transcript TEXT NOT NULL DEFAULT '', at TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS layer_outputs (
  env TEXT NOT NULL, layer TEXT NOT NULL, outputs TEXT NOT NULL, updated_at TEXT NOT NULL,
  PRIMARY KEY (env, layer));

CREATE TABLE IF NOT EXISTS git_repos (name TEXT PRIMARY KEY);

CREATE TABLE IF NOT EXISTS git_objects (
  repo TEXT NOT NULL, hash TEXT NOT NULL, kind SMALLINT NOT NULL, content BYTEA NOT NULL,
  PRIMARY KEY (repo, hash));

CREATE TABLE IF NOT EXISTS git_refs (
  repo TEXT NOT NULL, name TEXT NOT NULL, target TEXT NOT NULL, PRIMARY KEY (repo, name));
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

// PostgresImage is the container image of the database on the local target and in tests.
const PostgresImage = "postgres:18.7"

// URLEnv names the environment variable with the address of the database. On a cloud the
// setup sets it. On the local target `clanker up` starts a PostgreSQL container and sets it.
const URLEnv = "CLANKER_DATABASE_URL"

// schemaLock is the key of the advisory lock that serializes schema changes, so that two
// processes that start at the same moment do not create the same table twice.
const schemaLock = 7070001

// Open connects to the PostgreSQL database at url and applies the schema.
//
// The pool has one connection. The control plane relied on one writer when its database
// was an embedded file, and the hash chain of the audit log needs each append to see the
// one before it. One connection keeps that order.
func Open(url string) (*sql.DB, error) {
	if url == "" {
		return nil, fmt.Errorf("the address of the database is empty: set %s to a "+
			"postgres:// address", URLEnv)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("open the database: %w", err)
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to the database: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("start the schema transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // a rollback after a commit does nothing
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, schemaLock); err != nil {
		db.Close()
		return nil, fmt.Errorf("lock the schema: %w", err)
	}
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply the schema: %w", err)
	}
	if err := tx.Commit(); err != nil {
		db.Close()
		return nil, fmt.Errorf("commit the schema: %w", err)
	}
	return db, nil
}

// LayerOutputs returns the outputs of a layer as the runner reported them after the last
// apply. A layer that was never applied gives an empty map.
func LayerOutputs(db *sql.DB, env, layer string) (map[string]any, error) {
	var raw string
	err := db.QueryRow(`SELECT outputs FROM layer_outputs WHERE env = $1 AND layer = $2`, env,
		layer).Scan(&raw)
	if err == sql.ErrNoRows {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read outputs of %s/%s: %w", env, layer, err)
	}
	outputs := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &outputs); err != nil {
		return nil, fmt.Errorf("decode outputs of %s/%s: %w", env, layer, err)
	}
	return outputs, nil
}

// SetLayerOutputs records the outputs of a layer.
func SetLayerOutputs(db *sql.DB, env, layer string, outputs map[string]any, now string) error {
	raw, err := json.Marshal(outputs)
	if err != nil {
		return fmt.Errorf("encode outputs of %s/%s: %w", env, layer, err)
	}
	_, err = db.Exec(`INSERT INTO layer_outputs (env, layer, outputs, updated_at)
		VALUES ($1,$2,$3,$4) ON CONFLICT (env, layer)
		DO UPDATE SET outputs = excluded.outputs, updated_at = excluded.updated_at`, env, layer,
		string(raw), now)
	if err != nil {
		return fmt.Errorf("record outputs of %s/%s: %w", env, layer, err)
	}
	return nil
}

// GetKV reads a value from the kv table. A missing key gives "".
func GetKV(db *sql.DB, key string) (string, error) {
	var v string
	err := db.QueryRow(`SELECT value FROM kv WHERE key = $1`, key).Scan(&v)
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
	_, err := db.Exec(`INSERT INTO kv(key, value) VALUES($1, $2)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("write kv %q: %w", key, err)
	}
	return nil
}
