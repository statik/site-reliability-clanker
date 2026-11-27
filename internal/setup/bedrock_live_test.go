package setup

import (
	"context"
	"os"
	"slices"
	"testing"
)

// TestLiveBedrockCheck runs the check of the step Language model against Amazon Bedrock with
// the AWS CLI and the profile of AWS_PROFILE in AWS_REGION: it lists the inference profiles,
// reads each default profile and calls each default model once with a few tokens. It
// changes nothing, and runs only with CLANKER_LIVE_BEDROCK=1.
func TestLiveBedrockCheck(t *testing.T) {
	if os.Getenv("CLANKER_LIVE_BEDROCK") != "1" {
		t.Skip("set CLANKER_LIVE_BEDROCK=1 to call Amazon Bedrock")
	}
	session := AWS{Profile: os.Getenv("AWS_PROFILE"), Region: os.Getenv("AWS_REGION"),
		Run: realCLI}
	profiles, err := session.InferenceProfiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	models := bedrockChoice(nil, nil)
	for _, id := range models {
		if !slices.ContainsFunc(profiles, func(p InferenceProfile) bool { return p.ID == id }) {
			t.Errorf("the region lists no profile %s", id)
		}
	}
	t.Logf("%d active profiles; the first is %s", len(profiles), profiles[0].ID)
	if err := session.CheckBedrock(context.Background(), models, func(line string) {
		t.Log(line)
	}); err != nil {
		t.Fatal(err)
	}
}
