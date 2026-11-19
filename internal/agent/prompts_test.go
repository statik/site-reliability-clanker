package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
)

// The prompts have placeholders for the names of the application. With the example pack
// each prompt must be, byte for byte, the text that the evals measured (testdata/prompts
// holds that text). Then the results of the evals stay valid.
func TestThePromptsOfTheExamplePackAreTheMeasuredText(t *testing.T) {
	prompts := map[string]string{"explainer": explainerSystem, "author": authorSystem,
		"investigator": investigatorSystem, "ask": askSystem, "reviewer": reviewerSystem}
	app := packtest.Example(t)
	for name, template := range prompts {
		want, err := os.ReadFile(filepath.Join("testdata", "prompts", name+".txt"))
		if err != nil {
			t.Fatal(err)
		}
		if got := app.Render(template); got != string(want) {
			t.Errorf("the prompt %s of the example pack is not the measured text:\n%s", name, got)
		}
	}
}

func TestThePromptsOfAnotherPackNameThatPack(t *testing.T) {
	example, second := packtest.Example(t), packtest.Second(t)
	for name, template := range map[string]string{"author": authorSystem,
		"investigator": investigatorSystem} {
		got := second.Render(template)
		if strings.Contains(got, "{{") || strings.Contains(got, example.Name) {
			t.Errorf("the prompt %s still has a placeholder or the example name:\n%s", name, got)
		}
		if !strings.Contains(got, second.Name) {
			t.Errorf("the prompt %s does not name the pack %s", name, second.Name)
		}
	}
	tools := authorTools(second)
	described, _ := tools[0].Function.Parameters["properties"].(map[string]any)
	if text := described["repo"].(map[string]any)["description"]; text !=
		"platform-live, harbor or runbooks" {
		t.Errorf("the tool read_file describes its repositories as %q", text)
	}
}
