package awsrunner_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/awsrunner"
)

const exampleSetting = `{"cluster": "runners", "task_definition": "clanker-runner:3",
	"container": "runner", "subnets": ["subnet-0123456789abcdef0"],
	"security_groups": ["sg-0123456789abcdef0"]}`

func TestParseConfigReadsTheSetting(t *testing.T) {
	config, err := awsrunner.ParseConfig(exampleSetting)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config, exampleConfig()) {
		t.Fatalf("config = %#v, want %#v", config, exampleConfig())
	}
}

func TestParseConfigRefusesASettingThatIsNotValid(t *testing.T) {
	cases := []struct{ text, want string }{
		{"", "is not a JSON object"},
		{"[]", "is not a JSON object"},
		{strings.Replace(exampleSetting, `"cluster"`, `"region": "eu-west-1", "cluster"`, 1),
			`unknown field "region"`},
		{exampleSetting + `{}`, "more text after its JSON object"},
		{strings.Replace(exampleSetting, `"runners"`, `""`, 1), "has no cluster"},
		{strings.Replace(exampleSetting, `"clanker-runner:3"`, `""`, 1), "has no task_definition"},
		{strings.Replace(exampleSetting, `"runner",`, `"",`, 1), "has no container"},
		{strings.Replace(exampleSetting, `["subnet-0123456789abcdef0"]`, `[]`, 1),
			"names no subnet in subnets"},
		{strings.Replace(exampleSetting, `["sg-0123456789abcdef0"]`, `null`, 1),
			"names no security group in security_groups"},
		{strings.Replace(exampleSetting, `["subnet-0123456789abcdef0"]`,
			`["subnet-0123456789abcdef0", ""]`, 1),
			`item 2 of subnets in the runner setting is "", which is not the ID of a subnet`},
		{strings.Replace(exampleSetting, `["sg-0123456789abcdef0"]`,
			`["subnet-0123456789abcdef0"]`, 1), "not the ID of a security group"},
	}
	for _, c := range cases {
		_, err := awsrunner.ParseConfig(c.text)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: error = %v, want %q", c.text, err, c.want)
		}
	}
}
