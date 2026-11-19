package packstore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
)

// git runs the git program in dir for a test, with a fixed identity.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c",
		"user.email=test@example.com", "-c", "commit.gpgsign=false", "-c",
		"tag.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repository returns a git repository with the second pack of the tests in the directory
// packs/harbor, and the tag v0.1.0 on its one commit.
func repository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(packtest.SecondSource(t), packtest.Second(t).Dir())
	if err := os.CopyFS(filepath.Join(dir, "packs", "harbor"), os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "packs", "harbor", "build.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "--quiet", "--initial-branch=main")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "--quiet", "-m", "The pack")
	git(t, dir, "tag", "v0.1.0")
	return dir
}

func importOf(t *testing.T, repo, data, tag string) (Result, error) {
	t.Helper()
	return Import(context.Background(), Request{URL: repo, Tag: tag, Dir: "packs/harbor",
		DataDir: data})
}

func TestImportCopiesTheTagAndRecordsItsHash(t *testing.T) {
	repo, data := repository(t), t.TempDir()
	result, err := importOf(t, repo, data, "v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(data, "packs", "harbor", "0.1.0")
	if result.Dir != want || result.Existed || result.Pack.Name != "harbor" {
		t.Fatalf("result %+v", result)
	}
	lock := result.Lock
	if lock.Commit != git(t, repo, "rev-parse", "HEAD") || lock.Tag != "v0.1.0" ||
		lock.URL != repo || len(lock.SHA256) != 64 || lock.Dir != "packs/harbor" {
		t.Errorf("lock %+v", lock)
	}
	if _, err := os.Stat(filepath.Join(want, ".git")); err == nil {
		t.Error("the import copied the data of git")
	}
	if info, err := os.Stat(filepath.Join(want, "build.sh")); err != nil || info.Mode()&0o111 == 0 {
		t.Errorf("the build program cannot be run after the import: %v", err)
	}
	app, origin, err := Open(want)
	if err != nil || app.Name != "harbor" || origin.Source != repo || origin.Tag != "v0.1.0" ||
		origin.Commit != lock.Commit || origin.SHA256 != lock.SHA256 {
		t.Errorf("Open gives %q from %+v, %v", app.Name, origin, err)
	}
	again, err := importOf(t, repo, data, "v0.1.0")
	if err != nil || !again.Existed || again.Lock != lock {
		t.Errorf("the second import of the same files: %+v, %v", again, err)
	}
}

func TestAPackThatChangedOnTheDiskDoesNotStart(t *testing.T) {
	repo, data := repository(t), t.TempDir()
	result, err := importOf(t, repo, data, "v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(result.Dir, "notes", "3.4.0.md")
	if err := os.WriteFile(note, []byte("# Other text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(result.Dir); err == nil || !strings.Contains(err.Error(), "not the pack") {
		t.Fatalf("a changed pack opened: %v", err)
	}
	// The import puts the recorded files back.
	again, err := importOf(t, repo, data, "v0.1.0")
	if err != nil || again.Existed {
		t.Fatalf("the import after the change: %+v, %v", again, err)
	}
	if _, _, err := Open(result.Dir); err != nil {
		t.Errorf("the pack does not open after the new import: %v", err)
	}
}

func TestAVersionWithOtherFilesIsRefused(t *testing.T) {
	repo, data := repository(t), t.TempDir()
	if _, err := importOf(t, repo, data, "v0.1.0"); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(repo, "packs", "harbor", "notes", "3.4.0.md")
	if err := os.WriteFile(note, []byte("# Other text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "commit", "--quiet", "-am", "Change a note and keep the version")
	git(t, repo, "tag", "v0.1.0-again")
	_, err := importOf(t, repo, data, "v0.1.0-again")
	if err == nil || !strings.Contains(err.Error(), "A version does not change") {
		t.Fatalf("the same version with other files was imported: %v", err)
	}
}

func TestImportRefusesWhatIsNotATagOrNotSafe(t *testing.T) {
	repo, data := repository(t), t.TempDir()
	git(t, repo, "branch", "moving")
	link := filepath.Join(repo, "packs", "harbor", "outside")
	if err := os.Symlink("/etc/hosts", link); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "--quiet", "-m", "A link")
	git(t, repo, "tag", "v0.1.1")
	const harbor = "packs/harbor"
	at := func(tag, dir string) Request { return Request{URL: repo, Tag: tag, Dir: dir} }
	cases := map[string]struct {
		request Request
		want    string
	}{
		"a branch":               {at("moving", harbor), "is not a tag"},
		"a tag that is not":      {at("v9", harbor), "clone the tag"},
		"a tag like a flag":      {Request{URL: repo, Tag: "--upload-pack=x"}, "tag"},
		"an address like a flag": {Request{URL: "--upload-pack=x", Tag: "v0.1.0"}, "address"},
		"a plain http address": {Request{URL: "http://example.com/pack.git", Tag: "v1"},
			"not https or ssh"},
		"an address with a password": {Request{URL: "https://bot:hunter2@example.com/p.git",
			Tag: "v1"}, "has a password"},
		"a helper address":  {Request{URL: "ext::sh -c id", Tag: "v1"}, "not an https or ssh address"},
		"a file address":    {Request{URL: "file://" + repo, Tag: "v0.1.0"}, "not https or ssh"},
		"a directory above": {at("v0.1.0", "../x"), "inside the repository"},
		"no pack there":     {Request{URL: repo, Tag: "v0.1.0", Dir: "packs"}, "manifest"},
		"a symbolic link":   {at("v0.1.1", harbor), "not a plain file"},
	}
	for name, tc := range cases {
		tc.request.DataDir = data
		_, err := Import(context.Background(), tc.request)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want one with %q", name, err, tc.want)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(data, "packs")); len(entries) != 0 {
		t.Errorf("a refused import left %d entries in the data directory", len(entries))
	}
}

func TestADirectoryWithNoLockIsAPackOfTheDisk(t *testing.T) {
	dir := filepath.Join(packtest.SecondSource(t), packtest.Second(t).Dir())
	app, origin, err := Open(dir)
	if err != nil || app.Name != "harbor" || origin.Source != "directory" || origin.Tag != "" ||
		len(origin.SHA256) != 64 {
		t.Errorf("Open gives %q from %+v, %v", app.Name, origin, err)
	}
}
