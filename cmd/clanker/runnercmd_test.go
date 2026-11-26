package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/dispatch"
	"github.com/statik/site-reliability-clanker/internal/jobspec"
	"github.com/statik/site-reliability-clanker/internal/sim"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// tokenLauncher starts no runner. It hands the token of the job to the test.
type tokenLauncher chan string

func (l tokenLauncher) Launch(_ context.Context, token string) error {
	l <- token
	return nil
}

func TestARefusedRequestOfARunnerIsInTheAuditLogWithoutTheToken(t *testing.T) {
	db := storetest.Open(t)
	clock, err := sim.NewClock(db)
	if err != nil {
		t.Fatal(err)
	}
	log := audit.New(db, clock)
	tokens := make(tokenLauncher, 1)
	d := dispatch.New(tokens)
	d.Refused = recordRefusal(log)
	server := httptest.NewServer(d.Handler())
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, _ = d.Run(ctx, jobspec.Job{Spec: jobspec.Spec{Kind: jobspec.KindApply,
			Env: "example", Cloud: "aws", Layer: "20-cluster", CheckoutID: "WI-0007/abc"}})
	}()
	token := <-tokens
	request, err := http.NewRequest(http.MethodPost, server.URL+"/runner/v1/job/result",
		strings.NewReader(`{"status":"ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("a result with the token alone got status %d, want 403", response.StatusCode)
	}
	events, err := log.Query(audit.Filter{Types: []string{"runner.refused"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("the audit log has %d refusals, want 1: %+v", len(events), events)
	}
	event := events[0]
	if event.Actor != "dispatcher" || event.Env != "example" || event.Layer != "20-cluster" ||
		event.Data["request"] != "POST /runner/v1/job/result" ||
		event.Data["checkout"] != "WI-0007/abc" ||
		event.Data["reason"] != "the job was not fetched yet" {
		t.Errorf("event = %+v", event)
	}
	if strings.Contains(event.Summary, token) {
		t.Errorf("the event holds the token: %s", event.Summary)
	}
}
