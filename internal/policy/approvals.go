package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ApprovalsPath is the path of the approval settings in the runbooks repository.
const ApprovalsPath = "policy/approvals.json"

const approvalsFormat = `the file must be {"t3_approvers": N}, and the allowed values of N ` +
	`are 1 and 2`

// Approvals are the approval settings of the runbooks repository. The zero value is the
// settings of a repository that has no settings file: tier T3 needs two approvers.
type Approvals struct {
	// t3One is true when the file sets one approver for tier T3.
	t3One bool
}

// ParseApprovals reads the content of the settings file. It accepts exactly one JSON object
// with the one key t3_approvers and the value 1 or 2. Everything else is an error.
func ParseApprovals(source []byte) (Approvals, error) {
	value, err := t3Approvers(source)
	if err != nil {
		return Approvals{}, fmt.Errorf("%s is not valid (%w): %s", ApprovalsPath, err,
			approvalsFormat)
	}
	switch value {
	case "1":
		return Approvals{t3One: true}, nil
	case "2":
		return Approvals{}, nil
	default:
		return Approvals{}, fmt.Errorf("%s has t3_approvers = %s: %s", ApprovalsPath, value,
			approvalsFormat)
	}
}

// t3Approvers returns the number in the settings file as it is written there. It reads the
// file token by token, because the decoder of a struct ignores the case of a key and
// accepts a key that comes twice.
func t3Approvers(source []byte) (string, error) {
	// A file that is valid has four tokens: {, the key, the number and }.
	tokens, err := firstTokens(source, 5)
	if err != nil {
		return "", err
	}
	if len(tokens) != 4 || tokens[0] != json.Delim('{') || tokens[1] != "t3_approvers" ||
		tokens[3] != json.Delim('}') {
		return "", errors.New("it is not one JSON object with the one key t3_approvers")
	}
	number, ok := tokens[2].(json.Number)
	if !ok {
		return "", errors.New("the value of t3_approvers is not a number")
	}
	return number.String(), nil
}

// firstTokens returns the JSON tokens of source, and stops after limit tokens.
func firstTokens(source []byte, limit int) ([]json.Token, error) {
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()
	tokens := []json.Token{}
	for len(tokens) < limit {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

// Required is the number of distinct approvers that a change of the tier needs.
func (a Approvals) Required(tier string) int {
	switch tier {
	case "T3":
		if a.t3One {
			return 1
		}
		return 2
	case "T2":
		return 1
	default:
		return 0
	}
}
