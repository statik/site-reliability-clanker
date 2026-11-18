package agent

import "testing"

func TestFirstObjectIgnoresTheTextAroundTheObject(t *testing.T) {
	object := `{"diagnosis":"a {brace} in a string","nested":{"n":1}}`
	answers := []string{
		object,
		"  " + object + "\n\n",
		"```json\n" + object + "\n```",
		"```\n" + object + "\n```",
		"Here is the answer:\n" + object,
		object + "\nThe pods recover on the next probe.",
		object + "\n```json\n" + object + "\n```",
		object + `{"diagnosis":"a second object"}`,
		object + " {",
	}
	for _, answer := range answers {
		got, err := firstObject(answer)
		if err != nil || got != object {
			t.Errorf("firstObject(%q) = %q, %v; want the object", answer, got, err)
		}
	}
}

func TestFirstObjectRefusesAnAnswerWithNoCompleteObject(t *testing.T) {
	answers := []string{
		"",
		"The system is healthy.",
		"null",
		"[]",
		`{"diagnosis":"the answer stops here`,
		`{"diagnosis":"no closing brace"`,
		`The set {a, b} is not JSON. {"diagnosis":"too late"}`,
	}
	for _, answer := range answers {
		if got, err := firstObject(answer); err == nil {
			t.Errorf("firstObject(%q) = %q, want an error", answer, got)
		}
	}
}
