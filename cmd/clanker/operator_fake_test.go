package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeTofu stands in for tofu in the tests of bootstrap and destroy. It keeps state: init
// records the backend in the directory for the working data, as tofu does, and refuses a
// different backend there unless it migrates. An s3 state is a file in $FAKE_DIR/s3, a
// local state is the file of the path. apply writes $FAKE_DIR/applied.json to the state,
// destroy empties it, `state pull` prints it (or an empty state), and a state with the word
// unreadable cannot be read. Each call adds a line "<stack>|<backend of the stack>|<args>"
// to $FAKE_DIR/calls.
const fakeTofu = `#!/bin/sh
own=own-backend
if [ -f backend_override.tf ]; then own=local-backend; fi
echo "$(basename "$PWD")|$own|$*" >>"$FAKE_DIR/calls"
echo "${AWS_PROFILE-unset}|${AWS_ACCESS_KEY_ID-unset}" >"$FAKE_DIR/session"
data="${TF_DATA_DIR:-.terraform}"
recorded="$data/fake-backend"
state_file() {
  case "$1" in
  local:*) echo "${1#local:}" ;;
  s3:*) echo "$FAKE_DIR/s3/$(echo "${1#s3:}" | tr / _)" ;;
  esac
}
current="$(cat "$recorded" 2>/dev/null)"
case "$1" in
init)
  target=""
  migrate=no
  for arg in "$@"; do
    case "$arg" in
    -backend-config=key=*) target="s3:${arg#-backend-config=key=}" ;;
    -backend-config=path=*) target="local:${arg#-backend-config=path=}" ;;
    -migrate-state) migrate=yes ;;
    esac
  done
  mkdir -p "$data"
  if [ "$migrate" = yes ]; then
    from="$(state_file "$current")"
    to="$(state_file "$target")"
    mkdir -p "$(dirname "$to")"
    if [ -f "$from" ]; then cp "$from" "$to"; fi
  elif [ -n "$current" ] && [ "$current" != "$target" ]; then
    echo "Error: Backend configuration changed" >&2
    exit 1
  fi
  echo "$target" >"$recorded"
  ;;
apply)
  file="$(state_file "$current")"
  mkdir -p "$(dirname "$file")"
  cp "$FAKE_DIR/applied.json" "$file"
  ;;
destroy)
  echo '{"version":4,"resources":[]}' >"$(state_file "$current")"
  ;;
state)
  file="$(state_file "$current")"
  if [ ! -f "$file" ]; then
    echo '{"version":4,"serial":0,"resources":[]}'
  elif grep -q unreadable "$file"; then
    echo "Error: Failed to load state" >&2
    exit 1
  else
    cat "$file"
  fi
  ;;
output) cat "$FAKE_DIR/outputs.json" ;;
esac
`

// appliedState is the state that an apply with fakeTofu writes: a resource of the state
// bucket and one more.
const appliedState = `{"version":4,"resources":[
  {"module":"module.state_bucket","mode":"managed","type":"aws_s3_bucket","name":"this",
   "instances":[{}]},
  {"mode":"managed","type":"aws_iam_role","name":"planner","instances":[{}]}]}`

// stateWithoutBucket is a local state of 00-bootstrap from an apply that stopped before the
// state bucket existed.
const stateWithoutBucket = `{"version":4,"resources":[
  {"mode":"managed","type":"aws_iam_policy","name":"boundary","instances":[{}]}]}`

// printedOutputs is what `tofu output -json` prints for 00-bootstrap in the fake: the
// outputs of the layer and one sensitive output, which the setting leaves out.
const printedOutputs = `{
  "admin_secret": {"sensitive": true, "type": "string", "value": "secret-from-tofu"},
  "clanker_ttl": {"sensitive": false, "type": "string", "value": "168"},
  "evidence_bucket": {"sensitive": false, "type": "string", "value": "evidence"},
  "identities": {"sensitive": false, "type": ["map", "string"],
    "value": {"planner": "arn:aws:iam::000000000000:role/clanker/planner"}},
  "permission_guard": {"sensitive": false, "type": "string", "value": "guard"},
  "state_backend": {"sensitive": false, "type": ["map", "string"], "value": {
    "bucket": "state", "encrypt": "true", "region": "us-east-1", "use_lockfile": "true"}},
  "workload_role_path": {"sensitive": false, "type": "string", "value": "/clanker/"}
}`

// operatorFake is a stand-in for tofu and for the aws program, with a checkout and a data
// directory.
type operatorFake struct {
	t       *testing.T
	dir     string
	source  string
	dataDir string
	// stateInBucket is what head-object reports for the state of 00-bootstrap.
	stateInBucket bool
	awsCalls      []string
	// put has the arguments of put-parameter, and value the content of the value file.
	put   []string
	value string
}

func newOperatorFake(t *testing.T) *operatorFake {
	t.Helper()
	bin := t.TempDir()
	f := &operatorFake{t: t, dir: t.TempDir(), source: t.TempDir(), dataDir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(bin, "tofu"), []byte(fakeTofu), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DIR", f.dir)
	t.Setenv("TF_DATA_DIR", "")
	os.Unsetenv("TF_DATA_DIR")
	for name, text := range map[string]string{"applied.json": appliedState,
		"outputs.json": printedOutputs} {
		f.write(filepath.Join(f.dir, name), text)
	}
	for _, layer := range []string{"aws/00-bootstrap", "aws/10-network", "aws/20-cluster",
		"aws/30-data", "aws/40-platform", "aws/50-app", "local/00-bootstrap"} {
		f.write(filepath.Join(f.source, "platform", "stacks", layer, "main.tf"), "# stack\n")
	}
	return f
}

func (f *operatorFake) write(path, text string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *operatorFake) options() options {
	return options{dataDir: f.dataDir, sourceDir: f.source, source: os.DirFS(f.source),
		env: "test-env", cloud: "aws", profile: "test-profile", region: "us-east-1",
		brokerPrincipal: "arn:aws:iam::000000000000:role/test-core",
		parameterPath:   "/src/test", kmsKey: "alias/test"}
}

// aws stands in for the aws program.
func (f *operatorFake) aws(_ context.Context, args ...string) ([]byte, error) {
	f.awsCalls = append(f.awsCalls, strings.Join(args, " "))
	switch args[0] + " " + args[1] {
	case "sts get-caller-identity":
		return []byte(`{"Account":"000000000000","Arn":"arn:aws:iam::000000000000:user/t"}`), nil
	case "s3api head-object":
		if f.stateInBucket {
			return []byte(`{}`), nil
		}
		return nil, errors.New("aws s3api head-object: exit status 254: An error occurred " +
			"(404) when calling the HeadObject operation: Not Found")
	case "ssm put-parameter":
		f.put = args
		index := slices.Index(args, "--value")
		value, err := os.ReadFile(strings.TrimPrefix(args[index+1], "file://"))
		if err != nil {
			return nil, err
		}
		f.value = string(value)
		return []byte(`{"Version":1}`), nil
	}
	return nil, errors.New("the fake aws program has no " + args[0] + " " + args[1])
}

// calls returns the lines that fakeTofu wrote.
func (f *operatorFake) calls() []string {
	f.t.Helper()
	recorded, err := os.ReadFile(filepath.Join(f.dir, "calls"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(recorded)), "\n")
}

// callsOf returns the calls of the stack of a layer whose arguments start with prefix.
func (f *operatorFake) callsOf(layer, prefix string) []string {
	found := []string{}
	for _, call := range f.calls() {
		parts := strings.SplitN(call, "|", 3)
		if parts[0] == layer && strings.HasPrefix(parts[2], prefix) {
			found = append(found, call)
		}
	}
	return found
}

// s3Path is the file of the fake bucket that holds the state of a layer.
func (f *operatorFake) s3Path(layer string) string {
	return filepath.Join(f.dir, "s3", "test-env_"+layer+".tfstate")
}

// localState is the state file of 00-bootstrap in the state directory.
func (f *operatorFake) localState() string {
	return filepath.Join(f.dataDir, "state", "test-env", "00-bootstrap.tfstate")
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}
