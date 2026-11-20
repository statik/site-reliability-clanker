package jobspec

import (
	"fmt"
	"regexp"
)

// Reads of a diagnose job. Each one reads the application and changes nothing.
const (
	// ReadPods lists the pods of the namespace with their state.
	ReadPods = "pods"
	// ReadWorkloads lists the deployments of the namespace with their state.
	ReadWorkloads = "workloads"
	// ReadLogs gives the last lines of the logs of one workload.
	ReadLogs = "logs"
	// ReadEvents gives the events of the namespace, the newest last.
	ReadEvents = "events"
)

// MaxReadLines is the most lines that one read of logs can ask for.
const MaxReadLines = 200

// kubernetesName is the form of a namespace and of a workload. A name cannot start with
// "-", so a name is never a flag of a command.
var kubernetesName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// Read is one read of a diagnose job. It has names and a number, and no command: the
// runner and the core make the command from it with the same code.
type Read struct {
	// Read is one of the Read constants.
	Read string `json:"read"`
	// Workload is the deployment whose logs are read. Only ReadLogs has one.
	Workload string `json:"workload,omitempty"`
	// Lines is the number of the last log lines to read. Only ReadLogs has it.
	Lines int `json:"lines,omitempty"`
}

// KubectlArgs returns the arguments of the kubectl command of the read, in the namespace.
// It refuses a read that it does not know, and a name or a number outside its limits.
func (r Read) KubectlArgs(namespace string) ([]string, error) {
	if !kubernetesName.MatchString(namespace) {
		return nil, fmt.Errorf("the namespace %q of a read is not a name", namespace)
	}
	if r.Read != ReadLogs && (r.Workload != "" || r.Lines != 0) {
		return nil, fmt.Errorf("the read %q takes no workload and no lines", r.Read)
	}
	switch r.Read {
	case ReadPods:
		return []string{"get", "-n", namespace, "pods", "-o", "wide"}, nil
	case ReadWorkloads:
		return []string{"get", "-n", namespace, "deployments"}, nil
	case ReadEvents:
		return []string{"get", "-n", namespace, "events", "--sort-by=.lastTimestamp"}, nil
	case ReadLogs:
		if !kubernetesName.MatchString(r.Workload) {
			return nil, fmt.Errorf("the workload %q of a read of logs is not a name", r.Workload)
		}
		if r.Lines < 1 || r.Lines > MaxReadLines {
			return nil, fmt.Errorf("a read of logs has %d lines; it must have 1 to %d", r.Lines,
				MaxReadLines)
		}
		return []string{"logs", "-n", namespace, "deployment/" + r.Workload,
			fmt.Sprintf("--tail=%d", r.Lines)}, nil
	}
	return nil, fmt.Errorf("the read %q does not exist", r.Read)
}
