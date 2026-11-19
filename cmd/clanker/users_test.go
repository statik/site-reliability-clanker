package main

import (
	"reflect"
	"testing"
)

func TestTheOperatorTakesThePlaceOfTheFirstDemoApprover(t *testing.T) {
	if got := localUsers(""); !reflect.DeepEqual(got, demoUsers) {
		t.Errorf("with no operator the users are %v", got)
	}
	users := localUsers("Sam Lee")
	names := []string{}
	for _, u := range users {
		names = append(names, u.Name)
	}
	if want := []string{"sam", "bob", "carol", "dave"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("users = %v, want %v", names, want)
	}
	first := users[0]
	if first.Display != "Sam Lee" || first.Role != "approver" || first.Email != "sam@example.com" ||
		!first.SelfDeclared || users[1].SelfDeclared {
		t.Errorf("the operator is %+v", first)
	}
}

func TestTheUserNameOfTheOperator(t *testing.T) {
	cases := map[string]string{
		"Sam Lee":     "sam",
		"  Zoë Lee":   "zo",
		"李 Wei":       "wei",
		"O'Neil, Pat": "oneil",
		"Alice":       "alice",
		"Bob Stone":   "bob1",
		"Carol":       "carol1",
		"李雷":          "operator",
		"R2 D2":       "r2",
	}
	for operator, want := range cases {
		if got := operatorUserName(operator); got != want {
			t.Errorf("operatorUserName(%q) = %q, want %q", operator, got, want)
		}
	}
}
