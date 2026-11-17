// Package scrub removes secret values from text before the text leaves the control plane,
// to a model or to a notification channel.
package scrub

import "regexp"

type rule struct {
	pattern     *regexp.Regexp
	replacement string
}

var rules = []rule{
	{regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^:/\s@]+):[^@/\s]+@`), "$1:[REDACTED]@"},
	{regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
		"[REDACTED-TOKEN]"},
	{regexp.MustCompile(`\b(AKIA|ASIA)[A-Z0-9]{16}\b`), "[REDACTED-AWS-KEY]"},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`), "[REDACTED-API-KEY]"},
	{regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Za-z0-9/]+`),
		"[REDACTED-WEBHOOK]"},
	{regexp.MustCompile(`https://discord(app)?\.com/api/webhooks/[A-Za-z0-9/_-]+`),
		"[REDACTED-WEBHOOK]"},
	{regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key)(\s*[=:]\s*)("?)[^\s",}]+`),
		"$1$2$3[REDACTED]"},
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`),
		"[REDACTED-PRIVATE-KEY]"},
}

// Text returns s with every secret pattern replaced.
func Text(s string) string {
	for _, r := range rules {
		s = r.pattern.ReplaceAllString(s, r.replacement)
	}
	return s
}
