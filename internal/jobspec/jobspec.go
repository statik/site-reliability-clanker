// Package jobspec describes a job that the core of the control plane gives to a runner, and
// the result that comes back. The types are plain values, so a job can cross a process or a
// network boundary. The package starts no program and reads no file.
package jobspec

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"time"
)

// Kinds of job.
const (
	// KindPlan makes a plan of one layer and returns it. It changes nothing.
	KindPlan = "plan"
	// KindApply applies one saved plan. It never makes a new plan.
	KindApply = "apply"
	// KindRunbook runs one script of the runbooks repository.
	KindRunbook = "runbook"
	// KindDiagnose runs the reads of a debug session and returns what they print.
	KindDiagnose = "diagnose"
)

// Result statuses.
const (
	StatusOK     = "ok"
	StatusFailed = "failed"
	// StatusStale means that the state changed after the plan was saved. Nothing was applied.
	StatusStale = "stale"
)

// ErrStalePlan means that the state changed after the plan was saved.
var ErrStalePlan = errors.New("the saved plan is stale")

// AWSSession holds the keys of one STS session: the AWS credential of one job, from the
// access broker. The JSON form carries the keys, because that is how a runner gets them.
// The text and log forms carry only the key ID and the expiry, so that a log line or an
// error message with a session in it holds no secret.
type AWSSession struct {
	AccessKeyID     string    `json:"access_key_id"`
	SecretAccessKey string    `json:"secret_access_key"`
	SessionToken    string    `json:"session_token"`
	Expires         time.Time `json:"expires"`
}

// String returns the key ID and the expiry.
func (s AWSSession) String() string {
	return "AWS session " + s.AccessKeyID + ", expires " + s.Expires.UTC().Format(time.RFC3339)
}

// GoString is what the verb %#v prints.
func (s AWSSession) GoString() string { return s.String() }

// LogValue is what a structured log prints.
func (s AWSSession) LogValue() slog.Value { return slog.StringValue(s.String()) }

// Spec is the part of a job that travels as JSON.
type Spec struct {
	Kind  string `json:"kind"`
	Env   string `json:"env"`
	Cloud string `json:"cloud,omitempty"`
	Layer string `json:"layer,omitempty"`
	// CheckoutID names the tree that the job works on. A plan and its apply have the same
	// ID, so they run in the same directory on the runner.
	CheckoutID string `json:"checkout_id,omitempty"`
	// KubeConfig is the text of the credential of this job, from the access broker. It is
	// in the specification of this job only.
	KubeConfig string `json:"kubeconfig,omitempty"`
	// AWS is the AWS credential of this job, from the access broker. It is nil when the
	// job has none. It is in the specification of this job only.
	AWS *AWSSession `json:"aws,omitempty"`
	// PlanSHA256 is the approved hash of the plan of an apply job.
	PlanSHA256 string `json:"plan_sha256,omitempty"`
	// Runbook is the name of the script of a runbook job, and Params are its parameters.
	Runbook string            `json:"runbook,omitempty"`
	Params  map[string]string `json:"params,omitempty"`
	// Namespace and Reads are the reads of a diagnose job: what to read, and where. The
	// job carries no command.
	Namespace string `json:"namespace,omitempty"`
	Reads     []Read `json:"reads,omitempty"`
	// Variables are added to the environment of OpenTofu, for example TF_VAR_admin_token.
	Variables map[string]string `json:"variables,omitempty"`
	// Backend holds the settings of the state backend, as `tofu init -backend-config`
	// takes them. It is empty on the local target, where the runner keeps the state.
	Backend map[string]string `json:"backend,omitempty"`
}

// LogValue is what a structured log prints: the specification without its credentials. A
// log handler would otherwise print the JSON form, which carries them.
func (s Spec) LogValue() slog.Value {
	// The type without methods stops the log from asking for the value again.
	type withoutCredentials Spec
	printed := withoutCredentials(s)
	printed.KubeConfig, printed.AWS = "", nil
	return slog.AnyValue(printed)
}

// Job is a specification with its large parts and the place for its log.
type Job struct {
	Spec
	// Checkout is the tree as a gzip-compressed tar file.
	Checkout []byte
	// Plan is the plan file of an apply job: the exact bytes that were approved.
	Plan []byte
	// Log receives what the job prints, while it runs.
	Log io.Writer
}

// CommandOutput is the result of one read of a diagnose job.
type CommandOutput struct {
	Output string `json:"output"`
	// Failure is empty when the read worked.
	Failure string `json:"failure,omitempty"`
}

// Result is what a runner reports for a job.
type Result struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	// ToolDigest identifies the tool versions of the runner that ran the job.
	ToolDigest string `json:"tool_digest,omitempty"`
	// PlanSHA256 is the hash that the runner computed for the plan of a plan job.
	PlanSHA256 string `json:"plan_sha256,omitempty"`
	// Outputs are the outputs of the layer after an apply job, or what the script reported
	// after a runbook job: each value of a runbook job is text.
	Outputs  map[string]any  `json:"outputs,omitempty"`
	Commands []CommandOutput `json:"commands,omitempty"`
	// Plan and PlanJSON are the plan file of a plan job and its JSON form. They travel
	// apart from the JSON of the result.
	Plan     []byte `json:"-"`
	PlanJSON []byte `json:"-"`
}

// SHA256 returns the hex SHA-256 of data.
func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
