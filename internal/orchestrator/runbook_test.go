package orchestrator

import (
	"strings"
	"testing"
)

func TestLastPartKeepsTheNewestLines(t *testing.T) {
	short := "one\ntwo"
	if got := lastPart(short); got != short {
		t.Errorf("short output must stay unchanged, got %q", got)
	}
	var long strings.Builder
	for range 400 {
		long.WriteString("old line of log output\n")
	}
	long.WriteString("NEWEST ERROR status 500")
	got := lastPart(long.String())
	if len(got) > maxCommandOutput+40 {
		t.Errorf("output has %d characters, over the limit", len(got))
	}
	if !strings.HasSuffix(got, "NEWEST ERROR status 500") {
		t.Error("the newest line must be kept")
	}
	if !strings.HasPrefix(got, "(earlier output left out)\nold line") {
		t.Errorf("want a note and then a whole line, got %q", got[:60])
	}
}
