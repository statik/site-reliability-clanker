package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// endOfThinking is the mark that some models with a thinking mode leave in the answer. The
// text before the mark is a draft, not the answer.
const endOfThinking = "</think>"

// firstObject returns the first JSON object in the answer of a model: the text from the
// first opening brace to the brace that closes it. It ignores the text before and after the
// object, because some models put the object in a code fence, repeat it, or add a remark.
// When the answer has the mark endOfThinking, the answer starts after the last mark.
// An answer with no brace, or with an object that is not complete, is an error. The caller
// checks the fields of the object.
func firstObject(content string) (string, error) {
	if mark := strings.LastIndex(content, endOfThinking); mark >= 0 {
		content = content[mark+len(endOfThinking):]
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
