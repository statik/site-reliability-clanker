package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeReset struct {
	destroyed []string
	destroy   error
	calls     []string
	docker    map[string]error
	output    map[string]string
}

func (f *fakeReset) resetter() resetter {
	return resetter{
		destroy: func(_ context.Context, o options) error {
			f.destroyed = append(f.destroyed, o.env)
			return f.destroy
		},
		docker: func(args ...string) ([]byte, error) {
			f.calls = append(f.calls, strings.Join(args, " "))
			return []byte(f.output[args[0]]), f.docker[args[0]]
		},
		out: &bytes.Buffer{},
	}
}

// dataDir makes a data directory with the entries of an environment and two that stay.
func dataDir(t *testing.T, setupJSON string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"state", "work", "logs", "plugin-cache", "packs"} {
		if err := os.MkdirAll(filepath.Join(dir, name, "inner"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{"secrets.env": "KEY=1\n", "database.password": "p\n",
		"broker.key": "k\n", "notes.txt": "mine\n"}
	if setupJSON != "" {
		files["setup.json"] = setupJSON
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func entries(t *testing.T, dir string) string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range list {
		names = append(names, entry.Name())
	}
	return strings.Join(names, " ")
}

func TestResetRemovesTheEnvironmentTheDatabaseAndTheSetup(t *testing.T) {
	dir := dataDir(t, `{"target":"local","env":"demo"}`)
	fake := &fakeReset{}
	if err := fake.resetter().run(context.Background(), options{
		dataDir: dir, env: "local", cloud: "local"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fake.destroyed, " "); got != "demo" {
		t.Errorf("destroyed %q, want the environment of the saved setup", got)
	}
	name := databaseName(dir)
	want := "rm -f -v " + name + "|volume rm -f " + name
	if got := strings.Join(fake.calls, "|"); got != want {
		t.Errorf("docker calls %q, want %q", got, want)
	}
	if got := entries(t, dir); got != "notes.txt packs plugin-cache" {
		t.Errorf("the data directory has %q after the reset", got)
	}
}

func TestResetWithNoSavedSetupUsesTheFlag(t *testing.T) {
	dir := dataDir(t, "")
	fake := &fakeReset{}
	if err := fake.resetter().run(context.Background(), options{
		dataDir: dir, env: "local", cloud: "local"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fake.destroyed, " "); got != "local" {
		t.Errorf("destroyed %q, want local", got)
	}
}

func TestResetKeepsEverythingWhenTheDestroyFails(t *testing.T) {
	dir := dataDir(t, `{"target":"local","env":"local"}`)
	before := entries(t, dir)
	fake := &fakeReset{destroy: errors.New("the cluster does not answer")}
	err := fake.resetter().run(context.Background(), options{
		dataDir: dir, env: "local", cloud: "local"})
	if err == nil || !strings.Contains(err.Error(), "the cluster does not answer") {
		t.Fatalf("error %v, want the cause of the destroy", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("docker ran after a failed destroy: %v", fake.calls)
	}
	if got := entries(t, dir); got != before {
		t.Errorf("the data directory changed to %q", got)
	}
}

func TestResetAcceptsADatabaseThatDoesNotExist(t *testing.T) {
	dir := dataDir(t, "")
	fake := &fakeReset{
		docker: map[string]error{"rm": errors.New("exit status 1")},
		output: map[string]string{"rm": "Error response from daemon: No such container: x"},
	}
	if err := fake.resetter().run(context.Background(), options{
		dataDir: dir, env: "local", cloud: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Errorf("the state directory is still there: %v", err)
	}
}

func TestResetStopsWhenDockerFails(t *testing.T) {
	dir := dataDir(t, "")
	fake := &fakeReset{
		docker: map[string]error{"rm": errors.New("exit status 1")},
		output: map[string]string{"rm": "Cannot connect to the Docker daemon"},
	}
	err := fake.resetter().run(context.Background(), options{
		dataDir: dir, env: "local", cloud: "local"})
	if err == nil || !strings.Contains(err.Error(), "Cannot connect") {
		t.Fatalf("error %v, want the answer of docker", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "database.password")); err != nil {
		t.Errorf("the password file must stay: %v", err)
	}
}

func TestResetRefusesACloud(t *testing.T) {
	cases := map[string]options{
		"flag":  {cloud: "aws", env: "prod", dataDir: dataDir(t, "")},
		"saved": {cloud: "local", env: "local", dataDir: dataDir(t, `{"target":"aws"}`)},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			fake := &fakeReset{}
			err := fake.resetter().run(context.Background(), o)
			if err == nil || !strings.Contains(err.Error(), "only for the local target") {
				t.Fatalf("error %v, want a refusal", err)
			}
			if len(fake.destroyed)+len(fake.calls) != 0 {
				t.Errorf("the reset acted: %v %v", fake.destroyed, fake.calls)
			}
		})
	}
}
