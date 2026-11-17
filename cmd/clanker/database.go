package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/store"
)

// database returns the address of the PostgreSQL database of the control plane.
//
// CLANKER_DATABASE_URL wins: on a cloud the database is a managed service, and the setup
// gives its address. On the local target with no address, this function runs PostgreSQL in
// a Docker container that belongs to the data directory.
func database(o options) (string, error) {
	if address := os.Getenv(store.URLEnv); address != "" {
		return address, nil
	}
	if o.cloud != "local" {
		return "", fmt.Errorf("the control plane for %s needs a database: set %s to a "+
			"postgres:// address", o.cloud, store.URLEnv)
	}
	return localDatabase(o.dataDir)
}

// databaseName is the name of the container and of its volume. It contains a hash of the
// data directory, so two data directories on one machine have two databases.
func databaseName(dataDir string) string {
	sum := sha256.Sum256([]byte(dataDir))
	return "clanker-postgres-" + hex.EncodeToString(sum[:4])
}

func localDatabase(dataDir string) (string, error) {
	name := databaseName(dataDir)
	password, err := databasePassword(dataDir)
	if err != nil {
		return "", err
	}
	if exec.Command("docker", "start", name).Run() != nil {
		// The host port is free for Docker to choose, so two control planes do not collide.
		out, err := exec.Command("docker", "run", "-d", "--name", name,
			"--label", "clanker.role=database", "--label", "clanker.data="+dataDir,
			"-p", "127.0.0.1::5432", "-e", "POSTGRES_PASSWORD="+password,
			"-e", "POSTGRES_DB=clanker", "-v", name+":/var/lib/postgresql",
			store.PostgresImage).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("start the database container %s: %w: %s", name, err,
				strings.TrimSpace(string(out)))
		}
	}
	out, err := exec.Command("docker", "port", name, "5432/tcp").Output()
	if err != nil {
		return "", fmt.Errorf("read the port of the database container %s: %w", name, err)
	}
	hostPort := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	address := fmt.Sprintf("postgres://postgres:%s@%s/clanker?sslmode=disable", password, hostPort)
	return address, waitForDatabase(address, name)
}

// databasePassword reads the password of the local database, or makes one on the first
// start. Only the user can read the file.
func databasePassword(dataDir string) (string, error) {
	path := filepath.Join(dataDir, "database.password")
	saved, err := os.ReadFile(path)
	if err == nil {
		return strings.TrimSpace(string(saved)), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("make the database password: %w", err)
	}
	password := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(password+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("save the database password in %s: %w", path, err)
	}
	return password, nil
}

func waitForDatabase(address, name string) error {
	deadline := time.Now().Add(90 * time.Second)
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
		time.Sleep(time.Second)
	}
	return fmt.Errorf("the database container %s did not answer in 90 seconds: %w; if the "+
		"password file of the data directory was removed, remove the container and its "+
		"volume too: docker rm -f %s && docker volume rm %s", name, last, name, name)
}
