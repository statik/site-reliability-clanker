// Package forge is the small git forge inside the control plane. Repositories are bare
// directories on disk. The git CLI does all history, diff and merge work. Pull requests are
// rows in the control-plane database.
package forge

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Forge manages the bare repositories under Root.
type Forge struct {
	Root string
	db   *sql.DB
	mu   sync.Mutex
}

// New returns a forge that keeps its repositories under root.
func New(root string, db *sql.DB) (*Forge, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create forge directory %s: %w", root, err)
	}
	return &Forge{Root: root, db: db}, nil
}

func (f *Forge) repoPath(repo string) string { return filepath.Join(f.Root, repo+".git") }

// Exists reports whether the repository is present.
func (f *Forge) Exists(repo string) bool {
	_, err := os.Stat(filepath.Join(f.repoPath(repo), "HEAD"))
	return err == nil
}

// Repos lists the repository names.
func (f *Forge) Repos() ([]string, error) {
	entries, err := os.ReadDir(f.Root)
	if err != nil {
		return nil, fmt.Errorf("list forge directory %s: %w", f.Root, err)
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".git") {
			names = append(names, strings.TrimSuffix(e.Name(), ".git"))
		}
	}
	sort.Strings(names)
	return names, nil
}

func runGit(dir string, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s in %s: %w: %s", strings.Join(args, " "), dir, err,
			strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (f *Forge) git(repo string, args ...string) (string, error) {
	return runGit(f.repoPath(repo), nil, args...)
}

func authorEnv(author string) []string {
	email := author + "@clanker.invalid"
	return []string{
		"GIT_AUTHOR_NAME=" + author, "GIT_AUTHOR_EMAIL=" + email,
		"GIT_COMMITTER_NAME=" + author, "GIT_COMMITTER_EMAIL=" + email,
	}
}

// Seed creates the repository and commits the given source directories into it. Each key of
// sources is a path inside the repository. Each value is a directory on disk to copy.
// Seed does nothing when the repository exists.
func (f *Forge) Seed(repo string, sources map[string]string, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Exists(repo) {
		return nil
	}
	if _, err := runGit(f.Root, nil, "init", "--bare", "--initial-branch=main",
		f.repoPath(repo)); err != nil {
		return err
	}
	if _, err := f.git(repo, "config", "http.receivepack", "true"); err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "clanker-seed-")
	if err != nil {
		return fmt.Errorf("create temporary directory for seed of %s: %w", repo, err)
	}
	defer os.RemoveAll(work)
	if _, err := runGit(work, nil, "init", "--initial-branch=main"); err != nil {
		return err
	}
	for dest, src := range sources {
		if err := copyTree(src, filepath.Join(work, dest)); err != nil {
			return fmt.Errorf("copy %s into repository %s: %w", src, repo, err)
		}
	}
	env := authorEnv("clanker-bootstrap")
	if _, err := runGit(work, env, "add", "-A"); err != nil {
		return err
	}
	if _, err := runGit(work, env, "commit", "-m", message); err != nil {
		return err
	}
	_, err = runGit(work, env, "push", f.repoPath(repo), "main")
	return err
}

var skipNames = map[string]bool{".terraform": true, ".git": true, "node_modules": true}

func copyTree(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipNames[d.Name()] {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if strings.Contains(d.Name(), ".tfstate") || strings.HasSuffix(d.Name(), ".tfplan") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, info.Mode().Perm())
	})
}

// Head returns the commit that ref points to.
func (f *Forge) Head(repo, ref string) (string, error) {
	out, err := f.git(repo, "rev-parse", "--verify", ref+"^{commit}")
	return strings.TrimSpace(out), err
}

// CommitRequest describes a commit to create on a branch.
type CommitRequest struct {
	Repo    string
	Branch  string
	Base    string
	Author  string
	Message string
	Files   map[string]string
	Deletes []string
}

// Commit writes the files on top of Base and moves Branch to the new commit. When Branch
// exists, the commit goes on top of the branch.
func (f *Forge) Commit(req CommitRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(req.Files) == 0 && len(req.Deletes) == 0 {
		return "", fmt.Errorf("commit to %s/%s: no file changes given", req.Repo, req.Branch)
	}
	start := req.Base
	if _, err := f.git(req.Repo, "rev-parse", "--verify", "refs/heads/"+req.Branch); err == nil {
		start = req.Branch
	}
	work, err := os.MkdirTemp("", "clanker-commit-")
	if err != nil {
		return "", fmt.Errorf("create temporary directory for commit: %w", err)
	}
	defer os.RemoveAll(work)
	if _, err := runGit(work, nil, "clone", "--quiet", f.repoPath(req.Repo), "."); err != nil {
		return "", err
	}
	if _, err := runGit(work, nil, "checkout", "--quiet", "-B", req.Branch, start); err != nil {
		return "", err
	}
	for path, content := range req.Files {
		target := filepath.Join(work, filepath.Clean("/"+path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", fmt.Errorf("create directory for %s: %w", path, err)
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", path, err)
		}
	}
	for _, path := range req.Deletes {
		if err := os.Remove(filepath.Join(work, filepath.Clean("/"+path))); err != nil {
			return "", fmt.Errorf("delete %s: %w", path, err)
		}
	}
	env := authorEnv(req.Author)
	if _, err := runGit(work, env, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := runGit(work, env, "commit", "--quiet", "-m", req.Message); err != nil {
		return "", err
	}
	if _, err := runGit(work, env, "push", "--quiet", "origin", req.Branch); err != nil {
		return "", err
	}
	out, err := runGit(work, nil, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// Merge merges branch into main with a merge commit and returns the new head of main.
// It fails when main moved in a way that conflicts with the branch.
func (f *Forge) Merge(repo, branch, author, message string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	work, err := os.MkdirTemp("", "clanker-merge-")
	if err != nil {
		return "", fmt.Errorf("create temporary directory for merge: %w", err)
	}
	defer os.RemoveAll(work)
	if _, err := runGit(work, nil, "clone", "--quiet", f.repoPath(repo), "."); err != nil {
		return "", err
	}
	env := authorEnv(author)
	if _, err := runGit(work, env, "merge", "--quiet", "--no-ff", "-m", message,
		"origin/"+branch); err != nil {
		return "", err
	}
	if _, err := runGit(work, env, "push", "--quiet", "origin", "main"); err != nil {
		return "", err
	}
	out, err := runGit(work, nil, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// Export writes the tree of commit into dest.
func (f *Forge) Export(repo, commit, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("create export directory %s: %w", dest, err)
	}
	archive := exec.Command("git", "archive", "--format=tar", commit)
	archive.Dir = f.repoPath(repo)
	untar := exec.Command("tar", "-x", "-C", dest)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return fmt.Errorf("connect git archive to tar: %w", err)
	}
	untar.Stdin = pipe
	var stderr bytes.Buffer
	archive.Stderr = &stderr
	untar.Stderr = &stderr
	if err := untar.Start(); err != nil {
		return fmt.Errorf("start tar: %w", err)
	}
	if err := archive.Run(); err != nil {
		return fmt.Errorf("git archive %s of %s: %w: %s", commit, repo, err, stderr.String())
	}
	if err := untar.Wait(); err != nil {
		return fmt.Errorf("extract %s of %s: %w: %s", commit, repo, err, stderr.String())
	}
	return nil
}

// Diff returns the unified diff from base to head.
func (f *Forge) Diff(repo, base, head string) (string, error) {
	return f.git(repo, "diff", "--no-color", base+"..."+head)
}

// ChangedPaths lists the files that differ between base and head.
func (f *Forge) ChangedPaths(repo, base, head string) ([]string, error) {
	out, err := f.git(repo, "diff", "--name-only", base+"..."+head)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// ReadFile returns the content of path at ref.
func (f *Forge) ReadFile(repo, ref, path string) (string, error) {
	return f.git(repo, "show", ref+":"+path)
}

// TreeEntry is one row of a directory listing.
type TreeEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

// Tree lists the directory at path and ref.
func (f *Forge) Tree(repo, ref, path string) ([]TreeEntry, error) {
	target := ref
	if path != "" {
		target = ref + ":" + strings.TrimSuffix(path, "/")
	}
	out, err := f.git(repo, "ls-tree", target)
	if err != nil {
		return nil, err
	}
	entries := []TreeEntry{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		meta, name, ok := strings.Cut(line, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) < 2 {
			continue
		}
		entries = append(entries, TreeEntry{Name: name, Type: fields[1],
			Path: strings.TrimPrefix(path+"/"+name, "/")})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "tree"
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

// LogEntry is one commit in a history listing.
type LogEntry struct {
	Commit  string `json:"commit"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

// Log returns the newest commits of ref, at most limit.
func (f *Forge) Log(repo, ref string, limit int) ([]LogEntry, error) {
	out, err := f.git(repo, "log", fmt.Sprintf("-n%d", limit), "--format=%H%x1f%an%x1f%aI%x1f%s",
		ref)
	if err != nil {
		return nil, err
	}
	entries := []LogEntry{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Split(line, "\x1f")
		if len(parts) != 4 {
			continue
		}
		entries = append(entries, LogEntry{Commit: parts[0], Author: parts[1], Date: parts[2],
			Subject: parts[3]})
	}
	return entries, nil
}

// Branches lists the branch names of the repository.
func (f *Forge) Branches(repo string) ([]string, error) {
	out, err := f.git(repo, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// HTTPHandler serves clone and push over HTTP with git http-backend. Mount it at prefix.
func (f *Forge) HTTPHandler(prefix string) (http.Handler, error) {
	execPath, err := runGit(f.Root, nil, "--exec-path")
	if err != nil {
		return nil, err
	}
	backend := filepath.Join(strings.TrimSpace(execPath), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		return nil, fmt.Errorf("find git-http-backend at %s: %w", backend, err)
	}
	return &cgi.Handler{
		Path: backend,
		Root: prefix,
		Env:  []string{"GIT_PROJECT_ROOT=" + f.Root, "GIT_HTTP_EXPORT_ALL=1"},
	}, nil
}
