package agent

import _ "embed"

// The system prompts are text files so that the Inspect AI evals in /evals test the exact
// text that the runtime sends.
var (
	//go:embed prompts/explainer.txt
	explainerSystem string
	//go:embed prompts/author.txt
	authorSystem string
	//go:embed prompts/investigator.txt
	investigatorSystem string
	//go:embed prompts/ask.txt
	askSystem string
)
