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
		{"prefixed name", "ADMIN_TOKEN=local-demo-token-123", "local-demo-token-123"},
		{"joined name", "PGPASSWORD=sup3rs3cret", "sup3rs3cret"},
		{"JSON key", `{"password":"jsonS3cret"}`, "jsonS3cret"},
		{"bearer token", "Authorization: Bearer opaque-token-value-9911", "opaque-token-value-9911"},
		{"URL password with a slash", "postgres://app:pa/ss/w0rd@db:5432/x", "pa/ss/w0rd"},
		{"other webhook", "https://outlook.example.com/webhook/abc123def", "abc123def"},
		{"mail password", "SMTP_PASS: cnry-smtp-1183-bf", "cnry-smtp-1183-bf"},
		{"GitHub token", "ghp_abcdefghijklmnopqrstuvwxyz0123456789", "abcdefghijklmnopqrstuvwxyz"},
		{"URL without a user name", "redis://:r3disS3cret@cache:6379", "r3disS3cret"},
		{"connection string", "Server=db;Uid=app;Pwd=c0nnS3cret;", "c0nnS3cret"},
		{"basic authorization", "Authorization: Basic dXNlcjpwYXNzd29yZA==", "dXNlcjpwYXNzd29yZA"},
		{"key name", "ADMIN_KEY=adm1nK3yValue", "adm1nK3yValue"},
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
		"PASS: 12/12 policy tests",
		"commit 9f3c1e7 plan_sha256 5b1de02a tier T2",
		"",
	} {
		if out := scrub.Text(in); out != in {
			t.Errorf("ordinary text changed: %q became %q", in, out)
		}
	}
}
