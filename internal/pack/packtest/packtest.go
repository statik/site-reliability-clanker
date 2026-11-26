// Package packtest gives the tests of the core the example application pack of this
// repository and a second pack with other names.
package packtest

import (
	"crypto/sha256"
	"encoding/hex"
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

// Archive is the content of the chart archive that AddRecords puts into a pack.
var Archive = []byte("the chart archive of harbor 3.5.0\n")

// AddRecords gives the copy of the second pack in dir a release record for each release:
// 3.4.0 with no chart, so the layer installs deploy.chart, and 3.5.0 with a chart archive
// of the pack and a schema change.
func AddRecords(t testing.TB, dir string) {
	t.Helper()
	sum := sha256.Sum256(Archive)
	files := map[string]string{
		"notes/3.4.0.json": `{"values": {"image": {"tag": "3.4.0"}}, "schema_change": false}`,
		"notes/3.5.0.json": `{"chart": "charts/harbor-3.5.0.tgz", "chart_sha256": "` +
			hex.EncodeToString(sum[:]) + `", "schema_change": true}`,
		"charts/harbor-3.5.0.tgz": string(Archive),
	}
	for name, content := range files {
		file := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// SecondWithRecords returns a copy of the second pack with the records of AddRecords.
func SecondWithRecords(t testing.TB) pack.Pack {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, "harbor"), os.DirFS(filepath.Join(SecondSource(t),
		"harbor"))); err != nil {
		t.Fatal(err)
	}
	AddRecords(t, filepath.Join(root, "harbor"))
	return load(t, root, "harbor")
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
