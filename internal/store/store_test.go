package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenUsesTheJournalModeFromTheEnvironment(t *testing.T) {
	for value, want := range map[string]string{"": "wal", "WAL": "wal", "delete": "delete"} {
		t.Setenv(JournalEnv, value)
		db, err := Open(filepath.Join(t.TempDir(), "clanker.db"))
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		var mode string
		if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if mode != want {
			t.Errorf("%s=%q: journal mode %q, want %q", JournalEnv, value, mode, want)
		}
	}
}

func TestOpenRefusesAJournalModeThatIsNotSupported(t *testing.T) {
	t.Setenv(JournalEnv, "memory")
	_, err := Open(filepath.Join(t.TempDir(), "clanker.db"))
	if err == nil || !strings.Contains(err.Error(), "WAL or DELETE") {
		t.Fatalf("error = %v, want a refusal that names the two modes", err)
	}
}
