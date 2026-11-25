package main

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// setSignIn gives the settings of a sign-in with the admin admin@example.com and the
// approvers of the value.
func setSignIn(t *testing.T, approvers string) {
	t.Helper()
	t.Setenv(issuerEnv, "https://idp.example.com")
	t.Setenv(clientIDEnv, "clanker")
	t.Setenv(clientSecretEnv, "secret")
	t.Setenv(publicURLEnv, "https://clanker.example.com")
	t.Setenv(adminEmailEnv, "admin@example.com")
	t.Setenv(approverEmailsEnv, approvers)
}

func TestTheApproversAreReadWithTheSignIn(t *testing.T) {
	setSignIn(t, " Approver@Example.com,second@example.com ")
	settings, err := loadSignIn(options{cloud: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"approver@example.com", "second@example.com"}
	if !slices.Equal(settings.approverEmails, want) {
		t.Fatalf("approvers = %q, want %q", settings.approverEmails, want)
	}
}

func TestNoApproverSettingNamesNobody(t *testing.T) {
	for _, value := range []string{"", "  "} {
		setSignIn(t, value)
		settings, err := loadSignIn(options{cloud: "aws"})
		if err != nil || len(settings.approverEmails) != 0 {
			t.Fatalf("%q: approvers = %q, error = %v", value, settings.approverEmails, err)
		}
	}
}

func TestApproversThatAreNotValidStopTheStart(t *testing.T) {
	cases := map[string]string{
		"approver@example.com,":                    "entry 2 is empty",
		"approver@example.com,,second@example.com": "entry 2 is empty",
		"approver":                                  `"approver" is not an email address`,
		"Second Approver <second@example.com>":      "is not an email address",
		"<second@example.com>":                      "is not an email address",
		"approver@example.com second@example.com":   "is not an email address",
		"approver@example.com;second@example.com":   "is not an email address",
		"admin@example.com":                         "admin@example.com is the admin",
		"approver@example.com,ADMIN@example.com":    "admin@example.com is the admin",
		"approver@example.com,Approver@Example.com": "approver@example.com comes twice",
	}
	for value, want := range cases {
		setSignIn(t, value)
		_, err := loadSignIn(options{cloud: "aws"})
		if err == nil || !strings.Contains(err.Error(), approverEmailsEnv) ||
			!strings.Contains(err.Error(), want) {
			t.Errorf("%q: error = %v, want one with %q", value, err, want)
		}
	}
}

func TestApproversWithoutTheSignInStopTheStart(t *testing.T) {
	clearChat(t)
	t.Setenv(approverEmailsEnv, "approver@example.com")
	_, err := loadSignIn(options{cloud: "local"})
	if err == nil || !strings.Contains(err.Error(), approverEmailsEnv) ||
		!strings.Contains(err.Error(), issuerEnv) {
		t.Fatalf("error = %v, want a refusal that names both settings", err)
	}
}

func TestTheApproversComeFromParameterStore(t *testing.T) {
	setSignIn(t, "")
	t.Setenv(parameterPathEnv, "/src/prod")
	t.Setenv(regionEnv, "eu-west-1")
	store := &oneStore{parameters: []types.Parameter{{
		Name: aws.String("/src/prod/" + approverEmailsEnv), Value: aws.String("approver@example.com")}}}
	if err := loadParameters(context.Background(), options{cloud: "aws"}, store.open); err != nil {
		t.Fatal(err)
	}
	settings, err := loadSignIn(options{cloud: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(settings.approverEmails, []string{"approver@example.com"}) {
		t.Fatalf("approvers = %q, %s = %q", settings.approverEmails, approverEmailsEnv,
			os.Getenv(approverEmailsEnv))
	}
}

func TestTheApproversAreMadeOnceAndAUserKeepsTheRole(t *testing.T) {
	db := storetest.Open(t)
	if _, err := db.Exec(`INSERT INTO users (name, display, role, email) VALUES
		('dave', 'Dave Lindqvist', 'viewer', 'Dave@Example.com')`); err != nil {
		t.Fatal(err)
	}
	emails := []string{"approver@example.com", "dave@example.com"}
	for range 2 {
		if err := ensureAdmin(db, "Admin@Example.com"); err != nil {
			t.Fatal(err)
		}
		if err := ensureApprovers(db, emails); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.Query(`SELECT name, role, email FROM users ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := []string{}
	for rows.Next() {
		var name, role, email string
		if err := rows.Scan(&name, &role, &email); err != nil {
			t.Fatal(err)
		}
		got = append(got, name+" "+role+" "+email)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"admin@example.com admin admin@example.com",
		"approver@example.com approver approver@example.com", "dave viewer Dave@Example.com"}
	if !slices.Equal(got, want) {
		t.Fatalf("users = %q, want %q", got, want)
	}
}
