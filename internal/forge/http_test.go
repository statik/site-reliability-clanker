package forge

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitClient runs the real git program against the forge.
func gitClient(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null", "GIT_AUTHOR_NAME=carol", "GIT_AUTHOR_EMAIL=carol@example.com",
		"GIT_COMMITTER_NAME=carol", "GIT_COMMITTER_EMAIL=carol@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestTheGitClientCanCloneAndPush(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	f, _ := seeded(t)
	commit(t, f, "feature", map[string]string{"notes.txt": "from the forge\n"})
	handler, err := f.HTTPHandler("/git/")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	address := server.URL + "/git/" + repo + ".git"
	work := t.TempDir()

	gitClient(t, work, "clone", "--quiet", address, "checkout")
	checkout := filepath.Join(work, "checkout")
	// The client checks every object that the forge wrote: hashes, trees and commits.
	gitClient(t, checkout, "fsck", "--strict", "--no-dangling")
	content, err := os.ReadFile(filepath.Join(checkout, "platform", "app", "terraform.tfvars"))
	if err != nil || !strings.Contains(string(content), "replicas = 1") {
		t.Fatalf("the clone has the wrong file: %q, %v", content, err)
	}
	if info, err := os.Stat(filepath.Join(checkout, "platform", "run.sh")); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("run.sh is not executable in the clone: %v", err)
	}
	head, _ := f.Head(repo, "main")
	if got := gitClient(t, checkout, "rev-parse", "origin/main"); got != head {
		t.Fatalf("origin/main = %s, the forge says %s", got, head)
	}
	if got := gitClient(t, checkout, "show", "origin/feature:notes.txt"); got != "from the forge" {
		t.Fatalf("the branch of the forge is not in the clone: %q", got)
	}

	// A person makes a branch and pushes it.
	gitClient(t, checkout, "checkout", "--quiet", "-b", "human/replicas")
	tfvarsPath := filepath.Join(checkout, "platform", "app", "terraform.tfvars")
	if err := os.WriteFile(tfvarsPath, []byte(strings.Replace(string(content), "replicas = 1", "replicas = 2", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	gitClient(t, checkout, "commit", "--quiet", "-am", "Run two replicas")
	gitClient(t, checkout, "push", "--quiet", "origin", "human/replicas")
	pushed := gitClient(t, checkout, "rev-parse", "HEAD")
	if got, err := f.Head(repo, "human/replicas"); err != nil || got != pushed {
		t.Fatalf("the forge has %q for the pushed branch (%v), the client has %s", got, err, pushed)
	}
	if got := read(t, f, "human/replicas", tfvars); !strings.Contains(got, "replicas = 2") {
		t.Fatalf("the pushed file is wrong in the forge: %q", got)
	}

	// The forge merges the pushed branch, and the client gets the merge.
	merged, err := f.Merge(repo, "human/replicas", "bob", "Merge the replicas")
	if err != nil {
		t.Fatal(err)
	}
	gitClient(t, checkout, "fetch", "--quiet", "origin")
	if got := gitClient(t, checkout, "rev-parse", "origin/main"); got != merged {
		t.Fatalf("after the fetch origin/main = %s, want the merge commit %s", got, merged)
	}
	gitClient(t, checkout, "fsck", "--strict", "--no-dangling")
	if parents := gitClient(t, checkout, "rev-list", "--parents", "-n", "1", "origin/main"); len(strings.Fields(parents)) != 3 {
		t.Fatalf("the merge commit does not have two parents: %s", parents)
	}

	// A second push to the same branch moves it.
	if err := os.WriteFile(filepath.Join(checkout, "more.txt"), []byte("more\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitClient(t, checkout, "add", "more.txt")
	gitClient(t, checkout, "commit", "--quiet", "-m", "More")
	gitClient(t, checkout, "push", "--quiet", "origin", "human/replicas")
	if got, _ := f.Head(repo, "human/replicas"); got != gitClient(t, checkout, "rev-parse", "HEAD") {
		t.Fatal("the second push did not move the branch")
	}

	// Nobody pushes to main. It changes only when the forge merges a pull request.
	before, _ := f.Head(repo, "main")
	for _, target := range []string{"HEAD:main", ":main"} {
		push := exec.Command("git", "push", "origin", target)
		push.Dir = checkout
		push.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := push.CombinedOutput(); err == nil {
			t.Fatalf("git push origin %s was accepted:\n%s", target, out)
		}
	}
	if after, _ := f.Head(repo, "main"); after != before {
		t.Fatalf("a refused push moved main from %s to %s", before, after)
	}
}

func TestTheHandlerRefusesWhatItDoesNotServe(t *testing.T) {
	f, _ := seeded(t)
	handler, err := f.HTTPHandler("/git/")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]int{
		"/git/" + repo + ".git/info/refs?service=git-upload-pack":    200,
		"/git/" + repo + ".git/info/refs":                            403,
		"/git/" + repo + ".git/info/refs?service=git-upload-archive": 403,
		"/git/missing.git/info/refs?service=git-upload-pack":         404,
		"/git/a/b.git/info/refs?service=git-upload-pack":             404,
		"/git/" + repo + ".git/HEAD":                                 404,
		"/git/" + repo + ".git/git-receive-pack":                     404,
	}
	for path, want := range cases {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if recorder.Code != want {
			t.Errorf("GET %s: status %d, want %d", path, recorder.Code, want)
		}
	}
}
