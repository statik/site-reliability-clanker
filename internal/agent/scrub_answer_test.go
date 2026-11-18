package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/scrub"
)

func TestScrubAnswerKeepsAJSONAnswerValid(t *testing.T) {
	scrub.Add("known-answer-value-77")
	quote := `\"`
	answers := map[string]string{
		"sentence":       `{"tier":"T2","concerns":["the new password is hunter2 in the diff"]}`,
		"escaped quote":  `{"sentence":"the log has password=hunter2` + quote + ` and more","n":1}`,
		"number":         `{"sentence":"ok","token_count": 12345}`,
		"nested object":  `{"secret": {"name": "x"}, "sentence": "password is abc123"}`,
		"known value":    `{"evidence":"| relay | known-answer-value-77 |"}`,
		"named field":    `{"password":"hunter2","token_count": 5}`,
		"code fence":     "Here it is:\n```json\n" + `{"sentence":"token is ab12cd"}` + "\n```",
		"tool arguments": `{"title":"Rotate","body":"redis-cli -a hunter2 ping"}`,
	}
	for name, answer := range answers {
		if _, err := firstObject(answer); err != nil {
			t.Fatalf("%s: the test answer has no object: %v", name, err)
		}
		out := scrubAnswer(answer)
		object, err := firstObject(out)
		if err != nil {
			t.Errorf("%s: the scrubbed answer has no object: %v: %s", name, err, out)
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(object), &decoded); err != nil {
			t.Errorf("%s: the object does not decode: %v", name, err)
		}
		for _, secret := range []string{"hunter2", "abc123", "ab12cd", "known-answer-value-77"} {
			if strings.Contains(out, secret) {
				t.Errorf("%s: the secret %s is still in the answer: %s", name, secret, out)
			}
		}
	}
}

func TestScrubAnswerScrubsProseAndKeepsCleanText(t *testing.T) {
	got := scrubAnswer("The Redis password is cnry-arg-2290-vv and it is old.")
	if got != "The Redis password is [REDACTED] and it is old." {
		t.Errorf("prose answer: got %q", got)
	}
	for _, clean := range []string{"", "WI-0021 changes the image tag to 1.1.0.",
		`{"tier":"T2","concerns":[]}`} {
		if got := scrubAnswer(clean); got != clean {
			t.Errorf("clean answer changed: %q became %q", clean, got)
		}
	}
}
