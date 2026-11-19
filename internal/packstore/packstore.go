// Package packstore brings an application pack from a git repository to this machine and
// reads a pack from a directory. It starts the git program, so it is not a part of the
// core: the operator runs it, as the wizard runs the aws program.
//
// A pack comes from outside. The import refuses a symbolic link and a file that is not a
// plain file, copies no more than a fixed number of files and bytes, and records a hash of
// what it copied. A later start reads the pack only when the hash is still the same.
package packstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/pack"
)

// The limits of one import.
const (
	maxFiles = 5000
	maxBytes = 64 << 20
)

// BuiltIn is the source of the pack that comes with the files of the control plane.
const BuiltIn = "built in"

// lockSuffix is the end of the name of the lock file, beside the directory of a version.
const lockSuffix = ".lock.json"

var (
	tagPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	// An address like git@host:owner/repository, the short form of ssh.
	shortSSH = regexp.MustCompile(`^[A-Za-z0-9_.-]+@[A-Za-z0-9.-]+:[A-Za-z0-9_./-]+$`)
	dirName  = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
)

// Lock records where an imported pack came from and what its files were.
type Lock struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// URL, Tag and Commit name the place in the git repository. Dir is the directory of
	// the pack in that repository.
	URL    string `json:"url"`
	Tag    string `json:"tag"`
	Commit string `json:"commit"`
	Dir    string `json:"dir"`
	// SHA256 is pack.Hash of the files.
	SHA256     string `json:"sha256"`
	ImportedAt string `json:"imported_at"`
}

// Origin says where the pack of a start came from, for the audit log.
type Origin struct {
	// Source is BuiltIn, "directory" for a directory with no lock, or the URL of an import.
	Source, Tag, Commit, SHA256 string
}

// Request is one import.
type Request struct {
	// URL is an https or ssh address, or a directory of this machine with a repository.
	URL string
	// Tag is the tag to import. Dir is the directory of the pack in the repository; empty
	// means the root.
	Tag, Dir string
	// DataDir is the data directory of the control plane. The pack goes below it.
	DataDir string
	// Git runs the git program. Nil means the real program.
	Git func(ctx context.Context, dir string, args ...string) (string, error)
	// Now is the time of the import. Nil means the clock of the machine.
	Now func() time.Time
}

// Result is what an import did.
type Result struct {
	Pack pack.Pack
	Lock Lock
	// Dir is the directory of the imported pack. Existed says that the same files were
	// there from an earlier import, and nothing changed.
	Dir     string
	Existed bool
}

// CheckURL refuses an address that is not https, ssh or a directory of this machine.
func CheckURL(address string) error {
	switch {
	case address == "" || strings.HasPrefix(address, "-"):
		return fmt.Errorf("the address %q is not correct", address)
	case strings.HasPrefix(address, "https://"), strings.HasPrefix(address, "ssh://"):
		// The address goes into the lock file and the audit log, so it must hold no secret.
		if parsed, err := url.Parse(address); err != nil || parsed.Host == "" {
			return fmt.Errorf("the address %q is not correct", address)
		} else if _, secret := parsed.User.Password(); secret {
			return errors.New("the address has a password; remove it and let git read the " +
				"credential from its credential helper")
		}
		return nil
	case shortSSH.MatchString(address):
		return nil
	case strings.Contains(address, "://"):
		return fmt.Errorf("the address %q is not https or ssh; a pack comes from an https or "+
			"ssh address, or from a directory of this machine", address)
	}
	if info, err := os.Stat(address); err != nil || !info.IsDir() {
		return fmt.Errorf("%q is not an https or ssh address and not a directory of this "+
			"machine", address)
	}
	return nil
}

func realGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// The import must not wait for a person, and must not read the settings of a person.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *Request) check() error {
	if err := CheckURL(r.URL); err != nil {
		return err
	}
	if !tagPattern.MatchString(r.Tag) {
		return fmt.Errorf("the tag %q is not correct; give the tag of the version with --tag",
			r.Tag)
	}
	if r.Dir == "" {
		r.Dir = "."
	}
	if r.Dir != "." && (!dirName.MatchString(r.Dir) || strings.Contains(r.Dir, "..")) {
		return fmt.Errorf("the directory %q must be a path inside the repository", r.Dir)
	}
	if r.Git == nil {
		r.Git = realGit
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	return nil
}

// Import clones the tag, checks the pack, and copies it to
// <data dir>/packs/<name>/<version>, with a lock file beside that directory.
func Import(ctx context.Context, r Request) (Result, error) {
	if err := r.check(); err != nil {
		return Result{}, err
	}
	work, err := os.MkdirTemp("", "clanker-pack-")
	if err != nil {
		return Result{}, fmt.Errorf("make a directory for the clone: %w", err)
	}
	defer os.RemoveAll(work)
	commit, err := r.clone(ctx, filepath.Join(work, "clone"))
	if err != nil {
		return Result{}, err
	}
	// The files of the pack go to a clean directory first: the clone also has the data
	// of git, which is not a part of the pack and must not be in its hash.
	source := filepath.Join(work, "pack")
	if err := copyTree(filepath.Join(work, "clone", filepath.FromSlash(r.Dir)), source); err != nil {
		return Result{}, err
	}
	app, err := LoadDir(source)
	if err != nil {
		return Result{}, err
	}
	hash, err := pack.Hash(app.Files())
	if err != nil {
		return Result{}, err
	}
	lock := Lock{Name: app.Name, Version: app.Version, URL: r.URL, Tag: r.Tag, Commit: commit,
		Dir: r.Dir, SHA256: hash, ImportedAt: r.Now().UTC().Format(time.RFC3339)}
	return place(source, r.DataDir, lock)
}

// clone gets the one commit of the tag and returns its hash. The repository is cloned
// with no hooks to run and no submodules.
func (r Request) clone(ctx context.Context, work string) (string, error) {
	if _, err := r.Git(ctx, "", "clone", "--quiet", "--depth", "1", "--no-recurse-submodules",
		"--branch", r.Tag, "--", r.URL, work); err != nil {
		return "", fmt.Errorf("clone the tag %s of %s: %w", r.Tag, r.URL, err)
	}
	head, err := r.Git(ctx, work, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	// --branch also takes the name of a branch. A version must be a tag, which does not move.
	tagged, err := r.Git(ctx, work, "rev-parse", "--verify", "--quiet",
		"refs/tags/"+r.Tag+"^{commit}")
	if err != nil || tagged != head {
		return "", fmt.Errorf("%s is not a tag of %s; a version of a pack is a tag", r.Tag, r.URL)
	}
	return head, nil
}

// place copies the checked files to the directory of the version and writes the lock. A
// version that is there with other files is refused: a version does not change.
func place(source, dataDir string, lock Lock) (Result, error) {
	dest := filepath.Join(dataDir, "packs", lock.Name, lock.Version)
	lockPath := dest + lockSuffix
	if old, err := readLock(lockPath); err == nil {
		if old.SHA256 != lock.SHA256 {
			return Result{}, fmt.Errorf("version %s of the pack %s is on this machine with other "+
				"files (hash %s, from %s tag %s); the new files have the hash %s. A version does "+
				"not change: give the pack a new version", lock.Version, lock.Name, old.SHA256,
				old.URL, old.Tag, lock.SHA256)
		}
		if app, _, err := Open(dest); err == nil {
			return Result{Pack: app, Lock: old, Dir: dest, Existed: true}, nil
		}
		// The copy on this machine changed. The import puts the recorded files back.
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Result{}, err
	}
	if err := os.RemoveAll(dest); err != nil {
		return Result{}, fmt.Errorf("clear %s: %w", dest, err)
	}
	if err := copyTree(source, dest); err != nil {
		return Result{}, err
	}
	raw, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(lockPath, append(raw, '\n'), 0o644); err != nil {
		return Result{}, fmt.Errorf("write the lock file %s: %w", lockPath, err)
	}
	app, _, err := Open(dest)
	return Result{Pack: app, Lock: lock, Dir: dest}, err
}

func readLock(path string) (Lock, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Lock{}, err
	}
	var lock Lock
	if err := json.Unmarshal(raw, &lock); err != nil {
		return Lock{}, fmt.Errorf("%s is not a correct lock file: %w", path, err)
	}
	return lock, nil
}

// plainFiles walks a directory and calls visit for each plain file, with its path below
// the directory. It skips the directory of git. It refuses a symbolic link and any other
// thing that is not a file or a directory: a link can point outside the pack.
func plainFiles(dir string, visit func(relative string, info fs.FileInfo) error) error {
	return filepath.WalkDir(dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, name)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a plain file; a pack cannot hold a link or a device",
				filepath.ToSlash(relative))
		}
		return visit(relative, info)
	})
}

func copyTree(source, dest string) error {
	files, bytes := 0, int64(0)
	return plainFiles(source, func(relative string, info fs.FileInfo) error {
		files++
		bytes += info.Size()
		if files > maxFiles || bytes > maxBytes {
			return fmt.Errorf("the pack is larger than %d files or %d bytes", maxFiles, maxBytes)
		}
		mode := fs.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		return copyFile(filepath.Join(source, relative), filepath.Join(dest, relative), mode)
	})
}

func copyFile(from, to string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy %s: %w", to, err)
	}
	return out.Close()
}

// LoadDir reads and checks the pack in a directory of this machine. The directory must
// hold plain files only.
func LoadDir(dir string) (pack.Pack, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return pack.Pack{}, fmt.Errorf("resolve the directory of the pack: %w", err)
	}
	if info, err := os.Stat(absolute); err != nil || !info.IsDir() {
		return pack.Pack{}, fmt.Errorf("%s is not a directory", dir)
	}
	if err := plainFiles(absolute, func(string, fs.FileInfo) error { return nil }); err != nil {
		return pack.Pack{}, err
	}
	return pack.Load(os.DirFS(absolute), ".")
}

// Open reads the pack in a directory of this machine for a start of the control plane.
// When the directory has a lock file beside it, the files must still have the hash of the
// lock: an imported pack that changed on the disk does not start.
func Open(dir string) (pack.Pack, Origin, error) {
	app, err := LoadDir(dir)
	if err != nil {
		return pack.Pack{}, Origin{}, err
	}
	hash, err := pack.Hash(app.Files())
	if err != nil {
		return pack.Pack{}, Origin{}, err
	}
	absolute, _ := filepath.Abs(dir)
	lock, err := readLock(absolute + lockSuffix)
	if errors.Is(err, fs.ErrNotExist) {
		return app, Origin{Source: "directory", SHA256: hash}, nil
	}
	if err != nil {
		return pack.Pack{}, Origin{}, err
	}
	if lock.SHA256 != hash || lock.Name != app.Name || lock.Version != app.Version {
		return pack.Pack{}, Origin{}, fmt.Errorf("the pack in %s is not the pack that was "+
			"imported: the lock file has %s %s with the hash %s, and the files are %s %s with "+
			"the hash %s. Import the pack again", dir, lock.Name, lock.Version, lock.SHA256,
			app.Name, app.Version, hash)
	}
	return app, Origin{Source: lock.URL, Tag: lock.Tag, Commit: lock.Commit, SHA256: hash}, nil
}
