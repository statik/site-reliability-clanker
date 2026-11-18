package jobspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestAnAWSSessionTravelsAsJSONAndPrintsWithoutItsSecrets(t *testing.T) {
	session := AWSSession{AccessKeyID: "ASIAEXAMPLE", SecretAccessKey: "example-secret",
		SessionToken: "example-token", Expires: time.Date(2026, 11, 21, 2, 30, 0, 0, time.UTC)}
	spec := Spec{Kind: KindPlan, KubeConfig: "text-of-a-kubeconfig", AWS: &session}
	wire, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var back Spec
	if err := json.Unmarshal(wire, &back); err != nil {
		t.Fatal(err)
	}
	if back.AWS == nil || *back.AWS != session {
		t.Fatalf("after the wire the session is %v", back.AWS)
	}
	var logged bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logged, nil))
	logger.Info("job", "session", session, "pointer", &session, "spec", spec)
	if strings.Contains(logged.String(), spec.KubeConfig) {
		t.Errorf("the log holds the kubeconfig: %s", logged.String())
	}
	printed := fmt.Sprintf("%v %+v %#v %s %v %+v %#v", session, session, session, session,
		&session, spec, spec) + logged.String()
	for _, secret := range []string{session.SecretAccessKey, session.SessionToken} {
		if strings.Contains(printed, secret) {
			t.Errorf("the printed session holds %q: %s", secret, printed)
		}
	}
	if !strings.Contains(printed, "ASIAEXAMPLE") ||
		!strings.Contains(printed, "2026-11-21T02:30:00Z") {
		t.Errorf("the printed session does not name its key ID and expiry: %s", printed)
	}
	wire, err = json.Marshal(Spec{Kind: KindPlan})
	if err != nil || strings.Contains(string(wire), "aws") {
		t.Fatalf("a job without a session has the JSON %s, error %v", wire, err)
	}
}
