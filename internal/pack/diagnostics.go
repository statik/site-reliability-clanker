package pack

import "fmt"

// Reads of a debug session.
const (
	ReadLogs   = "logs"
	ReadEvents = "events"
)

// maxReadLines is the most lines that one read of logs can ask for.
const maxReadLines = 200

// Read is one read of a debug session. A pack gives only names and a number: the core
// makes the command, so a pack cannot choose a verb, a flag or a namespace.
type Read struct {
	// Read is ReadLogs or ReadEvents.
	Read string `json:"read"`
	// Component is the component whose logs are read. A read of events has none.
	Component string `json:"component,omitempty"`
	// Lines is the number of the last log lines to read, from 1 to 200.
	Lines int `json:"lines,omitempty"`
}

func (p Pack) checkDiagnostics() error {
	if len(p.Diagnostics) == 0 || len(p.Diagnostics) > 8 {
		return fmt.Errorf("diagnostics must have 1 to 8 reads")
	}
	for index, read := range p.Diagnostics {
		if err := p.checkRead(read); err != nil {
			return fmt.Errorf("diagnostics read %d: %w", index+1, err)
		}
	}
	return nil
}

func (p Pack) checkRead(read Read) error {
	switch read.Read {
	case ReadLogs:
		if _, ok := p.Component(read.Component); !ok {
			return fmt.Errorf("the component %q is not one of the components", read.Component)
		}
		if read.Lines < 1 || read.Lines > maxReadLines {
			return fmt.Errorf("lines is %d; it must be from 1 to %d", read.Lines, maxReadLines)
		}
	case ReadEvents:
		if read.Component != "" || read.Lines != 0 {
			return fmt.Errorf("a read of events has no component and no lines")
		}
	default:
		return fmt.Errorf("the read %q is not %s or %s", read.Read, ReadLogs, ReadEvents)
	}
	return nil
}

// DebugArgs returns the arguments of each kubectl command of a debug session. The first
// two commands read the state of the pods and of the deployments; the core always runs
// them. Then comes one command for each read of the pack. Every command has the namespace
// of the application, and every command reads.
func (p Pack) DebugArgs() [][]string {
	commands := [][]string{
		{"get", "-n", p.Namespace, "pods", "-o", "wide"},
		{"get", "-n", p.Namespace, "deployments"},
	}
	for _, read := range p.Diagnostics {
		switch read.Read {
		case ReadLogs:
			component, _ := p.Component(read.Component)
			commands = append(commands, []string{"logs", "-n", p.Namespace,
				"deployment/" + component.Workload, fmt.Sprintf("--tail=%d", read.Lines)})
		case ReadEvents:
			commands = append(commands, []string{"get", "-n", p.Namespace, "events",
				"--sort-by=.lastTimestamp"})
		}
	}
	return commands
}
