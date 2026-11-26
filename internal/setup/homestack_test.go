package setup

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// interruptedTofu stands in for tofu: it waits until it gets an interrupt, records it and
// ends as tofu does after it saved its state.
const interruptedTofu = `#!/bin/sh
trap 'echo interrupted >>"$MARK"; exit 130' INT
echo started >"$MARK.started"
while :; do sleep 0.05; done
`

func TestAStoppedRunInterruptsOpenTofuOnceAndWaits(t *testing.T) {
	bin, dir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tofu"), []byte(interruptedTofu), 0o755); err != nil {
		t.Fatal(err)
	}
	mark := filepath.Join(dir, "mark")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+
		"/bin")
	t.Setenv("MARK", mark)
	home := awsHome{dir: dir}
	if err := os.MkdirAll(home.stack(), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			if _, err := os.Stat(mark + ".started"); err == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	s := homeStack{home: home, profile: "p", region: "us-west-2", out: io.Discard}
	if err := s.tofu(ctx, nil, "apply"); err == nil {
		t.Fatal("a stopped apply reported success")
	}
	recorded, err := os.ReadFile(mark)
	if err != nil || strings.Count(string(recorded), "interrupted") != 1 {
		t.Errorf("tofu recorded %q, %v; want one interrupt", recorded, err)
	}
}
