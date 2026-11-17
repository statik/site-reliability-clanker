package forge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// The git library merges only by fast-forward. This file has the three-way merge that the
// forge needs: first by path, then by line for a file that both sides changed.

// file is one path of a tree: its mode and the hash of its content.
type file struct {
	Mode filemode.FileMode
	Hash plumbing.Hash
}

// ConflictError names the paths that both sides changed in ways that do not combine.
type ConflictError struct{ Paths []string }

func (e *ConflictError) Error() string {
	return "both sides changed " + strings.Join(e.Paths, ", ") + " in ways that conflict"
}

// mergeFiles combines two sets of files that come from the same base. mergeContent merges
// the content of one file that both sides changed.
func mergeFiles(base, ours, theirs map[string]file,
	mergeContent func(base, ours, theirs file) (file, bool, error)) (map[string]file, error) {
	paths := map[string]bool{}
	for _, side := range []map[string]file{base, ours, theirs} {
		for path := range side {
			paths[path] = true
		}
	}
	merged := map[string]file{}
	conflicts := []string{}
	for path := range paths {
		b, inBase := base[path]
		o, inOurs := ours[path]
		t, inTheirs := theirs[path]
		switch {
		case inOurs == inTheirs && o == t:
			// Both sides agree, also when both deleted the file.
			if inOurs {
				merged[path] = o
			}
		case inOurs == inBase && o == b:
			// Only their side changed the file.
			if inTheirs {
				merged[path] = t
			}
		case inTheirs == inBase && t == b:
			// Only our side changed the file.
			if inOurs {
				merged[path] = o
			}
		case inBase && inOurs && inTheirs:
			result, clean, err := mergeContent(b, o, t)
			if err != nil {
				return nil, fmt.Errorf("merge %s: %w", path, err)
			}
			if !clean {
				conflicts = append(conflicts, path)
				continue
			}
			merged[path] = result
		default:
			// One side deleted or added the file and the other side changed it.
			conflicts = append(conflicts, path)
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return nil, &ConflictError{Paths: conflicts}
	}
	return merged, nil
}

// lineLimit bounds the work of the line merge. A larger file that both sides changed is a
// conflict.
const lineLimit = 20_000_000

// matches returns, for each line of base, the index of the line of other that it matches
// in a longest common subsequence, or -1.
func matches(base, other []string) []int {
	rows, cols := len(base), len(other)
	lengths := make([][]int32, rows+1)
	for i := range lengths {
		lengths[i] = make([]int32, cols+1)
	}
	for i := rows - 1; i >= 0; i-- {
		for j := cols - 1; j >= 0; j-- {
			if base[i] == other[j] {
				lengths[i][j] = lengths[i+1][j+1] + 1
			} else {
				lengths[i][j] = max(lengths[i+1][j], lengths[i][j+1])
			}
		}
	}
	match := make([]int, rows)
	for i := range match {
		match[i] = -1
	}
	for i, j := 0, 0; i < rows && j < cols; {
		switch {
		case base[i] == other[j]:
			match[i] = j
			i++
			j++
		case lengths[i+1][j] >= lengths[i][j+1]:
			i++
		default:
			j++
		}
	}
	return match
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mergeLines is a diff3 merge of three versions of a text. It returns false when both
// sides changed the same region in different ways.
func mergeLines(baseText, oursText, theirsText string) (string, bool) {
	base := strings.SplitAfter(baseText, "\n")
	ours := strings.SplitAfter(oursText, "\n")
	theirs := strings.SplitAfter(theirsText, "\n")
	if len(base)*len(ours) > lineLimit || len(base)*len(theirs) > lineLimit {
		return "", false
	}
	inOurs, inTheirs := matches(base, ours), matches(base, theirs)
	var merged []string
	b, o, t := 0, 0, 0
	for b < len(base) || o < len(ours) || t < len(theirs) {
		// A stable line is the next line of all three versions.
		if b < len(base) && inOurs[b] == o && inTheirs[b] == t {
			merged = append(merged, base[b])
			b, o, t = b+1, o+1, t+1
			continue
		}
		// The region is not stable. It ends at the next base line that both sides kept.
		endBase, endOurs, endTheirs := len(base), len(ours), len(theirs)
		for i := b; i < len(base); i++ {
			if inOurs[i] >= o && inTheirs[i] >= t {
				endBase, endOurs, endTheirs = i, inOurs[i], inTheirs[i]
				break
			}
		}
		baseRegion, oursRegion, theirsRegion := base[b:endBase], ours[o:endOurs], theirs[t:endTheirs]
		switch {
		case equalLines(oursRegion, baseRegion):
			merged = append(merged, theirsRegion...)
		case equalLines(theirsRegion, baseRegion), equalLines(oursRegion, theirsRegion):
			merged = append(merged, oursRegion...)
		default:
			return "", false
		}
		b, o, t = endBase, endOurs, endTheirs
	}
	return strings.Join(merged, ""), true
}

// flatten returns every file of the tree with its full path.
func flatten(tree *object.Tree) (map[string]file, error) {
	files := map[string]file{}
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		name, entry, err := walker.Next()
		if err != nil {
			break
		}
		if entry.Mode != filemode.Dir {
			files[name] = file{Mode: entry.Mode, Hash: entry.Hash}
		}
	}
	return files, nil
}

// writeTree stores the tree objects for a set of files and returns the hash of the root.
func (s *sqlStorage) writeTree(files map[string]file) (plumbing.Hash, error) {
	here := []object.TreeEntry{}
	below := map[string]map[string]file{}
	for path, f := range files {
		name, rest, nested := strings.Cut(path, "/")
		if !nested {
			here = append(here, object.TreeEntry{Name: name, Mode: f.Mode, Hash: f.Hash})
			continue
		}
		if below[name] == nil {
			below[name] = map[string]file{}
		}
		below[name][rest] = f
	}
	for name, children := range below {
		hash, err := s.writeTree(children)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		here = append(here, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: hash})
	}
	sort.Sort(object.TreeEntrySorter(here))
	encoded := s.NewEncodedObject()
	if err := (&object.Tree{Entries: here}).Encode(encoded); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("encode tree: %w", err)
	}
	return s.SetEncodedObject(encoded)
}
