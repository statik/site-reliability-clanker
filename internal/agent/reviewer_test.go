package agent

import (
	"strings"
	"testing"
)

func TestParseReviewAcceptsOnlyTheAgreedObject(t *testing.T) {
	good := map[string]string{
		`{"tier":"T3","concerns":["aws_db_instance.main: backup_retention_period goes to 0"]}`: "T3",
		"```json\n{\"tier\":\"T2\",\"concerns\":[]}\n```":                                      "T2",
		`  {"tier":"T0","concerns":[]}  `:                                                      "T0",
		`{"concerns":["the order of the fields is free"],"tier":"T1"}`:                         "T1",
		"{\"tier\":\"T3\",\"concerns\":[]}\n\n":                                                "T3",
		// The text around the first object is ignored.
		`{"tier":"T3","concerns":[]} The change is safe.`:          "T3",
		`{"tier":"T3","concerns":[]}{"tier":"T0"}`:                 "T3",
		`{"tier":"T3","concerns":[]} {`:                            "T3",
		"```json\n{\"tier\":\"T3\",\"concerns\":[]}\n```\nA note.": "T3",
		"```\n{\"tier\":\"T3\",\"concerns\":[]}\n```":              "T3",
		`Here: {"tier":"T3","concerns":[]}`:                        "T3",
	}
	for content, want := range good {
		review, err := parseReview(content)
		if err != nil || review.Tier != want || review.Concerns == nil || review.Source != "" {
			t.Errorf("parseReview(%q) = %+v, %v; want tier %s", content, review, err, want)
		}
	}
	notObject, notTier := "did not return the JSON object", "is not one of"
	bad := map[string]string{
		`The change looks safe.`:                         notObject,
		``:                                               notObject,
		`{"tier":"T4","concerns":[]}`:                    notTier,
		`{"tier":"low","concerns":[]}`:                   notTier,
		`{"tier":"t3","concerns":[]}`:                    notTier,
		`{"concerns":["no tier"]}`:                       notTier,
		`{"tier":null,"concerns":[]}`:                    notTier,
		`{"tier":3,"concerns":[]}`:                       notObject,
		`{"tier":"T2","concerns":[],"approved":true}`:    notObject,
		`{"tier":"T2","concerns":"one string, no list"}`: notObject,
		`{"tier":"T2","concerns":[1]}`:                   notObject,
		`{"tier":"T2","concerns":[null]}`:                notObject,
		`{"tier":"T2","concerns":{}}`:                    notObject,
		// A first object in another form is not saved by a second object.
		`{"tier":"T3","concerns":[],"approved":true} {"tier":"T3","concerns":[]}`: notObject,
		// The code sets the source. The model does not.
		`{"tier":"T3","concerns":[],"source":"a model"}`: notObject,
		// The names of the fields are exact, and each field is there once.
		`{"Tier":"T3","concerns":[]}`:               notObject,
		`{"tier":"T3","CONCERNS":[]}`:               notObject,
		`{"tier":"T3","tier":"T0","concerns":[]}`:   notObject,
		`{"tier":"T3","concerns":[],"concerns":[]}`: notObject,
		`{"tier":"T3","concerns":null}`:             notObject,
		`{"tier":"T3"}`:                             notObject,
		// Not one whole object.
		`{"tier":"T3","concerns":[]`: notObject,
		`null`:                       notObject,
		`[]`:                         notObject,
	}
	for content, want := range bad {
		_, err := parseReview(content)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("parseReview(%q): error %v, want one with %q", content, err, want)
		}
	}
}
