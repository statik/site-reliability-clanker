package setup

import (
	"context"
	"strings"
	"testing"
)

func TestTaggedLeftSkipsWhatWaitsForItsDeletion(t *testing.T) {
	instance := "arn:aws:ec2:us-west-2:111122223333:instance/i-0123"
	key := "arn:aws:kms:us-west-2:111122223333:key/0f1c"
	database := "arn:aws:rds:us-west-2:111122223333:db:clanker"
	cli := &fakeCLI{answers: map[string]string{
		"resourcegroupstaggingapi get-resources": `["` + instance + `","` + key + `","` +
			database + `"]`,
		"ec2 describe-instances": `["terminated"]`,
		"kms describe-key":       `"PendingDeletion"`,
	}}
	r := &installRun{in: InstallInput{Env: "fixture"},
		aws: AWS{Profile: "p", Region: "us-west-2", Env: "fixture", Run: cli.run},
		out: &lineWriter{send: func(string) {}}}
	left, err := r.taggedLeft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || !strings.Contains(left[0], database) {
		t.Errorf("left = %q, want only the database", left)
	}
	cli.answers["ec2 describe-instances"] = `["running"]`
	if left, _ := r.taggedLeft(context.Background()); len(left) != 2 {
		t.Errorf("left = %q, want the running instance and the database", left)
	}
}
