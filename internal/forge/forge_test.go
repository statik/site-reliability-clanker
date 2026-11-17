package forge

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

const repo = "platform-live"

// seeded returns a forge with one repository that has three files on main.
func seeded(t *testing.T) (*Forge, *sql.DB) {
	t.Helper()
	db := storetest.Open(t)
	f := New(db)
	clock := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	f.Now = func() time.Time {
		clock = clock.Add(time.Minute)
		return clock
	}
	source := t.TempDir()
	write(t, filepath.Join(source, "app", "terraform.tfvars"), "version  = \"1.0.0\"\nreplicas = 1\nregion   = \"local\"\n")
	write(t, filepath.Join(source, "app", "main.tf"), "module \"app\" {}\n")
	write(t, filepath.Join(source, "run.sh"), "#!/bin/sh\necho run\n")
	if err := os.Chmod(filepath.Join(source, "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(source, ".terraform", "provider.bin"), "not for the repository")
	write(t, filepath.Join(source, "app", "terraform.tfstate"), "{}")
	if err := f.Seed(repo, map[string]string{"platform": source}, "First commit"); err != nil {
		t.Fatal(err)
	}
	return f, db
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, f *Forge, branch string, files map[string]string) string {
	t.Helper()
	hash, err := f.Commit(CommitRequest{Repo: repo, Branch: branch, Base: "main", Author: "alice",
		Message: "Change on " + branch, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func read(t *testing.T, f *Forge, ref, path string) string {
	t.Helper()
	content, err := f.ReadFile(repo, ref, path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

const tfvars = "platform/app/terraform.tfvars"

func TestSeedStoresTheFilesAndSkipsStateAndProviders(t *testing.T) {
	f, _ := seeded(t)
	if !f.Exists(repo) || f.Exists("other") {
		t.Fatal("Exists is wrong")
	}
	repos, err := f.Repos()
	if err != nil || len(repos) != 1 || repos[0] != repo {
		t.Fatalf("Repos = %v, %v", repos, err)
	}
	if got := read(t, f, "main", "platform/app/main.tf"); got != "module \"app\" {}\n" {
		t.Fatalf("main.tf = %q", got)
	}
	for _, path := range []string{"platform/.terraform/provider.bin", "platform/app/terraform.tfstate"} {
		if _, err := f.ReadFile(repo, "main", path); err == nil {
			t.Errorf("%s is in the repository", path)
		}
	}
	entries, err := f.Tree(repo, "main", "platform")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name != "app" || entries[0].Type != "tree" ||
		entries[1].Path != "platform/run.sh" || entries[1].Type != "blob" {
		t.Fatalf("Tree = %+v", entries)
	}
	branches, err := f.Branches(repo)
	if err != nil || len(branches) != 1 || branches[0] != "main" {
		t.Fatalf("Branches = %v, %v", branches, err)
	}
}

func TestSeedDoesNothingForARepositoryThatExists(t *testing.T) {
	f, _ := seeded(t)
	before, _ := f.Head(repo, "main")
	other := t.TempDir()
	write(t, filepath.Join(other, "new.txt"), "new")
	if err := f.Seed(repo, map[string]string{"": other}, "Second seed"); err != nil {
		t.Fatal(err)
	}
	if after, _ := f.Head(repo, "main"); after != before {
		t.Fatal("the second seed changed main")
	}
}

func TestTheRepositoryIsStillThereForANewForge(t *testing.T) {
	f, db := seeded(t)
	head := commit(t, f, "feature", map[string]string{"notes.txt": "kept\n"})
	again := New(db)
	if got, err := again.Head(repo, "feature"); err != nil || got != head {
		t.Fatalf("Head = %q, %v; want %q", got, err, head)
	}
	if content, err := again.ReadFile(repo, "feature", "notes.txt"); err != nil || content != "kept\n" {
		t.Fatalf("notes.txt = %q, %v", content, err)
	}
}

func TestCommitStartsABranchAndThenAddsToIt(t *testing.T) {
	f, _ := seeded(t)
	main, _ := f.Head(repo, "main")
	first := commit(t, f, "feature", map[string]string{"a.txt": "one\n"})
	second := commit(t, f, "feature", map[string]string{"a.txt": "two\n", "/../b.txt": "b\n"})
	if head, _ := f.Head(repo, "feature"); head != second {
		t.Fatalf("branch head = %s, want %s", head, second)
	}
	if now, _ := f.Head(repo, "main"); now != main {
		t.Fatal("a commit on a branch moved main")
	}
	if got := read(t, f, "feature", "a.txt"); got != "two\n" {
		t.Fatalf("a.txt = %q", got)
	}
	if got := read(t, f, "feature", "b.txt"); got != "b\n" {
		t.Fatalf("a path that steps out of the tree was not cleaned: %q", got)
	}
	log, err := f.Log(repo, "feature", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 3 || log[0].Commit != second || log[1].Commit != first || log[2].Commit != main ||
		log[0].Author != "alice" || log[0].Subject != "Change on feature" {
		t.Fatalf("Log = %+v", log)
	}
	if short, _ := f.Log(repo, "feature", 2); len(short) != 2 {
		t.Fatalf("Log with limit 2 gave %d commits", len(short))
	}
}

func TestCommitRefusesAChangeThatChangesNothing(t *testing.T) {
	f, _ := seeded(t)
	same := read(t, f, "main", tfvars)
	cases := map[string]CommitRequest{
		"no files":     {Repo: repo, Branch: "x", Base: "main", Author: "a", Message: "m"},
		"same content": {Repo: repo, Branch: "x", Base: "main", Author: "a", Message: "m", Files: map[string]string{tfvars: same}},
		"missing file": {Repo: repo, Branch: "x", Base: "main", Author: "a", Message: "m", Deletes: []string{"none.txt"}},
		"unknown base": {Repo: repo, Branch: "x", Base: "no-such-branch", Author: "a", Message: "m", Files: map[string]string{"a": "b"}},
		"unknown repo": {Repo: "nope", Branch: "x", Base: "main", Author: "a", Message: "m", Files: map[string]string{"a": "b"}},
	}
	for name, request := range cases {
		if _, err := f.Commit(request); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if branches, _ := f.Branches(repo); len(branches) != 1 {
		t.Fatalf("a refused commit made a branch: %v", branches)
	}
}

func TestCommitDeletesAFile(t *testing.T) {
	f, _ := seeded(t)
	if _, err := f.Commit(CommitRequest{Repo: repo, Branch: "cleanup", Base: "main", Author: "a",
		Message: "Remove the script", Deletes: []string{"platform/run.sh"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ReadFile(repo, "cleanup", "platform/run.sh"); err == nil {
		t.Fatal("the file is still on the branch")
	}
	paths, err := f.ChangedPaths(repo, "main", "cleanup")
	if err != nil || len(paths) != 1 || paths[0] != "platform/run.sh" {
		t.Fatalf("ChangedPaths = %v, %v", paths, err)
	}
}

func TestMergeCombinesChangesToDifferentLinesOfOneFile(t *testing.T) {
	f, _ := seeded(t)
	commit(t, f, "version", map[string]string{tfvars: "version  = \"1.1.0\"\nreplicas = 1\nregion   = \"local\"\n"})
	commit(t, f, "replicas", map[string]string{tfvars: "version  = \"1.0.0\"\nreplicas = 1\nregion   = \"local\"\nextra    = 2\n"})
	if _, err := f.Merge(repo, "version", "bob", "Merge the version"); err != nil {
		t.Fatal(err)
	}
	merged, err := f.Merge(repo, "replicas", "bob", "Merge the replicas")
	if err != nil {
		t.Fatal(err)
	}
	want := "version  = \"1.1.0\"\nreplicas = 1\nregion   = \"local\"\nextra    = 2\n"
	if got := read(t, f, "main", tfvars); got != want {
		t.Fatalf("merged file =\n%s\nwant\n%s", got, want)
	}
	if head, _ := f.Head(repo, "main"); head != merged {
		t.Fatalf("main = %s, want the merge commit %s", head, merged)
	}
	log, _ := f.Log(repo, "main", 1)
	if log[0].Subject != "Merge the replicas" || log[0].Author != "bob" {
		t.Fatalf("merge commit = %+v", log[0])
	}
}

func TestMergeRefusesAConflictAndLeavesMain(t *testing.T) {
	f, _ := seeded(t)
	commit(t, f, "one", map[string]string{tfvars: "version  = \"1.1.0\"\nreplicas = 1\nregion   = \"local\"\n"})
	commit(t, f, "two", map[string]string{tfvars: "version  = \"2.0.0\"\nreplicas = 1\nregion   = \"local\"\n"})
	if _, err := f.Merge(repo, "one", "bob", "Merge one"); err != nil {
		t.Fatal(err)
	}
	before, _ := f.Head(repo, "main")
	_, err := f.Merge(repo, "two", "bob", "Merge two")
	var conflict *ConflictError
	if !errors.As(err, &conflict) || len(conflict.Paths) != 1 || conflict.Paths[0] != tfvars {
		t.Fatalf("error = %v, want a conflict on %s", err, tfvars)
	}
	if after, _ := f.Head(repo, "main"); after != before {
		t.Fatal("a refused merge moved main")
	}
}

func TestMergeOfABranchThatIsMergedAlreadyDoesNothing(t *testing.T) {
	f, _ := seeded(t)
	commit(t, f, "feature", map[string]string{"a.txt": "a\n"})
	first, err := f.Merge(repo, "feature", "bob", "Merge")
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.Merge(repo, "feature", "bob", "Merge again")
	if err != nil || second != first {
		t.Fatalf("second merge = %s, %v; want %s and no error", second, err, first)
	}
}

func TestMergeKeepsADeletionAndAnAddition(t *testing.T) {
	f, _ := seeded(t)
	if _, err := f.Commit(CommitRequest{Repo: repo, Branch: "remove", Base: "main", Author: "a",
		Message: "Remove", Deletes: []string{"platform/run.sh"}}); err != nil {
		t.Fatal(err)
	}
	commit(t, f, "add", map[string]string{"platform/new.tf": "resource {}\n"})
	for _, branch := range []string{"remove", "add"} {
		if _, err := f.Merge(repo, branch, "bob", "Merge "+branch); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.ReadFile(repo, "main", "platform/run.sh"); err == nil {
		t.Fatal("the deleted file came back")
	}
	if got := read(t, f, "main", "platform/new.tf"); got != "resource {}\n" {
		t.Fatalf("new.tf = %q", got)
	}
	if got := read(t, f, "main", "platform/app/main.tf"); got == "" {
		t.Fatal("a file that nobody changed is gone")
	}
}

func TestMergeRefusesAChangeToAFileThatTheOtherSideDeleted(t *testing.T) {
	f, _ := seeded(t)
	if _, err := f.Commit(CommitRequest{Repo: repo, Branch: "remove", Base: "main", Author: "a",
		Message: "Remove", Deletes: []string{"platform/run.sh"}}); err != nil {
		t.Fatal(err)
	}
	commit(t, f, "edit", map[string]string{"platform/run.sh": "#!/bin/sh\necho changed\n"})
	if _, err := f.Merge(repo, "remove", "bob", "Merge remove"); err != nil {
		t.Fatal(err)
	}
	var conflict *ConflictError
	if _, err := f.Merge(repo, "edit", "bob", "Merge edit"); !errors.As(err, &conflict) {
		t.Fatalf("error = %v, want a conflict", err)
	}
}

func TestUpdateBranchBringsMainIntoTheBranch(t *testing.T) {
	f, _ := seeded(t)
	feature := commit(t, f, "feature", map[string]string{tfvars: "version  = \"1.0.0\"\nreplicas = 1\nregion   = \"cloud\"\n"})
	if head, err := f.UpdateBranch(repo, "feature", "clanker"); err != nil || head != feature {
		t.Fatalf("an up-to-date branch changed: %s, %v", head, err)
	}
	commit(t, f, "other", map[string]string{tfvars: "version  = \"1.1.0\"\nreplicas = 1\nregion   = \"local\"\n"})
	if _, err := f.Merge(repo, "other", "bob", "Merge other"); err != nil {
		t.Fatal(err)
	}
	updated, err := f.UpdateBranch(repo, "feature", "clanker")
	if err != nil {
		t.Fatal(err)
	}
	if updated == feature {
		t.Fatal("the branch did not move")
	}
	want := "version  = \"1.1.0\"\nreplicas = 1\nregion   = \"cloud\"\n"
	if got := read(t, f, "feature", tfvars); got != want {
		t.Fatalf("branch file =\n%s\nwant\n%s", got, want)
	}
	// The pull request shows only what the branch adds to main.
	paths, err := f.ChangedPaths(repo, "main", "feature")
	if err != nil || len(paths) != 1 || paths[0] != tfvars {
		t.Fatalf("ChangedPaths = %v, %v", paths, err)
	}
	diff, err := f.Diff(repo, "main", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-region   = \"local\"") || !strings.Contains(diff, "+region   = \"cloud\"") ||
		strings.Contains(diff, "1.0.0") {
		t.Fatalf("the diff is not only the change of the branch:\n%s", diff)
	}
}

func TestUpdateBranchMovesABranchWithNoOwnCommitsForward(t *testing.T) {
	f, _ := seeded(t)
	s, err := f.storage(repo)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := f.Head(repo, "main")
	// A branch at the old main with no commit of its own.
	if err := moveBranch(s, "stale", plumbing.NewHash(old), plumbing.ZeroHash); err != nil {
		t.Fatal(err)
	}
	commit(t, f, "feature", map[string]string{"a.txt": "a\n"})
	main, err := f.Merge(repo, "feature", "bob", "Merge")
	if err != nil {
		t.Fatal(err)
	}
	head, err := f.UpdateBranch(repo, "stale", "clanker")
	if err != nil || head != main {
		t.Fatalf("UpdateBranch = %s, %v; want the head of main %s with no new commit", head, err, main)
	}
}

func TestUpdateBranchRefusesAConflict(t *testing.T) {
	f, _ := seeded(t)
	feature := commit(t, f, "feature", map[string]string{tfvars: "version  = \"3.0.0\"\nreplicas = 1\nregion   = \"local\"\n"})
	commit(t, f, "other", map[string]string{tfvars: "version  = \"1.1.0\"\nreplicas = 1\nregion   = \"local\"\n"})
	if _, err := f.Merge(repo, "other", "bob", "Merge other"); err != nil {
		t.Fatal(err)
	}
	_, err := f.UpdateBranch(repo, "feature", "clanker")
	if err == nil || !strings.Contains(err.Error(), "conflicts with main") {
		t.Fatalf("error = %v, want a conflict", err)
	}
	if head, _ := f.Head(repo, "feature"); head != feature {
		t.Fatal("a refused update moved the branch")
	}
}

func TestExportWritesTheTreeWithTheExecutableBit(t *testing.T) {
	f, _ := seeded(t)
	head, _ := f.Head(repo, "main")
	dest := filepath.Join(t.TempDir(), "checkout")
	if err := f.Export(repo, head, dest); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dest, "platform", "app", "terraform.tfvars"))
	if err != nil || !strings.Contains(string(content), "replicas = 1") {
		t.Fatalf("tfvars = %q, %v", content, err)
	}
	script, err := os.Stat(filepath.Join(dest, "platform", "run.sh"))
	if err != nil || script.Mode().Perm()&0o100 == 0 {
		t.Fatalf("run.sh is not executable: %v, %v", script, err)
	}
	plain, _ := os.Stat(filepath.Join(dest, "platform", "app", "main.tf"))
	if plain.Mode().Perm()&0o111 != 0 {
		t.Fatal("main.tf is executable")
	}
	if err := f.Export(repo, "0000000000000000000000000000000000000000", t.TempDir()); err == nil {
		t.Fatal("no error for a commit that does not exist")
	}
}

func TestABranchMovesOnlyFromTheCommitThatTheWriterRead(t *testing.T) {
	f, _ := seeded(t)
	first := plumbing.NewHash(commit(t, f, "feature", map[string]string{"a.txt": "one\n"}))
	s, err := f.storage(repo)
	if err != nil {
		t.Fatal(err)
	}
	head, _ := f.Head(repo, "main")
	main := plumbing.NewHash(head)
	// A writer that read main as the head of the branch is too late: the branch is at first.
	if err := moveBranch(s, "feature", main, main); err == nil ||
		!strings.Contains(err.Error(), "moved during the change") {
		t.Fatalf("error = %v, want a refusal", err)
	}
	if now, _ := f.Head(repo, "feature"); now != first.String() {
		t.Fatal("the late writer moved the branch")
	}
	if err := moveBranch(s, "feature", main, plumbing.ZeroHash); err == nil {
		t.Fatal("a branch that exists was created again")
	}
	if err := moveBranch(s, "feature", main, first); err != nil {
		t.Fatalf("the writer with the right old commit was refused: %v", err)
	}
}

func TestArchiveHoldsTheTreeOfACommit(t *testing.T) {
	f, _ := seeded(t)
	head, _ := f.Head(repo, "main")
	raw, err := f.Archive(repo, head)
	if err != nil {
		t.Fatal(err)
	}
	unzipped, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(unzipped)
	modes := map[string]int64{}
	bodies := map[string]string{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(reader)
		modes[header.Name], bodies[header.Name] = header.Mode, string(body)
	}
	if len(modes) != 3 {
		t.Fatalf("the archive has %d files, want 3: %v", len(modes), modes)
	}
	if modes["platform/run.sh"] != 0o755 || modes["platform/app/main.tf"] != 0o644 {
		t.Fatalf("modes = %v", modes)
	}
	if !strings.Contains(bodies[tfvars], "replicas = 1") {
		t.Fatalf("tfvars = %q", bodies[tfvars])
	}
	if _, err := f.Archive(repo, "0000000000000000000000000000000000000000"); err == nil {
		t.Fatal("no error for a commit that does not exist")
	}
}
