// Package storetest gives each test its own empty database schema in a real PostgreSQL.
//
// The server comes from CLANKER_TEST_DATABASE_URL (CI sets it to a service container).
// Without the variable, the package starts a PostgreSQL container with Docker and keeps it
// for the next test run.
package storetest

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/store"
)

const (
	urlEnv    = "CLANKER_TEST_DATABASE_URL"
	container = "clanker-postgres-test"
	localURL  = "postgres://postgres:test@127.0.0.1:7433/postgres?sslmode=disable"
)

var (
	once    sync.Once
	baseURL string
	baseErr error
)

// URL returns the address of the PostgreSQL server of the tests.
func URL(t testing.TB) string {
	t.Helper()
	once.Do(func() { baseURL, baseErr = server() })
	if baseErr != nil {
		t.Fatalf("no PostgreSQL for the test: %v", baseErr)
	}
	return baseURL
}

// Open returns a database with the schema of the control plane and no rows. The schema is
// removed when the test ends.
func Open(t testing.TB) *sql.DB {
	t.Helper()
	baseURL := URL(t)
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	schema := "t_" + hex.EncodeToString(raw)
	admin, err := sql.Open("pgx", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		admin.Close()
		t.Fatalf("create the schema of the test: %v", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := store.Open(parsed.String())
	if err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Errorf("remove the schema of the test: %v", err)
		}
		admin.Close()
	})
	return db
}

// server returns the address of a PostgreSQL that answers.
func server() (string, error) {
	if address := os.Getenv(urlEnv); address != "" {
		return address, wait(address)
	}
	if exec.Command("docker", "start", container).Run() != nil {
		out, err := exec.Command("docker", "run", "-d", "--name", container,
			"-p", "127.0.0.1:7433:5432", "-e", "POSTGRES_PASSWORD=test", "--tmpfs",
			"/var/lib/postgresql", store.PostgresImage).CombinedOutput()
		// Test binaries of several packages start at the same time. Another one can win.
		if err != nil && !strings.Contains(string(out), "already in use") {
			return "", fmt.Errorf("start the container %s: %w: %s; or set %s", container, err,
				strings.TrimSpace(string(out)), urlEnv)
		}
	}
	return localURL, wait(localURL)
}

func wait(address string) error {
	deadline := time.Now().Add(60 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		db, err := sql.Open("pgx", address)
		if err == nil {
			err = db.Ping()
			db.Close()
		}
		if err == nil {
			return nil
		}
		last = err
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("PostgreSQL did not answer in 60 seconds: %w", last)
}
