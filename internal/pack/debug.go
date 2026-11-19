package pack

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// readVerbs are the kubectl verbs of a debug session. All of them read.
var readVerbs = []string{"get", "logs", "describe", "top"}

// debugFlags are the flags that a command of a pack can have. None of them selects a
// namespace, a cluster or an identity: the core does that.
var debugFlags = []string{"-o", "--tail", "--since", "--sort-by", "-l", "--selector", "-c",
	"--container", "--previous", "--all-containers"}

// outputFormats are the values of -o that print no full object. A full object can hold a
// secret.
var outputFormats = []string{"wide", "name"}

var (
	debugWordPattern  = regexp.MustCompile(`^[a-z0-9][A-Za-z0-9./_-]{0,80}$`)
	debugValuePattern = regexp.MustCompile(`^[A-Za-z0-9.=,_/-]{1,80}$`)
)

func (p Pack) checkDebugCommands() error {
	if len(p.DebugCommands) == 0 || len(p.DebugCommands) > 8 {
		return fmt.Errorf("debug_commands must have 1 to 8 commands")
	}
	for _, command := range p.DebugCommands {
		if err := checkDebugCommand(command); err != nil {
			return fmt.Errorf("debug command %q: %w", strings.Join(command, " "), err)
		}
	}
	return nil
}

func checkDebugCommand(command []string) error {
	if len(command) < 2 || len(command) > 8 {
		return fmt.Errorf("a command has 2 to 8 words")
	}
	if !slices.Contains(readVerbs, command[0]) {
		return fmt.Errorf("the first word must be one of %s", strings.Join(readVerbs, ", "))
	}
	for index := 1; index < len(command); index++ {
		if err := checkDebugWord(command[index-1], command[index]); err != nil {
			return err
		}
	}
	return nil
}

// checkDebugWord checks one word of a command after the verb.
func checkDebugWord(previous, word string) error {
	switch {
	case strings.HasPrefix(word, "-"):
		return checkDebugFlag(word)
	case previous == "-o":
		if !slices.Contains(outputFormats, word) {
			return fmt.Errorf("-o must be one of %s", strings.Join(outputFormats, ", "))
		}
	case !debugWordPattern.MatchString(word), strings.Contains(word, "secret"):
		return fmt.Errorf("the word %q is not permitted", word)
	}
	return nil
}

func checkDebugFlag(word string) error {
	name, value, hasValue := strings.Cut(word, "=")
	if !slices.Contains(debugFlags, name) {
		return fmt.Errorf("the flag %q is not permitted; use one of %s", name,
			strings.Join(debugFlags, ", "))
	}
	if name == "-o" && hasValue {
		return fmt.Errorf("write -o and its value as two words")
	}
	if hasValue && !debugValuePattern.MatchString(value) {
		return fmt.Errorf("the value of %s is not permitted", name)
	}
	return nil
}
