// Package packtest gives the tests of the core the example application pack of this
// repository and a second pack with other names.
package packtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/statik/site-reliability-clanker/internal/pack"
)

// Root returns the root of the checkout: the directory above the test that has the
// directories apps and platform.
func Root(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, apps := os.Stat(filepath.Join(dir, "apps"))
		_, platform := os.Stat(filepath.Join(dir, "platform"))
		if apps == nil && platform == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("the test does not run inside a checkout of the repository")
		}
		dir = parent
	}
}

// Example returns the example pack of the repository: the one pack in the directory apps.
func Example(t testing.TB) pack.Pack {
	t.Helper()
	source := os.DirFS(Root(t))
	dir, err := pack.Discover(source, "apps")
	if err != nil {
		t.Fatal(err)
	}
	return load(t, Root(t), dir)
}

// Second returns a pack with other names than the example pack. A test that passes with
// it does not depend on a name of the example application.
func Second(t testing.TB) pack.Pack {
	t.Helper()
	return load(t, filepath.Join(Root(t), "internal", "pack", "testdata"), "harbor")
}

// SecondSource returns the file system that holds the second pack, at Second(t).Dir().
func SecondSource(t testing.TB) string {
	t.Helper()
	return filepath.Join(Root(t), "internal", "pack", "testdata")
}

func load(t testing.TB, root, dir string) pack.Pack {
	t.Helper()
	p, err := pack.Load(os.DirFS(root), dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Runbooks returns the content of the runbooks repository as a start of the control plane
// seeds it: the policies and routes of the core, and in the directory of the jobs the
// runbooks of the example pack, with the files of each tree in extra added to them.
func Runbooks(t testing.TB, extra ...fs.FS) map[string]fs.FS {
	t.Helper()
	jobs, err := Example(t).RunbookFiles()
	if err != nil || jobs == nil {
		t.Fatalf("the example pack has no runbooks: %v", err)
	}
	merged := fstest.MapFS{}
	for _, tree := range append([]fs.FS{jobs}, extra...) {
		names, err := fs.Glob(tree, "*")
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			data, err := fs.ReadFile(tree, name)
			if err != nil {
				t.Fatal(err)
			}
			merged[name] = &fstest.MapFile{Data: data, Mode: 0o755}
		}
	}
	return map[string]fs.FS{".": os.DirFS(filepath.Join(Root(t), "runbooks")), "jobs": merged}
}
