package agent

import (
	"context"
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// The example pack has a recorded explanation of its blank slate. Another pack gets the
// template text for the same work item, not the prose about the example application.
func TestReplayAnswersOnlyWithTheRecordingsOfThePack(t *testing.T) {
	wi := orchestrator.WorkItem{ID: "WI-0001", Title: "Blank-slate deployment of local",
		Kind: orchestrator.KindBlankSlate, Env: "local", Layers: orchestrator.Layers}
	tests := map[string]struct {
		app    pack.Pack
		source string
	}{
		"example pack": {packtest.Example(t), "replay"},
		"second pack":  {packtest.Second(t), "template"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			db := storetest.Open(t)
			a := &Agent{App: test.app, DB: db, Forge: forge.New(db), Clock: fixedClock{},
				Audit: audit.New(db, fixedClock{}), LLM: Replay{}}
			explanation, err := a.Explain(context.Background(), wi, nil)
			if err != nil {
				t.Fatal(err)
			}
			if explanation.Source != test.source {
				t.Errorf("source = %q, want %q; sentence: %s", explanation.Source, test.source,
					explanation.Sentence)
			}
			var transcripts []string
			rows, err := db.Query(`SELECT transcript FROM agent_calls`)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for rows.Next() {
				var transcript string
				if err := rows.Scan(&transcript); err != nil {
					t.Fatal(err)
				}
				transcripts = append(transcripts, transcript)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			want := `"replay_key":"` + test.app.Name + `_explain_blank-slate"`
			if test.source == "replay" && (len(transcripts) != 1 ||
				!strings.Contains(transcripts[0], want)) {
				t.Errorf("the recorded calls are %q, want one with %s", transcripts, want)
			}
		})
	}
}

// Agent.complete reads only recordings whose name starts with the name of a pack and "_". A
// file without that start is never read.
func TestEveryRecordingStartsWithTheNameOfThePack(t *testing.T) {
	prefix := packtest.Example(t).Name + "_"
	names, err := fs.Glob(recordings, "replay/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("there is no recording")
	}
	for _, name := range names {
		if !strings.HasPrefix(path.Base(name), prefix) {
			t.Errorf("the recording %s does not start with %q", name, prefix)
		}
	}
}
