// Package dispatch gives jobs to runners and takes their results. It is the side of the
// core: it starts no program itself. A Launcher starts a runner, and the runner then talks
// to this package over HTTP with the token of its one job. The first fetch of the job gives
// the runner a runner key, and each later request must carry it as well.
package dispatch

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/internal/jobspec"
)

// Launcher starts a runner for one job. The runner gets the token and asks the core for
// the job.
//
// Launch can return when the runner started, or block until the runner ended. An error
// means that no runner will report for this job.
type Launcher interface {
	Launch(ctx context.Context, token string) error
}

// Environment variables that tell a runner where its core is and which job is its own.
// Each launcher gives them to the runner that it starts.
const (
	CoreURLEnv = "CLANKER_CORE_URL"
	TokenEnv   = "CLANKER_JOB_TOKEN"
)

const (
	// maxPlan bounds an uploaded plan file or plan JSON.
	maxPlan = 256 << 20
	// maxLog bounds one piece of a log.
	maxLog = 4 << 20
	// maxRefusals bounds the refused requests of one job that Dispatcher.Refused learns of,
	// so that a holder of a token cannot fill the audit log.
	maxRefusals = 10
)

// Refusal is a request with the token of a running job that the dispatcher refused. It
// holds no secret: no token, no runner key and no credential of the job.
type Refusal struct {
	// Kind, Env, Layer and CheckoutID name the job.
	Kind, Env, Layer, CheckoutID string
	// Request is the method and the path of the request.
	Request string
	// Remote is the address that the request came from.
	Remote string
	Reason string
}

// Dispatcher runs jobs on runners.
type Dispatcher struct {
	Launcher Launcher
	// Silence is how long a runner can say nothing before its job fails. A runner sends a
	// heartbeat every few seconds.
	Silence time.Duration
	// StartGrace is how long a runner has from its launch to its first request. A runner in
	// the cloud must first get a machine and pull its image. When it is 0, Silence counts
	// from the launch.
	StartGrace time.Duration
	// Refused, when it is set, learns of the first refused requests of each job that had
	// the job token: a second fetch of the job, or a request without the runner key of the
	// first fetch. Someone other than the runner has the token. The core records them in
	// the audit log.
	Refused func(Refusal)

	mu      sync.Mutex
	pending map[string]*pending
}

// New returns a dispatcher that starts runners with launcher.
func New(launcher Launcher) *Dispatcher {
	return &Dispatcher{Launcher: launcher, Silence: 90 * time.Second, pending: map[string]*pending{}}
}

// pending is one job that a runner works on.
type pending struct {
	job  jobspec.Job
	done chan struct{}

	mu sync.Mutex
	// seen is the launch, and after the first request of the runner its last request.
	seen    time.Time
	started bool
	// runnerKey is the hash of the runner key that the first fetch gave. It is empty
	// before that fetch. A token gives its job once.
	runnerKey string
	refusals  int
	plan      []byte
	planJSON  []byte
	result    *jobspec.Result
}

func (p *pending) touch(now time.Time) {
	p.mu.Lock()
	p.seen, p.started = now, true
	p.mu.Unlock()
}

// bind gives the job to the runner that asks first and returns a new runner key. It is
// false when the job was given.
func (p *pending) bind() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.runnerKey != "" {
		return "", false
	}
	runnerKey := rand.Text()
	p.runnerKey = key(runnerKey)
	return runnerKey, true
}

// refusal returns why a request with runnerKey cannot act for the job, or "" when it can.
func (p *pending) refusal(runnerKey string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.runnerKey == "":
		return "the job was not fetched yet"
	case runnerKey == "":
		return "the request has no runner key"
	case subtle.ConstantTimeCompare([]byte(key(runnerKey)), []byte(p.runnerKey)) != 1:
		return "the runner key is not the one that the first fetch of the job gave"
	}
	return ""
}

func (p *pending) finish(result jobspec.Result) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.result != nil {
		return false
	}
	result.Plan, result.PlanJSON = p.plan, p.planJSON
	p.result = &result
	close(p.done)
	return true
}

// key is what the dispatcher keeps of a token or a runner key: its hash.
func key(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// Run gives the job to a new runner and waits for the result. A failed job is an error.
// A stale plan is an error that wraps jobspec.ErrStalePlan.
func (d *Dispatcher) Run(ctx context.Context, job jobspec.Job) (jobspec.Result, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return jobspec.Result{}, fmt.Errorf("make the token of the job: %w", err)
	}
	token := hex.EncodeToString(raw)
	p := &pending{job: job, done: make(chan struct{}), seen: time.Now()}
	d.mu.Lock()
	d.pending[key(token)] = p
	d.mu.Unlock()
	// After this the token opens nothing.
	defer func() {
		d.mu.Lock()
		delete(d.pending, key(token))
		d.mu.Unlock()
	}()

	launched := make(chan error, 1)
	go func() { launched <- d.Launcher.Launch(ctx, token) }()
	check := time.NewTicker(time.Second)
	defer check.Stop()
	for {
		select {
		case <-p.done:
			return outcome(job, *p.result)
		case err := <-launched:
			launched = nil
			// The runner can end just after it reported.
			select {
			case <-p.done:
				return outcome(job, *p.result)
			default:
			}
			if err != nil {
				return jobspec.Result{}, fmt.Errorf("the runner of the %s job gave no result: %w",
					job.Kind, err)
			}
		case now := <-check.C:
			if err := d.overdue(p, now); err != nil {
				return jobspec.Result{}, err
			}
		case <-ctx.Done():
			return jobspec.Result{}, fmt.Errorf("the %s job was stopped: %w", job.Kind, ctx.Err())
		}
	}
}

// overdue returns an error when the runner of p was quiet at now for longer than it may be.
// Before its first request StartGrace applies, after it Silence.
func (d *Dispatcher) overdue(p *pending, now time.Time) error {
	p.mu.Lock()
	silent, started := now.Sub(p.seen), p.started
	p.mu.Unlock()
	if !started && d.StartGrace > 0 {
		if silent > d.StartGrace {
			return fmt.Errorf("the runner of the %s job made no request in the %s after its launch",
				p.job.Kind, silent.Round(time.Second))
		}
		return nil
	}
	if silent > d.Silence {
		return fmt.Errorf("the runner of the %s job said nothing for %s", p.job.Kind,
			silent.Round(time.Second))
	}
	return nil
}

func outcome(job jobspec.Job, result jobspec.Result) (jobspec.Result, error) {
	switch result.Status {
	case jobspec.StatusOK:
		return result, nil
	case jobspec.StatusStale:
		return result, fmt.Errorf("%w: %s", jobspec.ErrStalePlan, result.Error)
	case jobspec.StatusFailed:
		return result, errors.New(result.Error)
	default:
		return result, fmt.Errorf("the runner of the %s job reported the status %q, which "+
			"does not exist", job.Kind, result.Status)
	}
}

// open returns the job of the token of a request. For a request without the token of a
// running job it answers 401 Unauthorized and returns nil.
func (d *Dispatcher) open(w http.ResponseWriter, r *http.Request) *pending {
	token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	d.mu.Lock()
	p := d.pending[key(token)]
	d.mu.Unlock()
	if !found || p == nil {
		http.Error(w, "the token opens no running job", http.StatusUnauthorized)
		return nil
	}
	return p
}

// refuse answers a request that had the token of the job of p with status and reason, and
// tells Refused of it. The reason holds no secret.
func (d *Dispatcher) refuse(p *pending, w http.ResponseWriter, r *http.Request, status int,
	reason string) {
	http.Error(w, reason, status)
	if d.Refused == nil {
		return
	}
	p.mu.Lock()
	p.refusals++
	tell := p.refusals <= maxRefusals
	p.mu.Unlock()
	if tell {
		d.Refused(Refusal{Kind: p.job.Kind, Env: p.job.Env, Layer: p.job.Layer,
			CheckoutID: p.job.CheckoutID, Request: r.Method + " " + r.URL.Path,
			Remote: r.RemoteAddr, Reason: reason})
	}
}

// Handler serves the runners. Mount it at "/runner/v1/". A request carries the token of its
// job as a bearer token, and reaches only that job. The first GET of the job specification
// binds the job to its runner: the answer has a new runner key in the header
// jobspec.RunnerKeyHeader, and a later GET gets 409 Conflict. Every other request must
// carry that key in the same header, or it gets 403 Forbidden. A refused request does not
// count as a sign of life of the runner.
func (d *Dispatcher) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /runner/v1/job", func(w http.ResponseWriter, r *http.Request) {
		p := d.open(w, r)
		if p == nil {
			return
		}
		runnerKey, given := p.bind()
		if !given {
			d.refuse(p, w, r, http.StatusConflict, "the job was given to a runner already")
			return
		}
		p.touch(time.Now())
		w.Header().Set(jobspec.RunnerKeyHeader, runnerKey)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p.job.Spec)
	})
	route := func(pattern string, handle func(p *pending, w http.ResponseWriter, r *http.Request)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			p := d.open(w, r)
			if p == nil {
				return
			}
			if reason := p.refusal(r.Header.Get(jobspec.RunnerKeyHeader)); reason != "" {
				d.refuse(p, w, r, http.StatusForbidden, reason)
				return
			}
			p.touch(time.Now())
			handle(p, w, r)
		})
	}
	route("GET /runner/v1/job/checkout", func(p *pending, w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(p.job.Checkout)
	})
	route("GET /runner/v1/job/plan", func(p *pending, w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(p.job.Plan)
	})
	route("POST /runner/v1/job/heartbeat", func(_ *pending, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	route("POST /runner/v1/job/log", func(p *pending, w http.ResponseWriter, r *http.Request) {
		text, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxLog))
		if err != nil {
			http.Error(w, "read the log: "+err.Error(), http.StatusBadRequest)
			return
		}
		if p.job.Log != nil {
			if _, err := p.job.Log.Write(text); err != nil {
				http.Error(w, "store the log: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	upload := func(set func(p *pending, data []byte)) func(*pending, http.ResponseWriter, *http.Request) {
		return func(p *pending, w http.ResponseWriter, r *http.Request) {
			data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPlan))
			if err != nil {
				http.Error(w, "read the upload: "+err.Error(), http.StatusBadRequest)
				return
			}
			p.mu.Lock()
			set(p, data)
			p.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		}
	}
	route("PUT /runner/v1/job/plan", upload(func(p *pending, data []byte) { p.plan = data }))
	route("PUT /runner/v1/job/plan-json", upload(func(p *pending, data []byte) { p.planJSON = data }))
	route("POST /runner/v1/job/result", func(p *pending, w http.ResponseWriter, r *http.Request) {
		var result jobspec.Result
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLog)).Decode(&result); err != nil {
			http.Error(w, "read the result: "+err.Error(), http.StatusBadRequest)
			return
		}
		if !p.finish(result) {
			http.Error(w, "the job has a result already", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
