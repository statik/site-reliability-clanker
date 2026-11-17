package scrub_test

import (
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/scrub"
)

func TestTextRemovesSecrets(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		secret string
	}{
		{"database URL", "postgres://postgres:hunter2secret@postgres.svc:5432/paperboat", "hunter2secret"},
		{"JWT", "token eyJhbGciOiJSUzI1NiIs.eyJpc3MiOiJrdWJlcm5ldGVz.c2lnbmF0dXJlX2hlcmVfMTIz end",
			"eyJpc3MiOiJrdWJlcm5ldGVz"},
		{"AWS key", "key AKIAIOSFODNN7EXAMPLE used", "AKIAIOSFODNN7EXAMPLE"},
		{"API key", "Authorization: Bearer sk-or-v1-abcdef0123456789abcdef", "abcdef0123456789abcdef"},
		{"Slack webhook", "post to https://hooks.slack.com/services/T000/B000/XXXXXXXX", "T000/B000"},
		{"Discord webhook", "https://discord.com/api/webhooks/1234/abcd-EFGH_ijkl", "abcd-EFGH_ijkl"},
		{"password assignment", `password = "s3cretValue"`, "s3cretValue"},
		{"env style", "API_KEY=abc123def456", "abc123def456"},
		{"private key", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----", "MIIEow"},
	}
	for _, tc := range cases {
		out := scrub.Text(tc.in)
		if strings.Contains(out, tc.secret) {
			t.Errorf("%s: the secret is still in the output: %s", tc.name, out)
		}
		if !strings.Contains(out, "REDACTED") {
			t.Errorf("%s: want a REDACTED marker, got %s", tc.name, out)
		}
	}
}

func TestTextKeepsOrdinaryText(t *testing.T) {
	for _, in := range []string{
		"Plan of 50-app: 0 to add, 1 to change, 0 to destroy",
		"values.image.tag: 1.0.0 → 1.1.0",
		"http://localhost:8088/api/healthz answered 200",
		"",
	} {
		if out := scrub.Text(in); out != in {
			t.Errorf("ordinary text changed: %q became %q", in, out)
		}
	}
}
