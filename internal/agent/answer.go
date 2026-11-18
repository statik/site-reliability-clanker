package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/scrub"
)

// endOfThinking is the mark that some models with a thinking mode leave in the answer. An
// object before the mark can be a draft of the object after it.
const endOfThinking = "</think>"

// firstObject returns the first JSON object in the answer of a model: the text from the
// first opening brace to the brace that closes it. It ignores the text before and after the
// object, because some models put the object in a code fence, repeat it, or add a remark.
// When an object starts after the last mark endOfThinking, the function reads that object.
// When no object starts after the last mark, it reads the text before the mark.
// An answer with no brace, or with an object that is not complete, is an error. The caller
// checks the fields of the object.
func firstObject(content string) (string, error) {
	if before, after, found := cutLast(content, endOfThinking); found {
		content = before
		if strings.Contains(after, "{") {
			content = after
		}
	}
	start := strings.IndexByte(content, '{')
	if start < 0 {
		return "", errors.New("the answer has no JSON object")
	}
	var object json.RawMessage
	if err := json.NewDecoder(strings.NewReader(content[start:])).Decode(&object); err != nil {
		return "", fmt.Errorf("the JSON object in the answer is not complete: %w", err)
	}
	return string(object), nil
}

// cutLast splits text around the last mark in it.
func cutLast(text, mark string) (before, after string, found bool) {
	at := strings.LastIndex(text, mark)
	if at < 0 {
		return text, "", false
	}
	return text[:at], text[at+len(mark):], true
}

// scrubAnswer removes secrets from an answer of a model, or from the arguments of a tool
// call. A role reads a JSON object from most answers, and a replacement can break one: it
// can take the backslash of an escaped quote, or put a marker in the place of a number.
// When the scrubbed text has no complete object any more, the function decodes the
// object of the answer, scrubs each string in it and encodes the result.
func scrubAnswer(content string) string {
	scrubbed := scrub.Text(content)
	if scrubbed == content {
		return content
	}
	object, err := firstObject(content)
	if err != nil {
		return scrubbed
	}
	if _, err := firstObject(scrubbed); err == nil {
		return scrubbed
	}
	var value any
	if err := json.Unmarshal([]byte(object), &value); err != nil {
		return scrubbed
	}
	encoded, err := json.Marshal(scrubStrings(value))
	if err != nil {
		return scrubbed
	}
	return string(encoded)
}

// scrubStrings scrubs every string of a decoded JSON value.
func scrubStrings(value any) any {
	switch v := value.(type) {
	case string:
		return scrub.Text(v)
	case []any:
		for i, item := range v {
			v[i] = scrubStrings(item)
		}
	case map[string]any:
		for name, item := range v {
			if text, ok := item.(string); ok {
				v[name] = scrubNamed(name, text)
				continue
			}
			v[name] = scrubStrings(item)
		}
	}
	return value
}

// scrubNamed scrubs the string of one field. The scrubber sees the name with the value,
// so a field with a name such as "password" loses its value as it does in the JSON text.
func scrubNamed(name, text string) string {
	prefix := name + ": "
	if out := scrub.Text(prefix + text); strings.HasPrefix(out, prefix) {
		return out[len(prefix):]
	}
	return scrub.Text(text)
}
