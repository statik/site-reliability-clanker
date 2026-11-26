package applayer

import "strings"

// Align lines up the "=" of the assignments of HCL text as `tofu fmt` does, so the files
// that the core writes pass `tofu fmt -check`. The core cannot run tofu.
//
// The rule is the one of hclwrite: a line is an assignment when it has a "=" after its
// first character whose right side opens no bracket that it does not close. Consecutive
// assignments form a chain; every other line, an empty one too, ends it. In a chain, each
// "=" goes one column after the longest left side.
func Align(text string) string {
	lines := strings.Split(text, "\n")
	start := -1
	for i := 0; i <= len(lines); i++ {
		at := -1
		if i < len(lines) {
			at = assignAt(lines[i])
		}
		if at >= 0 && start < 0 {
			start = i
		}
		if at < 0 && start >= 0 {
			alignChain(lines[start:i])
			start = -1
		}
	}
	return strings.Join(lines, "\n")
}

// alignChain aligns the "=" of a chain of assignments.
func alignChain(lines []string) {
	width := 0
	for _, line := range lines {
		width = max(width, len(strings.TrimRight(line[:assignAt(line)], " ")))
	}
	for i, line := range lines {
		at := assignAt(line)
		lead := strings.TrimRight(line[:at], " ")
		lines[i] = lead + strings.Repeat(" ", width-len(lead)+1) + "= " +
			strings.TrimLeft(line[at+1:], " ")
	}
}

// assignAt returns the index of the "=" of an assignment in a line, or -1 when the line is
// not an assignment.
func assignAt(line string) int {
	at := equalSign(line)
	if at <= 0 || strings.TrimSpace(line[:at]) == "" || bracketBalance(line[at+1:]) != 0 {
		return -1
	}
	return at
}

// equalSign returns the index of the first "=" outside a string that is not part of "==",
// "!=", "<=", ">=" or "=>", or -1.
func equalSign(line string) int {
	inString := false
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case inString && c == '\\':
			i++
		case c == '"':
			inString = !inString
		case inString || c != '=':
		case i+1 < len(line) && (line[i+1] == '=' || line[i+1] == '>'):
			i++
		case i > 0 && strings.IndexByte("=!<>", line[i-1]) >= 0:
		default:
			return i
		}
	}
	return -1
}

// bracketBalance returns the brackets that text opens minus those that it closes, outside
// strings.
func bracketBalance(text string) int {
	balance, inString := 0, false
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case inString && c == '\\':
			i++
		case c == '"':
			inString = !inString
		case inString:
		case strings.IndexByte("{[(", c) >= 0:
			balance++
		case strings.IndexByte("}])", c) >= 0:
			balance--
		}
	}
	return balance
}
