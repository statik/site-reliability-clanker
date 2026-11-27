package main

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/setup"
)

func TestInstallFlagsChooseTheModel(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte("sk-or-v1-0123456789abcdef0123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := options{cloud: setup.TargetAWS, env: "prod-1", profile: "team/operator"}
	cases := []struct {
		name     string
		flags    installFlags
		provider string
		models   map[string]string
		key      string
		err      string
	}{
		{"no choice", installFlags{}, "", nil, "", ""},
		{"Bedrock with the defaults", installFlags{modelProvider: "bedrock"}, "bedrock", nil,
			"", ""},
		{"a model alone chooses Bedrock", installFlags{bedrockModels: roleModels{
			"author": "global.anthropic.claude-sonnet-5-5"}}, "bedrock",
			map[string]string{"author": "global.anthropic.claude-sonnet-5-5"}, "", ""},
		{"a key file alone keeps the provider to the installer", installFlags{
			modelKeyFile: keyFile}, "", nil, "sk-or-v1-0123456789abcdef0123", ""},
		{"Bedrock with a key", installFlags{modelProvider: "bedrock", modelKeyFile: keyFile},
			"", nil, "", "needs no key"},
		{"OpenRouter with a model", installFlags{modelProvider: "openrouter",
			bedrockModels: roleModels{"author": "x.y.z"}}, "", nil, "", "--bedrock-model is for"},
		{"another provider", installFlags{modelProvider: "openai"}, "", nil, "",
			`"openai" does not exist`},
	}
	for _, tc := range cases {
		o := base
		o.install = tc.flags
		in, err := installInput(o)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s: error %v, want %q", tc.name, err, tc.err)
			}
			continue
		}
		if err != nil || in.ModelProvider != tc.provider || in.ModelKey != tc.key ||
			!maps.Equal(in.BedrockModels, tc.models) {
			t.Errorf("%s: provider %q, key %q, models %v, %v", tc.name, in.ModelProvider,
				in.ModelKey, in.BedrockModels, err)
		}
	}
	var models roleModels
	if err := models.Set("author"); err == nil || !strings.Contains(err.Error(), "ROLE=ID") {
		t.Errorf("a model without = gave %v", err)
	}
}
