package pack_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
)

// The core must not name an application: every name comes from the manifest of a pack.
// This test reads each Go file of the core that is not a test, the simulator of the demo
// too, and fails when one has the name of the example application. Recorded answers of a
// model (internal/agent/replay) and other data files are not Go files, so they are not in
// this check.
func TestNoGoFileOfTheCoreNamesTheExampleApplication(t *testing.T) {
	root := packtest.Root(t)
	name := packtest.Example(t).Name
	for _, tree := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, d fs.DirEntry,
			err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") {
				return err
			}
			text, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for number, line := range strings.Split(string(text), "\n") {
				if strings.Contains(strings.ToLower(line), name) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s:%d names the example application: %s", rel, number+1,
						strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
