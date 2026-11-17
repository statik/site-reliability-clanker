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
// with the key t3_approvers and the value 1 or 2. Everything else is an error.
func ParseApprovals(source []byte) (Approvals, error) {
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.DisallowUnknownFields()
	var file struct {
		T3Approvers *int `json:"t3_approvers"`
	}
	if err := decoder.Decode(&file); err != nil {
		return Approvals{}, fmt.Errorf("%s is not valid (%w): %s", ApprovalsPath, err,
			approvalsFormat)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Approvals{}, fmt.Errorf("%s has data after the JSON object: %s", ApprovalsPath,
			approvalsFormat)
	}
	if file.T3Approvers == nil {
		return Approvals{}, fmt.Errorf("%s has no value for t3_approvers: %s", ApprovalsPath,
			approvalsFormat)
	}
	switch *file.T3Approvers {
	case 1:
		return Approvals{t3One: true}, nil
	case 2:
		return Approvals{}, nil
	default:
		return Approvals{}, fmt.Errorf("%s has t3_approvers = %d: %s", ApprovalsPath,
			*file.T3Approvers, approvalsFormat)
	}
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
