package forge

import (
	"errors"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeLines(t *testing.T) {
	cases := []struct {
		name, base, ours, theirs, want string
		clean                          bool
	}{
		{"nothing changed", "a\nb\nc\n", "a\nb\nc\n", "a\nb\nc\n", "a\nb\nc\n", true},
		{"only ours", "a\nb\nc\n", "a\nB\nc\n", "a\nb\nc\n", "a\nB\nc\n", true},
		{"only theirs", "a\nb\nc\n", "a\nb\nc\n", "a\nb\nC\n", "a\nb\nC\n", true},
		{"far apart", "a\nb\nc\nd\ne\n", "A\nb\nc\nd\ne\n", "a\nb\nc\nd\nE\n", "A\nb\nc\nd\nE\n", true},
		{"same change on both sides", "a\nb\nc\n", "a\nX\nc\n", "a\nX\nc\n", "a\nX\nc\n", true},
		{"ours adds at the end, theirs at the start", "a\nb\n", "a\nb\nz\n", "0\na\nb\n", "0\na\nb\nz\n", true},
		{"ours deletes, theirs edits elsewhere", "a\nb\nc\nd\n", "a\nc\nd\n", "a\nb\nc\nD\n", "a\nc\nD\n", true},
		{"empty base, same addition", "", "x\n", "x\n", "x\n", true},
		{"no newline at the end", "a\nb\nc", "A\nb\nc", "a\nb\nc", "A\nb\nc", true},
		{"same line, different values", "a\nb\nc\n", "a\nX\nc\n", "a\nY\nc\n", "", false},
		{"lines next to each other", "a\nb\nc\n", "A\nb\nc\n", "a\nB\nc\n", "", false},
		{"ours deletes what theirs edits", "a\nb\nc\n", "a\nc\n", "a\nB\nc\n", "", false},
		{"both add different lines at one place", "a\nc\n", "a\nx\nc\n", "a\ny\nc\n", "", false},
	}
	for _, c := range cases {
		got, clean := mergeLines(c.base, c.ours, c.theirs)
		if clean != c.clean || (clean && got != c.want) {
			t.Errorf("%s: got %q clean=%v, want %q clean=%v", c.name, got, clean, c.want, c.clean)
		}
	}
}

// gitMerge runs `git merge-file`. It returns the merged text and whether it was clean.
func gitMerge(t *testing.T, dir, base, ours, theirs string) (string, bool) {
	t.Helper()
	for name, content := range map[string]string{"base": base, "ours": ours, "theirs": theirs} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("git", "merge-file", "-p", "ours", "base", "theirs")
	cmd.Dir = dir
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 && exit.ExitCode() < 128 {
		return "", false
	}
	if err != nil {
		t.Fatalf("git merge-file: %v", err)
	}
	return string(out), true
}

func mutate(random *rand.Rand, lines []string) []string {
	out := append([]string{}, lines...)
	for range random.Intn(3) {
		if len(out) == 0 {
			out = append(out, "new\n")
			continue
		}
		at := random.Intn(len(out))
		switch random.Intn(3) {
		case 0:
			out[at] = "changed " + string(rune('a'+random.Intn(26))) + "\n"
		case 1:
			out = append(out[:at], out[at+1:]...)
		default:
			out = append(out[:at], append([]string{"added " + string(rune('a'+random.Intn(26))) + "\n"}, out[at:]...)...)
		}
	}
	return out
}

// The line merge must never give a clean result that git would not give. It may refuse a
// merge that git accepts.
func TestMergeLinesAgreesWithGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	random := rand.New(rand.NewSource(20261117))
	dir := t.TempDir()
	clean, refusedByBoth, stricter := 0, 0, 0
	for round := range 400 {
		base := []string{}
		for i := range 4 + random.Intn(12) {
			base = append(base, "line "+string(rune('a'+i))+"\n")
		}
		baseText := strings.Join(base, "")
		oursText := strings.Join(mutate(random, base), "")
		theirsText := strings.Join(mutate(random, base), "")
		got, ok := mergeLines(baseText, oursText, theirsText)
		want, gitOK := gitMerge(t, dir, baseText, oursText, theirsText)
		switch {
		case ok && !gitOK:
			t.Fatalf("round %d: a clean merge where git has a conflict\nbase %q\nours %q\ntheirs %q\ngot %q",
				round, baseText, oursText, theirsText, got)
		case ok && got != want:
			t.Fatalf("round %d: the result differs from git\nbase %q\nours %q\ntheirs %q\ngot  %q\nwant %q",
				round, baseText, oursText, theirsText, got, want)
		case ok:
			clean++
		case gitOK:
			stricter++
		default:
			refusedByBoth++
		}
	}
	t.Logf("400 rounds: %d clean and equal to git, %d refused by both, %d refused only here",
		clean, refusedByBoth, stricter)
	if clean < 100 {
		t.Fatalf("only %d clean merges: the test does not exercise the merge", clean)
	}
}

func TestMergeFilesByPath(t *testing.T) {
	regular := func(id byte) file { return file{Mode: 0o100644, Hash: [20]byte{id}} }
	never := func(file, file, file) (file, bool, error) {
		t.Fatal("the content merge ran for a path that only one side changed")
		return file{}, false, nil
	}
	base := map[string]file{"same": regular(1), "ours-edit": regular(2), "theirs-edit": regular(3),
		"ours-delete": regular(4), "theirs-delete": regular(5), "both-delete": regular(6)}
	ours := map[string]file{"same": regular(1), "ours-edit": regular(20), "theirs-edit": regular(3),
		"theirs-delete": regular(5), "ours-add": regular(7), "both-add-same": regular(9)}
	theirs := map[string]file{"same": regular(1), "ours-edit": regular(2), "theirs-edit": regular(30),
		"ours-delete": regular(4), "theirs-add": regular(8), "both-add-same": regular(9)}
	merged, err := mergeFiles(base, ours, theirs, never)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]file{"same": regular(1), "ours-edit": regular(20), "theirs-edit": regular(30),
		"ours-add": regular(7), "theirs-add": regular(8), "both-add-same": regular(9)}
	if len(merged) != len(want) {
		t.Fatalf("merged = %v", merged)
	}
	for path, f := range want {
		if merged[path] != f {
			t.Errorf("%s = %v, want %v", path, merged[path], f)
		}
	}

	conflicting := map[string]file{"a": regular(1), "b": regular(1)}
	_, err = mergeFiles(conflicting,
		map[string]file{"a": regular(2), "new": regular(5)},
		map[string]file{"a": regular(3), "b": regular(4), "new": regular(6)},
		func(file, file, file) (file, bool, error) { return file{}, false, nil })
	var conflict *ConflictError
	if !errors.As(err, &conflict) || strings.Join(conflict.Paths, ",") != "a,b,new" {
		t.Fatalf("error = %v, want a conflict on a, b and new", err)
	}
}
