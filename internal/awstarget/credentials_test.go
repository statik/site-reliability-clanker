package awstarget

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go-v2/service/sts/types"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
)

const (
	plannerRole = "arn:aws:iam::111122223333:role/clanker/clanker-example-planner"
	applierRole = "arn:aws:iam::111122223333:role/clanker/clanker-example-applier-30-data"
	secretKey   = "example-secret-access-key"
	token       = "example-session-token"
)

var expiry = time.Date(2026, 11, 21, 2, 30, 0, 0, time.UTC)

// fakeSTS answers AssumeRole with fixed keys and keeps the request.
type fakeSTS struct {
	in  *sts.AssumeRoleInput
	err error
}

func (f *fakeSTS) AssumeRole(_ context.Context, in *sts.AssumeRoleInput,
	_ ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	f.in = in
	if f.err != nil {
		return nil, f.err
	}
	return &sts.AssumeRoleOutput{Credentials: &types.Credentials{
		AccessKeyId: aws.String("ASIAEXAMPLE"), SecretAccessKey: aws.String(secretKey),
		SessionToken: aws.String(token), Expiration: aws.Time(expiry)}}, nil
}

// recorder keeps the audit events.
type recorder struct {
	events []audit.Event
	err    error
}

func (r *recorder) record(e audit.Event) error {
	r.events = append(r.events, e)
	return r.err
}

func issuer(client STS, r *recorder) Credentials {
	return Credentials{STS: client, Record: r.record,
		Roles: map[string]string{"planner": plannerRole, "applier-30-data": applierRole}}
}

func applyGrant() broker.Grant {
	return broker.Grant{ID: "G-9f3c1e7a2b4d", WorkItem: "WI-0142", Env: "example",
		Layer: "30-data", Identity: "applier-30-data", Kind: broker.KindApply,
		Commit: "9f3c1e7", PlanSHA256: "5b1d0123456789abcdefe02a",
		Approvers: []string{"approver.one@example.com", "approver-two"}}
}

func tagsOf(in *sts.AssumeRoleInput) map[string]string {
	tags := map[string]string{}
	for _, tag := range in.Tags {
		tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return tags
}

func TestIssueAsksForTheRoleOfTheIdentityWithTheNameAndTheTagsOfTheGrant(t *testing.T) {
	client, events := &fakeSTS{}, &recorder{}
	if _, err := issuer(client, events).Issue(context.Background(), applyGrant(),
		20*time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := aws.ToString(client.in.RoleArn); got != applierRole {
		t.Fatalf("role = %q, want the role of applier-30-data", got)
	}
	if got := aws.ToString(client.in.RoleSessionName); got != "WI-0142-G-9f3c1e7a2b4d" {
		t.Fatalf("session name = %q", got)
	}
	if client.in.Policy != nil || len(client.in.PolicyArns) != 0 {
		t.Fatal("the request has a session policy, which is not built")
	}
	want := map[string]string{"clanker_grant": "G-9f3c1e7a2b4d", "clanker_env": "example",
		"clanker_layer": "30-data", "clanker_work_item": "WI-0142", "clanker_plan": "5b1d01234567"}
	got := tagsOf(client.in)
	if len(got) != len(want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("tag %s = %q, want %q", key, got[key], value)
		}
	}
}

func TestAPlanGrantGetsTheReadRoleAndNoEmptyTag(t *testing.T) {
	client := &fakeSTS{}
	g := broker.Grant{ID: "G-000000000001", WorkItem: "WI-0007", Env: "example",
		Layer: "30-data", Identity: "planner", Kind: broker.KindPlan}
	if _, err := issuer(client, &recorder{}).Issue(context.Background(), g,
		15*time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := aws.ToString(client.in.RoleArn); got != plannerRole {
		t.Fatalf("role = %q, want the role of the planner", got)
	}
	tags := tagsOf(client.in)
	if _, found := tags["clanker_plan"]; found || len(tags) != 4 {
		t.Fatalf("tags = %v: a plan grant has no plan hash, and STS refuses an empty tag", tags)
	}
}

func TestAnIdentityWithoutARoleGetsNoSession(t *testing.T) {
	client := &fakeSTS{}
	g := applyGrant()
	g.Identity = "runbook"
	_, err := issuer(client, &recorder{}).Issue(context.Background(), g, 15*time.Minute)
	if err == nil || !strings.Contains(err.Error(), "no role for the identity runbook") {
		t.Fatalf("error = %v", err)
	}
	if client.in != nil {
		t.Fatal("STS was called")
	}
}

func TestTheSessionLivesAsLongAsTheGrantInsideTheLimitsOfSTS(t *testing.T) {
	cases := []struct {
		remaining time.Duration
		want      int32
	}{
		{-time.Second, 900},
		{time.Second, 900},
		{899 * time.Second, 900},
		{900 * time.Second, 900},
		{901 * time.Second, 901},
		{20*time.Minute + 900*time.Millisecond, 1200},
		{time.Hour - 2*time.Second, 3598},
		{time.Hour, 3600},
		{2 * time.Hour, 3600},
	}
	for _, c := range cases {
		client := &fakeSTS{}
		if _, err := issuer(client, &recorder{}).Issue(context.Background(), applyGrant(),
			c.remaining); err != nil {
			t.Fatal(err)
		}
		if got := aws.ToInt32(client.in.DurationSeconds); got != c.want {
			t.Errorf("remaining %s: DurationSeconds = %d, want %d", c.remaining, got, c.want)
		}
	}
}

func TestTheSessionNameHasOnlyAllowedCharactersAndTheWholeGrantID(t *testing.T) {
	allowed := regexp.MustCompile(`^[A-Za-z0-9_.-]{2,64}$`)
	cases := []struct{ workItem, id, want string }{
		{"WI-0142", "G-9f3c1e7a2b4d", "WI-0142-G-9f3c1e7a2b4d"},
		{"", "G-9f3c1e7a2b4d", "G-9f3c1e7a2b4d"},
		{"one@example.com fix/it", "G-1", "one-example.com-fix-it-G-1"},
		{strings.Repeat("w", 80), "G-9f3c1e7a2b4d", strings.Repeat("w", 49) + "-G-9f3c1e7a2b4d"},
		{"WI-1", strings.Repeat("g", 70), strings.Repeat("g", 64)},
	}
	for _, c := range cases {
		got := SessionName(broker.Grant{ID: c.id, WorkItem: c.workItem})
		if got != c.want || !allowed.MatchString(got) {
			t.Errorf("SessionName(%q, %q) = %q, want %q", c.workItem, c.id, got, c.want)
		}
	}
}

func TestTheCredentialHasTheKeysAndTheExpiryOfSTS(t *testing.T) {
	cred, err := issuer(&fakeSTS{}, &recorder{}).Issue(context.Background(), applyGrant(),
		20*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if cred.AWS == nil || cred.AWS.AccessKeyID != "ASIAEXAMPLE" ||
		cred.AWS.SecretAccessKey != secretKey || cred.AWS.SessionToken != token {
		t.Fatalf("credential = %v", cred.AWS)
	}
	if !cred.AWS.Expires.Equal(expiry) {
		t.Fatalf("expires = %s, want %s", cred.AWS.Expires, expiry)
	}
	if cred.KubeConfig != "" {
		t.Fatal("an STS session has no kubeconfig")
	}
}

// leaks fails the test when text holds a secret part of the session, or an approver.
func leaks(t *testing.T, where, text string) {
	t.Helper()
	for _, secret := range []string{secretKey, token, "approver", "@"} {
		if strings.Contains(text, secret) {
			t.Errorf("%s holds %q: %s", where, secret, text)
		}
	}
}

func TestNoSecretAndNoApproverIsInTheAuditLogTheMechanismOrALog(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(previous)

	client, events := &fakeSTS{}, &recorder{}
	g := applyGrant()
	cred, err := issuer(client, events).Issue(context.Background(), g, 20*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := issuer(client, events).Withdraw(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	slog.Info("credential", "aws", cred.AWS, "value", *cred.AWS)
	leaks(t, "the log", logged.String())
	leaks(t, "the mechanism", cred.Mechanism)
	leaks(t, "the printed credential", fmt.Sprintf("%v %+v %#v %s", cred.AWS, *cred.AWS,
		*cred.AWS, cred.AWS))
	leaks(t, "the request", fmt.Sprintf("%s %v", aws.ToString(client.in.RoleSessionName),
		tagsOf(client.in)))
	if len(events.events) != 2 {
		t.Fatalf("%d audit events, want one for the issue and one for the end", len(events.events))
	}
	for _, e := range events.events {
		leaks(t, "the audit event "+e.Type, fmt.Sprintf("%+v", e))
	}
	issued := events.events[0]
	want := map[string]any{"grant": g.ID, "role_arn": applierRole,
		"session_name": "WI-0142-G-9f3c1e7a2b4d", "expires": "2026-11-21T02:30:00Z"}
	if issued.Type != "credential.issued" || len(issued.Data) != len(want) {
		t.Fatalf("event = %+v", issued)
	}
	for key, value := range want {
		if issued.Data[key] != value {
			t.Errorf("data %s = %v, want %v", key, issued.Data[key], value)
		}
	}
}

func TestAFailureOfSTSIsAClearErrorWithNoCredential(t *testing.T) {
	client := &fakeSTS{err: errors.New("AccessDenied: not authorized to perform sts:AssumeRole")}
	events := &recorder{}
	cred, err := issuer(client, events).Issue(context.Background(), applyGrant(), time.Hour)
	if err == nil {
		t.Fatal("no error")
	}
	for _, part := range []string{applierRole, "G-9f3c1e7a2b4d", "AccessDenied", "sts:TagSession"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not name %q", err, part)
		}
	}
	leaks(t, "the error", err.Error())
	if cred.AWS != nil || len(events.events) != 0 {
		t.Fatalf("credential = %v, events = %d, want none", cred.AWS, len(events.events))
	}
}

func TestTheKeysAreDroppedWhenTheAuditLogDoesNotTakeTheEvent(t *testing.T) {
	events := &recorder{err: errors.New("the database is away")}
	cred, err := issuer(&fakeSTS{}, events).Issue(context.Background(), applyGrant(), time.Hour)
	if err == nil || !strings.Contains(err.Error(), "the database is away") {
		t.Fatalf("error = %v", err)
	}
	if cred.AWS != nil {
		t.Fatal("a credential came back without its audit event")
	}
}

func TestWithdrawRecordsThatTheSessionIsLeftToExpire(t *testing.T) {
	client, events := &fakeSTS{}, &recorder{}
	if err := issuer(client, events).Withdraw(context.Background(), applyGrant()); err != nil {
		t.Fatal(err)
	}
	if client.in != nil {
		t.Fatal("Withdraw called STS, which has no call that revokes a session")
	}
	if len(events.events) != 1 || events.events[0].Type != "credential.left_to_expire" ||
		events.events[0].Data["session_name"] != "WI-0142-G-9f3c1e7a2b4d" {
		t.Fatalf("events = %+v", events.events)
	}
	g := applyGrant()
	g.Identity = "runbook"
	if err := issuer(client, events).Withdraw(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if len(events.events) != 1 {
		t.Fatal("an identity without a role has no session to record")
	}
}

const assumeRoleAnswer = `<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <AssumeRoleResult>
    <Credentials>
      <AccessKeyId>ASIAEXAMPLE</AccessKeyId>
      <SecretAccessKey>` + secretKey + `</SecretAccessKey>
      <SessionToken>` + token + `</SessionToken>
      <Expiration>2026-11-21T02:30:00Z</Expiration>
    </Credentials>
  </AssumeRoleResult>
  <ResponseMetadata><RequestId>example</RequestId></ResponseMetadata>
</AssumeRoleResponse>`

const deniedAnswer = `<ErrorResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <Error><Type>Sender</Type><Code>AccessDenied</Code><Message>not authorized</Message></Error>
  <RequestId>example</RequestId>
</ErrorResponse>`

// standIn is an STS endpoint on this machine and a client of the SDK that calls it.
func standIn(t *testing.T, status int, answer string, form *url.Values) STS {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		*form = r.PostForm
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(status)
		fmt.Fprint(w, answer)
	}))
	t.Cleanup(server.Close)
	return sts.New(sts.Options{Region: "eu-west-1", BaseEndpoint: aws.String(server.URL),
		Credentials: credentials.NewStaticCredentialsProvider("AKIAEXAMPLE", "broker-key", "")})
}

func TestIssueSpeaksTheQueryProtocolOfSTS(t *testing.T) {
	var form url.Values
	client := standIn(t, http.StatusOK, assumeRoleAnswer, &form)
	cred, err := issuer(client, &recorder{}).Issue(context.Background(), applyGrant(),
		10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"Action": "AssumeRole", "RoleArn": applierRole,
		"RoleSessionName": "WI-0142-G-9f3c1e7a2b4d", "DurationSeconds": "900",
		"Tags.member.1.Key": "clanker_grant", "Tags.member.1.Value": "G-9f3c1e7a2b4d",
		"Tags.member.3.Key": "clanker_layer", "Tags.member.3.Value": "30-data"}
	for key, value := range want {
		if form.Get(key) != value {
			t.Errorf("form %s = %q, want %q", key, form.Get(key), value)
		}
	}
	if form.Has("Policy") {
		t.Error("the request has a session policy")
	}
	if cred.AWS.SessionToken != token || !cred.AWS.Expires.Equal(expiry) {
		t.Fatalf("credential = %v", cred.AWS)
	}
}

func TestARefusalOfSTSNamesTheCodeOfSTS(t *testing.T) {
	var form url.Values
	client := standIn(t, http.StatusForbidden, deniedAnswer, &form)
	_, err := issuer(client, &recorder{}).Issue(context.Background(), applyGrant(), time.Hour)
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("error = %v", err)
	}
	leaks(t, "the error", err.Error())
	if strings.Contains(err.Error(), "broker-key") {
		t.Fatalf("the error holds the key of the caller: %v", err)
	}
}
