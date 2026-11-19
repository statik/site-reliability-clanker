// Package scrub removes secret values from text before the text leaves the control plane,
// to a model or to a notification channel. It knows a secret in two ways: by its value,
// when the control plane registered the value with Add, and by its shape.
package scrub

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// MinKnownLength is the length in bytes below which Add ignores a value. A short value
// such as "admin" or "5432" is also an ordinary word or number, so its replacement would
// damage text that holds no secret. A secret of fewer than 8 characters is also weak.
const MinKnownLength = 8

// knownReplacement stands for a known value. It has no quote and no backslash, so it
// leaves a JSON string valid.
const knownReplacement = "[REDACTED]"

var (
	knownLock sync.RWMutex
	// known holds the registered values and their JSON forms.
	known = map[string]struct{}{}
)

// Add registers secret values that the control plane knows. Text replaces every
// occurrence of a registered value, whatever surrounds it. Add ignores a value that is
// shorter than MinKnownLength. It is safe for concurrent use.
func Add(values ...string) {
	knownLock.Lock()
	defer knownLock.Unlock()
	for _, value := range values {
		if len(value) < MinKnownLength {
			continue
		}
		known[value] = struct{}{}
		// The input of a model is JSON, where a quote, a backslash, <, > and & have an
		// escape. The value is in that text only in its JSON form.
		if encoded, err := json.Marshal(value); err == nil {
			known[string(encoded[1:len(encoded)-1])] = struct{}{}
		}
	}
}

// replaceKnown replaces every occurrence of a registered value. Two occurrences that
// overlap become one replacement, so no part of either value stays in the text.
func replaceKnown(s string) string {
	knownLock.RLock()
	spans := [][2]int{}
	for value := range known {
		for from := 0; ; {
			at := strings.Index(s[from:], value)
			if at < 0 {
				break
			}
			spans = append(spans, [2]int{from + at, from + at + len(value)})
			from += at + 1
		}
	}
	knownLock.RUnlock()
	if len(spans) == 0 {
		return s
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	var out strings.Builder
	end := 0
	for _, span := range spans {
		if span[0] >= end {
			out.WriteString(s[end:span[0]])
			out.WriteString(knownReplacement)
		}
		end = max(end, span[1])
	}
	out.WriteString(s[end:])
	return out.String()
}

type rule struct {
	pattern     *regexp.Regexp
	replacement string
}

var rules = []rule{
	// A password inside a URL. The password can contain a slash.
	{regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^:/\s@]*):[^@\s]+@`), "$1:[REDACTED]@"},
	{regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
		"[REDACTED-TOKEN]"},
	{regexp.MustCompile(`\b(AKIA|ASIA)[A-Z0-9]{16}\b`), "[REDACTED-AWS-KEY]"},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`), "[REDACTED-API-KEY]"},
	{regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr|github_pat)_[A-Za-z0-9_]{20,}`),
		"[REDACTED-TOKEN]"},
	{regexp.MustCompile(`(?i)((?:bearer|basic)\s+)[A-Za-z0-9._~+/=-]{8,}`), "$1[REDACTED]"},
	// Any URL with "webhook" or "hooks" in it. The path of a webhook URL is the secret.
	{regexp.MustCompile(`(?i)https?://[^\s"']*(webhook|hooks)[^\s"']*`), "[REDACTED-WEBHOOK]"},
	// A name that says it holds a secret, then = or :, then the value. The name can have a
	// prefix or suffix (ADMIN_TOKEN, SMTP_PASS, PGPASSWORD, ADMIN_KEY, Pwd) and JSON quotes.
	// The quotes can have a backslash before them: a JSON log line inside a JSON string.
	// The value stops before an escaped quote, so the outer JSON stays correct.
	{regexp.MustCompile(`(?i)([a-z0-9_.-]*(?:password|passwd|_pass|pwd|secret|token|api[_-]?key|_key)` +
		`[a-z0-9_.-]*(?:\\*")?\s*[=:]\s*(?:\\*")?)(?:\\[^"\s]|[^\s",}\\])+`),
		"$1[REDACTED]"},
	// A long flag of a command line that names a secret, then a space, then the value. The
	// rule above reads the form with =. A value that starts with - is the next flag.
	{regexp.MustCompile(`(?i)(--[a-z0-9-]*(?:password|passwd|token|secret|api-?key) +"?)` +
		`[^\s",}-][^\s",}]*`), "$1[REDACTED]"},
	// The password flag -a of redis-cli. The rule names the program, because -a is an
	// ordinary flag of grep, tar and cp. The command ends at a pipe or a separator.
	{regexp.MustCompile(`(\bredis-cli\b[^\n|;&\\]*?\s(?:-a|--pass)[ =]"?)[^\s",}]+`),
		"$1[REDACTED]"},
	// The password flag -p of the programs that take a password there. The rule names the
	// programs, because -p is a port or a process for ssh, ps and mkdir. It reads the case
	// of the flag: -P is the port of mysql.
	{regexp.MustCompile(`(\b(?:sshpass|mysql|mysqldump|mysqladmin|mariadb|mongosh|mongodump)\b` +
		`[^\n|;&\\]*?\s-p ?"?)[^\s",}]+`), "$1[REDACTED]"},
	// A sentence that tells a secret: "the password is X", "the credential was X". The
	// value must have a letter and a digit, so "the password is required" and "the token
	// is 5 minutes old" stay. A value of only letters or only digits passes this rule.
	{regexp.MustCompile(`(?i)((?:password|passwd|passphrase|credential|secret|token|api[ _-]?key)` +
		`s?\s+(?:is|was)\s+(?:now\s+)?"?)[^\s",}]*(?:[a-z][^\s",}]*[0-9]|[0-9][^\s",}]*[a-z])` +
		`[^\s",}]*`), "$1[REDACTED]"},
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`),
		"[REDACTED-PRIVATE-KEY]"},
}

// Text returns s with every registered value and every secret pattern replaced. The
// values go first: a pattern that matches a part of a value must not leave the rest.
func Text(s string) string {
	s = replaceKnown(s)
	for _, r := range rules {
		s = r.pattern.ReplaceAllString(s, r.replacement)
	}
	return s
}
