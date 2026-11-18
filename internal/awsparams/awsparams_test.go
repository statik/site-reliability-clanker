package awsparams

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// fakeStore answers with its pages in order, and records each request.
type fakeStore struct {
	pages    [][]types.Parameter
	err      error
	requests []*ssm.GetParametersByPathInput
}

func (f *fakeStore) GetParametersByPath(_ context.Context, in *ssm.GetParametersByPathInput,
	_ ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	f.requests = append(f.requests, in)
	if f.err != nil {
		return nil, f.err
	}
	index := len(f.requests) - 1
	out := &ssm.GetParametersByPathOutput{Parameters: f.pages[index]}
	if index < len(f.pages)-1 {
		out.NextToken = aws.String("more")
	}
	return out, nil
}

func parameter(name, value string) types.Parameter {
	return types.Parameter{Name: aws.String(name), Value: aws.String(value)}
}

func TestLoadReturnsTheSettingsOfEveryPage(t *testing.T) {
	store := &fakeStore{pages: [][]types.Parameter{
		{parameter("/src/prod/CLANKER_DATABASE_URL", "postgres://db")},
		{parameter("/src/prod/OPENROUTER_API_KEY", "key")},
	}}
	settings, err := Load(context.Background(), store, "/src/prod/")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"CLANKER_DATABASE_URL": "postgres://db", "OPENROUTER_API_KEY": "key"}
	if !maps.Equal(settings, want) {
		t.Fatalf("settings = %v, want %v", settings, want)
	}
	first, second := store.requests[0], store.requests[1]
	if aws.ToString(first.Path) != "/src/prod" || !aws.ToBool(first.WithDecryption) {
		t.Fatalf("first request = path %q, decryption %v", aws.ToString(first.Path),
			aws.ToBool(first.WithDecryption))
	}
	if first.NextToken != nil || aws.ToString(second.NextToken) != "more" {
		t.Fatal("the second request did not continue from the first page")
	}
}

func TestLoadRefusesAPathThatIsNotValid(t *testing.T) {
	for _, path := range []string{"", "/", "src/prod", "//"} {
		store := &fakeStore{}
		if _, err := Load(context.Background(), store, path); err == nil {
			t.Errorf("path %q: no error", path)
		}
		if len(store.requests) != 0 {
			t.Errorf("path %q: the store was asked", path)
		}
	}
}

func TestLoadRefusesANameThatIsNotASetting(t *testing.T) {
	for _, name := range []string{"/src/prod/PATH", "/src/prod/CLANKER_", "/src/prod/sub/CLANKER_X",
		"/src/prod/clanker_database_url"} {
		store := &fakeStore{pages: [][]types.Parameter{{parameter(name, "x")}}}
		_, err := Load(context.Background(), store, "/src/prod")
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("parameter %s: error = %v, want a refusal that names it", name, err)
		}
	}
}

func TestLoadFailsWhenThePathHasNoParameter(t *testing.T) {
	store := &fakeStore{pages: [][]types.Parameter{{}}}
	_, err := Load(context.Background(), store, "/src/prod")
	if err == nil || !strings.Contains(err.Error(), "no parameter below /src/prod") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadReportsAFailureOfTheStoreWithThePermissionItNeeds(t *testing.T) {
	denied := errors.New("AccessDeniedException")
	_, err := Load(context.Background(), &fakeStore{err: denied}, "/src/prod")
	if !errors.Is(err, denied) || !strings.Contains(err.Error(), "ssm:GetParametersByPath") {
		t.Fatalf("error = %v", err)
	}
}
