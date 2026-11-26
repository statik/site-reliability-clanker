package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The local bootstrap works in a copy below the data directory, so two data directories
// that share one checkout do not share the backend that OpenTofu records (#140).
func TestLocalBootstrapsOfTwoDataDirectoriesDoNotShareTheirBackend(t *testing.T) {
	f := newOperatorFake(t)
	before := listFiles(t, f.source)
	for _, dataDir := range []string{t.TempDir(), t.TempDir()} {
		o := options{dataDir: dataDir, sourceDir: f.source, source: os.DirFS(f.source),
			env: "local", cloud: "local"}
		if err := bootstrap(context.Background(), o); err != nil {
			t.Fatalf("bootstrap with the data directory %s: %v", dataDir, err)
		}
		state := filepath.Join(dataDir, "state", "local", "00-bootstrap.tfstate")
		if !exists(t, state) {
			t.Errorf("the bootstrap left no state in %s", state)
		}
	}
	if after := listFiles(t, f.source); !slices.Equal(before, after) {
		t.Errorf("the bootstrap changed the source files from %q to %q", before, after)
	}
}

func TestBootstrapOfAnotherCloudIsRefused(t *testing.T) {
	err := bootstrap(context.Background(), options{cloud: "azure"})
	if err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("error = %v, want a refusal", err)
	}
}
