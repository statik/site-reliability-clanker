package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
