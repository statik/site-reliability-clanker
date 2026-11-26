// Package forge is the small git forge inside the control plane. The objects and the
// references of each repository are rows in the database of the control plane, and so are
// the pull requests. A git library does the history work in this process, so the forge
// starts no program and keeps no file.
package forge

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage"
)

const mainBranch = "main"

// Forge manages the repositories in the database.
type Forge struct {
	db *sql.DB
	// mu makes each change of a branch one step in this process. The database guards the
	// same step against another process: a branch moves only from the commit that the
	// writer read.
	mu       sync.Mutex
	storages map[string]*sqlStorage
	// Now gives the time of new commits. Tests replace it.
	Now func() time.Time
}

// New returns a forge that keeps its repositories in db.
func New(db *sql.DB) *Forge {
	return &Forge{db: db, storages: map[string]*sqlStorage{}, Now: time.Now}
}

// Exists reports whether the repository is present.
func (f *Forge) Exists(repo string) bool {
	var name string
	return f.db.QueryRow(`SELECT name FROM git_repos WHERE name = $1`, repo).Scan(&name) == nil
}

// Repos lists the repository names.
func (f *Forge) Repos() ([]string, error) {
	rows, err := f.db.Query(`SELECT name FROM git_repos ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list repositories: %w", err)
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read repository name: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// storage returns the storage of a repository that exists.
func (f *Forge) storage(repo string) (*sqlStorage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.storageLocked(repo)
}

func (f *Forge) storageLocked(repo string) (*sqlStorage, error) {
	if s, ok := f.storages[repo]; ok {
		return s, nil
	}
	if !f.Exists(repo) {
		return nil, fmt.Errorf("repository %s does not exist", repo)
	}
	s := newStorage(f.db, repo)
	f.storages[repo] = s
	return s, nil
}

func open(s *sqlStorage) (*git.Repository, error) {
	repository, err := git.Open(s, nil)
	if err != nil {
		return nil, fmt.Errorf("open repository %s: %w", s.repo, err)
	}
	return repository, nil
}

func (f *Forge) signature(author string) object.Signature {
	return object.Signature{Name: author, Email: author + "@clanker.invalid", When: f.Now()}
}

func branchRef(branch string) plumbing.ReferenceName {
	return plumbing.NewBranchReferenceName(branch)
}

// commitAt resolves a branch name, a reference or a commit hash to a commit.
func commitAt(repository *git.Repository, repo, ref string) (*object.Commit, error) {
	hash, err := repository.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, fmt.Errorf("find %s in %s: %w", ref, repo, err)
	}
	commit, err := repository.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("read commit %s of %s: %w", ref, repo, err)
	}
	return commit, nil
}

func filesAt(commit *object.Commit) (map[string]file, error) {
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("read tree of commit %s: %w", commit.Hash, err)
	}
	return flatten(tree)
}

func (s *sqlStorage) writeBlob(content []byte) (plumbing.Hash, error) {
	return s.SetEncodedObject(memoryObject(plumbing.BlobObject, content))
}

func (s *sqlStorage) readBlob(hash plumbing.Hash) ([]byte, error) {
	encoded, err := s.EncodedObject(plumbing.BlobObject, hash)
	if err != nil {
		return nil, err
	}
	reader, err := encoded.Reader()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// writeCommit stores a commit of the files and returns its hash.
func (f *Forge) writeCommit(s *sqlStorage, files map[string]file, author, message string,
	parents ...plumbing.Hash) (plumbing.Hash, error) {
	tree, err := s.writeTree(files)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	signature := f.signature(author)
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}
	commit := &object.Commit{Author: signature, Committer: signature, Message: message,
		TreeHash: tree, ParentHashes: parents}
	encoded := s.NewEncodedObject()
	if err := commit.Encode(encoded); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("encode commit: %w", err)
	}
	return s.SetEncodedObject(encoded)
}

// moveBranch points the branch at the new commit, only if it still points at old. A zero
// old means that the branch must not exist yet.
func moveBranch(s *sqlStorage, branch string, updated, old plumbing.Hash) error {
	name := branchRef(branch)
	if old.IsZero() {
		if _, err := s.Reference(name); err == nil {
			return fmt.Errorf("branch %s of %s exists already", branch, s.repo)
		}
		return s.SetReference(plumbing.NewHashReference(name, updated))
	}
	err := s.CheckAndSetReference(plumbing.NewHashReference(name, updated),
		plumbing.NewHashReference(name, old))
	if errors.Is(err, storage.ErrReferenceHasChanged) {
		return fmt.Errorf("branch %s of %s moved during the change: try again", branch, s.repo)
	}
	return err
}

var skipNames = map[string]bool{".terraform": true, ".git": true, "node_modules": true}

// seedTree is the content of a repository that Seed makes, with the source of each path.
type seedTree struct {
	files map[string]file
	// owners holds the source of each file, and dirs the source of each directory that
	// has a file.
	owners, dirs map[string]string
}

// readTree stores the files of source as blobs and adds them to the tree below prefix.
func readTree(s *sqlStorage, source fs.FS, prefix string, tree seedTree) error {
	return fs.WalkDir(source, ".", func(relative string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipNames[d.Name()] {
			return fs.SkipDir
		}
		if d.IsDir() || strings.Contains(d.Name(), ".tfstate") || strings.HasSuffix(d.Name(), ".tfplan") {
			return nil
		}
		target := path.Join(prefix, relative)
		if err := tree.claim(target, prefix); err != nil {
			return err
		}
		stored, err := storeFile(s, source, relative, d)
		if err != nil {
			return err
		}
		tree.files[target] = stored
		return nil
	})
}

// storeFile stores one file of source as a blob and returns its entry in a tree.
func storeFile(s *sqlStorage, source fs.FS, relative string, d fs.DirEntry) (file, error) {
	content, err := fs.ReadFile(source, relative)
	if err != nil {
		return file{}, err
	}
	info, err := d.Info()
	if err != nil {
		return file{}, err
	}
	hash, err := s.writeBlob(content)
	if err != nil {
		return file{}, err
	}
	mode := filemode.Regular
	if info.Mode().Perm()&0o111 != 0 {
		mode = filemode.Executable
	}
	return file{Mode: mode, Hash: hash}, nil
}

// claim records that the source prefix writes the file target. One source must never
// replace a file of another, so claim refuses a file that the tree has already, a file at
// a directory of the tree, and a file at a directory above target.
func (t seedTree) claim(target, prefix string) error {
	if owner, taken := t.owners[target]; taken {
		return fmt.Errorf("the sources %q and %q both write %s", owner, prefix, target)
	}
	if owner, taken := t.dirs[target]; taken {
		return fmt.Errorf("the source %q writes files inside %s, and %q writes the file %s",
			owner, target, prefix, target)
	}
	for dir := path.Dir(target); dir != "."; dir = path.Dir(dir) {
		if owner, taken := t.owners[dir]; taken {
			return fmt.Errorf("the source %q writes the file %s, and %q writes %s inside it",
				owner, dir, prefix, target)
		}
	}
	t.owners[target] = prefix
	for dir := path.Dir(target); dir != "."; dir = path.Dir(dir) {
		t.dirs[dir] = prefix
	}
	return nil
}

// Seed creates the repository and commits the given source trees into it. Each key of
// sources is a path inside the repository. Each value is the tree of files to copy there.
// Two sources can share a directory, but never a file: Seed refuses a path that two
// sources write. Seed does nothing when the repository exists.
func (f *Forge) Seed(repo string, sources map[string]fs.FS, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Exists(repo) {
		return nil
	}
	// The repository becomes visible only with its first commit: the row in git_repos is
	// the last write.
	s := newStorage(f.db, repo)
	tree := seedTree{files: map[string]file{}, owners: map[string]string{},
		dirs: map[string]string{}}
	for _, dest := range slices.Sorted(maps.Keys(sources)) {
		if err := readTree(s, sources[dest], dest, tree); err != nil {
			return fmt.Errorf("copy the files for %q into repository %s: %w", dest, repo, err)
		}
	}
	commit, err := f.writeCommit(s, tree.files, "clanker-bootstrap", message)
	if err != nil {
		return fmt.Errorf("seed repository %s: %w", repo, err)
	}
	if err := s.SetReference(plumbing.NewHashReference(branchRef(mainBranch), commit)); err != nil {
		return err
	}
	if err := s.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD,
		branchRef(mainBranch))); err != nil {
		return err
	}
	if _, err := f.db.Exec(`INSERT INTO git_repos (name) VALUES ($1) ON CONFLICT (name) DO NOTHING`,
		repo); err != nil {
		return fmt.Errorf("record repository %s: %w", repo, err)
	}
	f.storages[repo] = s
	return nil
}

// Head returns the commit that ref points to.
func (f *Forge) Head(repo, ref string) (string, error) {
	s, err := f.storage(repo)
	if err != nil {
		return "", err
	}
	repository, err := open(s)
	if err != nil {
		return "", err
	}
	commit, err := commitAt(repository, repo, ref)
	if err != nil {
		return "", err
	}
	return commit.Hash.String(), nil
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

// cleanPath makes a path of a request safe: no leading slash and no step out of the tree.
func cleanPath(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+path)), "/")
}

// Commit writes the files on top of Base and moves Branch to the new commit. When Branch
// exists, the commit goes on top of the branch.
func (f *Forge) Commit(req CommitRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(req.Files) == 0 && len(req.Deletes) == 0 {
		return "", fmt.Errorf("commit to %s/%s: no file changes given", req.Repo, req.Branch)
	}
	s, err := f.storageLocked(req.Repo)
	if err != nil {
		return "", err
	}
	repository, err := open(s)
	if err != nil {
		return "", err
	}
	start, old := req.Base, plumbing.ZeroHash
	if reference, err := s.Reference(branchRef(req.Branch)); err == nil {
		start, old = reference.Hash().String(), reference.Hash()
	}
	parent, err := commitAt(repository, req.Repo, start)
	if err != nil {
		return "", err
	}
	files, err := filesAt(parent)
	if err != nil {
		return "", err
	}
	before := len(files)
	changed := false
	for path, content := range req.Files {
		hash, err := s.writeBlob([]byte(content))
		if err != nil {
			return "", err
		}
		path = cleanPath(path)
		mode := filemode.Regular
		if existing, ok := files[path]; ok {
			mode = existing.Mode
			changed = changed || existing.Hash != hash
		}
		files[path] = file{Mode: mode, Hash: hash}
	}
	for _, path := range req.Deletes {
		path = cleanPath(path)
		if _, ok := files[path]; !ok {
			return "", fmt.Errorf("delete %s: the file is not in %s/%s", path, req.Repo, start)
		}
		delete(files, path)
	}
	if !changed && len(files) == before {
		return "", fmt.Errorf("commit to %s/%s: the files are the same as in %s", req.Repo,
			req.Branch, start)
	}
	commit, err := f.writeCommit(s, files, req.Author, req.Message, parent.Hash)
	if err != nil {
		return "", err
	}
	if err := moveBranch(s, req.Branch, commit, old); err != nil {
		return "", err
	}
	return commit.String(), nil
}

// mergeCommits merges theirs into ours and returns the files of the result.
func mergeCommits(s *sqlStorage, ours, theirs *object.Commit) (map[string]file, error) {
	bases, err := ours.MergeBase(theirs)
	if err != nil {
		return nil, fmt.Errorf("find the common commit of %s and %s: %w", ours.Hash, theirs.Hash, err)
	}
	if len(bases) == 0 {
		return nil, fmt.Errorf("%s and %s have no common commit", ours.Hash, theirs.Hash)
	}
	baseFiles, err := filesAt(bases[0])
	if err != nil {
		return nil, err
	}
	ourFiles, err := filesAt(ours)
	if err != nil {
		return nil, err
	}
	theirFiles, err := filesAt(theirs)
	if err != nil {
		return nil, err
	}
	return mergeFiles(baseFiles, ourFiles, theirFiles, func(b, o, t file) (file, bool, error) {
		if o.Mode != t.Mode || !o.Mode.IsFile() {
			return file{}, false, nil
		}
		texts := make([]string, 0, 3)
		for _, side := range []file{b, o, t} {
			content, err := s.readBlob(side.Hash)
			if err != nil {
				return file{}, false, err
			}
			texts = append(texts, string(content))
		}
		merged, clean := mergeLines(texts[0], texts[1], texts[2])
		if !clean {
			return file{}, false, nil
		}
		hash, err := s.writeBlob([]byte(merged))
		return file{Mode: o.Mode, Hash: hash}, err == nil, err
	})
}

// Merge merges branch into main with a merge commit and returns the new head of main.
// It fails when main moved in a way that conflicts with the branch.
func (f *Forge) Merge(repo, branch, author, message string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.storageLocked(repo)
	if err != nil {
		return "", err
	}
	repository, err := open(s)
	if err != nil {
		return "", err
	}
	ours, err := commitAt(repository, repo, mainBranch)
	if err != nil {
		return "", err
	}
	theirs, err := commitAt(repository, repo, branch)
	if err != nil {
		return "", err
	}
	if merged, err := theirs.IsAncestor(ours); err != nil {
		return "", fmt.Errorf("compare %s with main: %w", branch, err)
	} else if merged {
		return ours.Hash.String(), nil
	}
	files, err := mergeCommits(s, ours, theirs)
	if err != nil {
		return "", fmt.Errorf("merge %s into main of %s: %w", branch, repo, err)
	}
	commit, err := f.writeCommit(s, files, author, message, ours.Hash, theirs.Hash)
	if err != nil {
		return "", err
	}
	if err := moveBranch(s, mainBranch, commit, ours.Hash); err != nil {
		return "", err
	}
	return commit.String(), nil
}

// UpdateBranch merges main into branch, so that a plan of the branch includes every change
// that merged after the branch started. It returns the new head of the branch. It fails on
// a conflict.
func (f *Forge) UpdateBranch(repo, branch, author string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.storageLocked(repo)
	if err != nil {
		return "", err
	}
	repository, err := open(s)
	if err != nil {
		return "", err
	}
	ours, err := commitAt(repository, repo, branch)
	if err != nil {
		return "", err
	}
	theirs, err := commitAt(repository, repo, mainBranch)
	if err != nil {
		return "", err
	}
	if contained, err := theirs.IsAncestor(ours); err != nil {
		return "", fmt.Errorf("compare %s with main: %w", branch, err)
	} else if contained {
		return ours.Hash.String(), nil
	}
	if behind, err := ours.IsAncestor(theirs); err != nil {
		return "", fmt.Errorf("compare %s with main: %w", branch, err)
	} else if behind {
		// The branch has nothing of its own: it only moves forward to main.
		if err := moveBranch(s, branch, theirs.Hash, ours.Hash); err != nil {
			return "", err
		}
		return theirs.Hash.String(), nil
	}
	files, err := mergeCommits(s, ours, theirs)
	if err != nil {
		return "", fmt.Errorf("branch %s conflicts with main: %w", branch, err)
	}
	commit, err := f.writeCommit(s, files, author, "Merge main into "+branch, ours.Hash, theirs.Hash)
	if err != nil {
		return "", err
	}
	if err := moveBranch(s, branch, commit, ours.Hash); err != nil {
		return "", err
	}
	return commit.String(), nil
}

// Archive returns the tree of commit as a gzip-compressed tar file. A runner unpacks it
// to work on the commit.
func (f *Forge) Archive(repo, commit string) ([]byte, error) {
	s, err := f.storage(repo)
	if err != nil {
		return nil, err
	}
	repository, err := open(s)
	if err != nil {
		return nil, err
	}
	at, err := commitAt(repository, repo, commit)
	if err != nil {
		return nil, err
	}
	files, err := filesAt(at)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var buffer bytes.Buffer
	zipped := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(zipped)
	for _, path := range paths {
		entry := files[path]
		if entry.Mode == filemode.Submodule {
			continue
		}
		content, err := s.readBlob(entry.Hash)
		if err != nil {
			return nil, fmt.Errorf("read %s of %s: %w", path, repo, err)
		}
		header := &tar.Header{Name: path, Mode: 0o644, Size: int64(len(content)),
			Typeflag: tar.TypeReg, ModTime: at.Committer.When}
		switch entry.Mode {
		case filemode.Executable:
			header.Mode = 0o755
		case filemode.Symlink:
			header.Typeflag, header.Linkname, header.Size = tar.TypeSymlink, string(content), 0
			content = nil
		}
		if err := archive.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("archive %s of %s: %w", path, repo, err)
		}
		if _, err := archive.Write(content); err != nil {
			return nil, fmt.Errorf("archive %s of %s: %w", path, repo, err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("finish the archive of %s: %w", repo, err)
	}
	if err := zipped.Close(); err != nil {
		return nil, fmt.Errorf("finish the archive of %s: %w", repo, err)
	}
	return buffer.Bytes(), nil
}

// trees returns the tree at the common commit of base and head, and the tree at head. This
// is the comparison that a pull request shows: what head adds since it left base.
func (f *Forge) trees(repo, base, head string) (*object.Tree, *object.Tree, error) {
	s, err := f.storage(repo)
	if err != nil {
		return nil, nil, err
	}
	repository, err := open(s)
	if err != nil {
		return nil, nil, err
	}
	baseCommit, err := commitAt(repository, repo, base)
	if err != nil {
		return nil, nil, err
	}
	headCommit, err := commitAt(repository, repo, head)
	if err != nil {
		return nil, nil, err
	}
	common, err := baseCommit.MergeBase(headCommit)
	if err != nil {
		return nil, nil, fmt.Errorf("find the common commit of %s and %s: %w", base, head, err)
	}
	if len(common) == 0 {
		return nil, nil, fmt.Errorf("%s and %s have no common commit in %s", base, head, repo)
	}
	from, err := common[0].Tree()
	if err != nil {
		return nil, nil, err
	}
	to, err := headCommit.Tree()
	if err != nil {
		return nil, nil, err
	}
	return from, to, nil
}

// Diff returns the unified diff from base to head.
func (f *Forge) Diff(repo, base, head string) (string, error) {
	from, to, err := f.trees(repo, base, head)
	if err != nil {
		return "", err
	}
	patch, err := from.Patch(to)
	if err != nil {
		return "", fmt.Errorf("compare %s with %s in %s: %w", base, head, repo, err)
	}
	return patch.String(), nil
}

// ChangedPaths lists the files that differ between base and head.
func (f *Forge) ChangedPaths(repo, base, head string) ([]string, error) {
	from, to, err := f.trees(repo, base, head)
	if err != nil {
		return nil, err
	}
	changes, err := object.DiffTree(from, to)
	if err != nil {
		return nil, fmt.Errorf("compare %s with %s in %s: %w", base, head, repo, err)
	}
	seen := map[string]bool{}
	for _, change := range changes {
		for _, name := range []string{change.From.Name, change.To.Name} {
			if name != "" {
				seen[name] = true
			}
		}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

// ErrFileNotFound is the cause of the error of ReadFile when the repository and the ref
// exist and the commit has no file at the path.
var ErrFileNotFound = object.ErrFileNotFound

// ErrDirectoryNotFound is the cause of the error of Tree when the repository and the ref
// exist and the commit has no directory at the path.
var ErrDirectoryNotFound = object.ErrDirectoryNotFound

// ReadFile returns the content of path at ref.
func (f *Forge) ReadFile(repo, ref, path string) (string, error) {
	s, err := f.storage(repo)
	if err != nil {
		return "", err
	}
	repository, err := open(s)
	if err != nil {
		return "", err
	}
	commit, err := commitAt(repository, repo, ref)
	if err != nil {
		return "", err
	}
	found, err := commit.File(cleanPath(path))
	if err != nil {
		return "", fmt.Errorf("read %s at %s in %s: %w", path, ref, repo, err)
	}
	return found.Contents()
}

// TreeEntry is one row of a directory listing.
type TreeEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

// Tree lists the directory at path and ref.
func (f *Forge) Tree(repo, ref, path string) ([]TreeEntry, error) {
	s, err := f.storage(repo)
	if err != nil {
		return nil, err
	}
	repository, err := open(s)
	if err != nil {
		return nil, err
	}
	commit, err := commitAt(repository, repo, ref)
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	path = strings.Trim(path, "/")
	if path != "" {
		if tree, err = tree.Tree(path); err != nil {
			return nil, fmt.Errorf("list %s at %s in %s: %w", path, ref, repo, err)
		}
	}
	entries := []TreeEntry{}
	for _, entry := range tree.Entries {
		kind := "blob"
		switch entry.Mode {
		case filemode.Dir:
			kind = "tree"
		case filemode.Submodule:
			kind = "commit"
		}
		entries = append(entries, TreeEntry{Name: entry.Name, Type: kind,
			Path: strings.TrimPrefix(path+"/"+entry.Name, "/")})
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
	s, err := f.storage(repo)
	if err != nil {
		return nil, err
	}
	repository, err := open(s)
	if err != nil {
		return nil, err
	}
	start, err := commitAt(repository, repo, ref)
	if err != nil {
		return nil, err
	}
	commits, err := repository.Log(&git.LogOptions{From: start.Hash, Order: git.LogOrderCommitterTime})
	if err != nil {
		return nil, fmt.Errorf("read history of %s in %s: %w", ref, repo, err)
	}
	defer commits.Close()
	entries := []LogEntry{}
	for len(entries) < limit {
		commit, err := commits.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read history of %s in %s: %w", ref, repo, err)
		}
		subject, _, _ := strings.Cut(commit.Message, "\n")
		entries = append(entries, LogEntry{Commit: commit.Hash.String(), Author: commit.Author.Name,
			Date: commit.Author.When.Format(time.RFC3339), Subject: subject})
	}
	return entries, nil
}

// Branches lists the branch names of the repository.
func (f *Forge) Branches(repo string) ([]string, error) {
	s, err := f.storage(repo)
	if err != nil {
		return nil, err
	}
	references, err := s.IterReferences()
	if err != nil {
		return nil, err
	}
	names := []string{}
	err = references.ForEach(func(reference *plumbing.Reference) error {
		if reference.Name().IsBranch() {
			names = append(names, reference.Name().Short())
		}
		return nil
	})
	sort.Strings(names)
	return names, err
}
