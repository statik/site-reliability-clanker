// Package scrub removes secret values from text before the text leaves the control plane,
// to a model or to a notification channel.
package scrub

import "regexp"

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
	{regexp.MustCompile(`(?i)([a-z0-9_.-]*(?:password|passwd|_pass|pwd|secret|token|api[_-]?key|_key)` +
		`[a-z0-9_.-]*"?\s*[=:]\s*"?)[^\s",}]+`), "$1[REDACTED]"},
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
