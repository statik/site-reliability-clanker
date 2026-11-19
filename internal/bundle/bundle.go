// Package bundle carries the files that the control plane reads from a checkout of the
// repository (platform, runbooks, the application pack and the built dashboard), so that
// one downloaded binary can run without a checkout.
//
// The archive is embedded only in a build with the tag `bundle` (see `make release`). A
// plain `go build` has no archive, and the binary then needs `--source`.
package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	// MapFS is a complete file system in memory. The standard library has no other one.
	"testing/fstest"
)

// archive is the gzip-compressed tar file. embed.go sets it in a build with the tag.
var archive []byte

// Present reports whether this binary carries the files.
func Present() bool { return len(archive) > 0 }

// Extract unpacks the files below dir and returns the directory that stands in for the
// checkout. The directory name contains the hash of the archive, so a new binary gets a
// new directory and a second start of the same binary does nothing.
func Extract(dir string) (string, error) {
	if !Present() {
		return "", errors.New("this binary carries no bundled files: build it with " +
			"`make release`, or set --source to a checkout of the repository")
	}
	return extract(archive, dir)
}

// FS returns the files as a read-only file system in memory. It writes nothing, so it also
// works where the program has no disk.
func FS() (fs.FS, error) {
	if !Present() {
		return nil, errors.New("this binary carries no bundled files: build it with " +
			"`make release`, or set --source to a checkout of the repository")
	}
	return memory(archive)
}

func memory(data []byte) (fs.FS, error) {
	zipped, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open the bundled files: %w", err)
	}
	files := fstest.MapFS{}
	reader := tar.NewReader(zipped)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read the bundled files: %w", err)
		}
		// Other entry types (directories, links, the pax header of git archive) are not
		// needed: a directory exists when a file is below it.
		if header.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(header.Name)
		if !fs.ValidPath(name) {
			return nil, fmt.Errorf("the bundle has an entry outside its directory: %q",
				header.Name)
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("read %s from the bundled files: %w", name, err)
		}
		mode := fs.FileMode(0o644)
		if header.Mode&0o111 != 0 {
			mode = 0o755
		}
		files[name] = &fstest.MapFile{Data: content, Mode: mode}
	}
}

func extract(data []byte, dir string) (string, error) {
	sum := sha256.Sum256(data)
	target := filepath.Join(dir, "source-"+hex.EncodeToString(sum[:6]))
	marker := filepath.Join(target, ".complete")
	if _, err := os.Stat(marker); err == nil {
		return target, nil
	}
	if err := os.RemoveAll(target); err != nil {
		return "", fmt.Errorf("remove incomplete bundle directory %s: %w", target, err)
	}
	zipped, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("open the bundled files: %w", err)
	}
	reader := tar.NewReader(zipped)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read the bundled files: %w", err)
		}
		if err := writeEntry(target, header, reader); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		return "", fmt.Errorf("mark bundle directory %s as complete: %w", target, err)
	}
	return target, nil
}

func writeEntry(target string, header *tar.Header, content io.Reader) error {
	name := filepath.Clean(header.Name)
	if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
		return fmt.Errorf("the bundle has an entry outside its directory: %q", header.Name)
	}
	path := filepath.Join(target, name)
	switch header.Typeflag {
	case tar.TypeDir:
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", path, err)
		}
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
		}
		mode := os.FileMode(0o644)
		if header.Mode&0o111 != 0 {
			mode = 0o755
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return fmt.Errorf("create %s: %w", path, err)
		}
		if _, err := io.Copy(file, content); err != nil {
			file.Close()
			return fmt.Errorf("write %s: %w", path, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close %s: %w", path, err)
		}
	}
	// Other entry types (links, the pax header of git archive) are not needed.
	return nil
}
