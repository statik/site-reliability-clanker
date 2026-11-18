package scrub_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
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

func TestTextRemovesSecretsByShape(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"sentence with is", "the new database password is cnry-prose-3307-hd",
			"the new database password is [REDACTED]"},
		{"sentence with was", "the old admin credential was cnry-old-8841-rz and must be rotated",
			"the old admin credential was [REDACTED] and must be rotated"},
		{"sentence with now", "The API key is now Zx81kq.", "The API key is now [REDACTED]"},
		{"sentence in JSON", `{"line":"the token is ab12cd34"}`,
			`{"line":"the token is [REDACTED]"}`},
		{"redis-cli", "redis-cli -h redis.paperboat.svc -a cnry-arg-2290-vv ping",
			"redis-cli -h redis.paperboat.svc -a [REDACTED] ping"},
		{"redis-cli keeps the port", "redis-cli -p 6379 -a hunter2 ping",
			"redis-cli -p 6379 -a [REDACTED] ping"},
		{"mysql joined", "mysqldump -uroot -phunter2 paperboat",
			"mysqldump -uroot -p[REDACTED] paperboat"},
		{"sshpass", "sshpass -p hunter2 ssh ops@host", "sshpass -p [REDACTED] ssh ops@host"},
		{"long flag", "tool login --password hunter2 --verbose",
			"tool login --password [REDACTED] --verbose"},
		{"long flag with prefix", "tool --access-token abc123", "tool --access-token [REDACTED]"},
		{"long flag with =", "tool --secret=abc123", "tool --secret=[REDACTED]"},
	}
	for _, tc := range cases {
		if got := scrub.Text(tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTextKeepsOrdinarySentencesAndCommands(t *testing.T) {
	for _, in := range []string{
		"the password is required",
		"the token is expired and the secret is rotated every 90 days",
		"the token is 5 minutes old",
		"The credential was rotated on 2026-03-02.",
		"grep -a file",
		"ps -p 1234",
		"tar -a -cf out.tar.gz dir",
		"cp -a source target",
		"mkdir -p /var/lib/data1",
		"ssh -p 2222 ops@host",
		"redis-cli -p 6379 ping",
		"mysql -h db1 -P 3306 paperboat",
		"redis-cli ping | grep -a PONG1",
		"tool --password --verbose",
		"tool --token-ttl 30m",
		"| mail relay | relay.example.com | 587 | cnry-bare-7719-qk |",
	} {
		if out := scrub.Text(in); out != in {
			t.Errorf("ordinary text changed: %q became %q", in, out)
		}
	}
}

func TestTextRemovesKnownValues(t *testing.T) {
	t.Cleanup(scrub.Forget)
	scrub.Add("cnry-bare-7719-qk", "abcdefgh12", "gh12345678", "short",
		"outer-inner-value-end", "inner-value", `qu"ote<and>&back\slash`)
	cases := []struct{ name, in, want string }{
		{"bare table cell", "| relay | 587 | cnry-bare-7719-qk |", "| relay | 587 | [REDACTED] |"},
		{"inside a word", "xcnry-bare-7719-qky", "x[REDACTED]y"},
		{"two times", "cnry-bare-7719-qk cnry-bare-7719-qk", "[REDACTED] [REDACTED]"},
		{"two values that overlap", "see abcdefgh12345678 here", "see [REDACTED] here"},
		{"a value inside a value", "see outer-inner-value-end here", "see [REDACTED] here"},
		{"a value below the floor", "a short word", "a short word"},
		{"raw form", `it is qu"ote<and>&back\slash.`, "it is [REDACTED]."},
		{"no value", "nothing here", "nothing here"},
	}
	for _, tc := range cases {
		if got := scrub.Text(tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTextRemovesTheJSONFormOfAKnownValue(t *testing.T) {
	t.Cleanup(scrub.Forget)
	value := `qu"ote<and>&back\slash`
	scrub.Add(value)
	encoded, err := json.Marshal(map[string]string{"line": value})
	if err != nil {
		t.Fatal(err)
	}
	if got := scrub.Text(string(encoded)); got != `{"line":"[REDACTED]"}` {
		t.Errorf("the JSON form stays: %s", got)
	}
}

func TestAddIgnoresValuesBelowTheFloor(t *testing.T) {
	t.Cleanup(scrub.Forget)
	scrub.Add("", "1234567", strings.Repeat("a", scrub.MinKnownLength-1))
	in := "port 1234567 and aaaaaaa"
	if out := scrub.Text(in); out != in {
		t.Errorf("a short value was replaced: %q", out)
	}
	scrub.Add(strings.Repeat("a", scrub.MinKnownLength))
	if out := scrub.Text("x aaaaaaaa y"); out != "x [REDACTED] y" {
		t.Errorf("a value at the floor was not replaced: %q", out)
	}
}

func TestAddAndTextRunConcurrently(t *testing.T) {
	t.Cleanup(scrub.Forget)
	var group sync.WaitGroup
	for i := range 16 {
		group.Add(2)
		go func() {
			defer group.Done()
			scrub.Add(fmt.Sprintf("known-value-%04d", i))
		}()
		go func() {
			defer group.Done()
			scrub.Text("the text has known-value-0003 and more")
		}()
	}
	group.Wait()
	if out := scrub.Text("known-value-0003 known-value-0015"); out != "[REDACTED] [REDACTED]" {
		t.Errorf("a value that was added concurrently stays: %q", out)
	}
}
