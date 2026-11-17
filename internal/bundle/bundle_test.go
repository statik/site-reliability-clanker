package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name string
	body string
	mode int64
}

func pack(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipped := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipped)
	for _, e := range entries {
		header := &tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.body)),
			Typeflag: tar.TypeReg}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestExtractWritesFilesAndKeepsTheExecutableBit(t *testing.T) {
	data := pack(t, []entry{
		{name: "platform/CONTRACT.md", body: "contract", mode: 0o644},
		{name: "apps/paperboat/build.sh", body: "#!/bin/sh\n", mode: 0o755},
	})
	dir := t.TempDir()
	target, err := extract(data, dir)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(target, "platform", "CONTRACT.md"))
	if err != nil || string(body) != "contract" {
		t.Fatalf("CONTRACT.md = %q, %v", body, err)
	}
	info, err := os.Stat(filepath.Join(target, "apps", "paperboat", "build.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o100 == 0 {
		t.Fatalf("build.sh lost its executable bit: %v", info.Mode())
	}
}

func TestExtractIsDoneOnceForTheSameArchive(t *testing.T) {
	data := pack(t, []entry{{name: "a.txt", body: "one", mode: 0o644}})
	dir := t.TempDir()
	target, err := extract(data, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "a.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := extract(data, dir)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(again, "a.txt"))
	if again != target || string(body) != "changed" {
		t.Fatalf("second extract wrote the files again: %s %q", again, body)
	}
}

func TestExtractRepairsAnIncompleteDirectory(t *testing.T) {
	data := pack(t, []entry{{name: "a.txt", body: "one", mode: 0o644}})
	dir := t.TempDir()
	target, err := extract(data, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(target, ".complete")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "a.txt"), []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := extract(data, dir); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(target, "a.txt"))
	if string(body) != "one" {
		t.Fatalf("a.txt = %q after repair, want %q", body, "one")
	}
}

func TestExtractRefusesAnEntryOutsideTheDirectory(t *testing.T) {
	data := pack(t, []entry{{name: "../escape.txt", body: "x", mode: 0o644}})
	dir := t.TempDir()
	_, err := extract(data, dir)
	if err == nil || !strings.Contains(err.Error(), "outside its directory") {
		t.Fatalf("error = %v, want a refusal", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "escape.txt")); statErr == nil {
		t.Fatal("the entry was written outside the bundle directory")
	}
}

func TestExtractWithoutAnArchiveSaysHowToFixIt(t *testing.T) {
	if Present() {
		t.Skip("this test binary was built with the bundle tag")
	}
	_, err := Extract(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "--source") {
		t.Fatalf("error = %v, want a hint about --source", err)
	}
}

func TestExtractRefusesDataThatIsNotAnArchive(t *testing.T) {
	if _, err := extract([]byte("not gzip"), t.TempDir()); err == nil {
		t.Fatal("no error for data that is not a gzip archive")
	}
}
