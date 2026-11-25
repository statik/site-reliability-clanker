// Package dispatch gives jobs to runners and takes their results. It is the side of the
// core: it starts no program itself. A Launcher starts a runner, and the runner then talks
// to this package over HTTP with the token of its one job.
package dispatch

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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
)

// Dispatcher runs jobs on runners.
type Dispatcher struct {
	Launcher Launcher
	// Silence is how long a runner can say nothing before its job fails. A runner sends a
	// heartbeat every few seconds.
	Silence time.Duration

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

	mu       sync.Mutex
	seen     time.Time
	plan     []byte
	planJSON []byte
	result   *jobspec.Result
}

func (p *pending) touch() {
	p.mu.Lock()
	p.seen = time.Now()
	p.mu.Unlock()
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

// key is what the dispatcher keeps of a token: its hash.
func key(token string) string {
	sum := sha256.Sum256([]byte(token))
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
		case <-check.C:
			p.mu.Lock()
			silent := time.Since(p.seen)
			p.mu.Unlock()
			if silent > d.Silence {
				return jobspec.Result{}, fmt.Errorf("the runner of the %s job said nothing for %s",
					job.Kind, silent.Round(time.Second))
			}
		case <-ctx.Done():
			return jobspec.Result{}, fmt.Errorf("the %s job was stopped: %w", job.Kind, ctx.Err())
		}
	}
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

// Handler serves the runners. Mount it at "/runner/v1/". A request carries the token of its
// job as a bearer token, and reaches only that job.
func (d *Dispatcher) Handler() http.Handler {
	mux := http.NewServeMux()
	route := func(pattern string, handle func(p *pending, w http.ResponseWriter, r *http.Request)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			d.mu.Lock()
			p := d.pending[key(token)]
			d.mu.Unlock()
			if !found || p == nil {
				http.Error(w, "the token opens no running job", http.StatusUnauthorized)
				return
			}
			p.touch()
			handle(p, w, r)
		})
	}
	route("GET /runner/v1/job", func(p *pending, w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p.job.Spec)
	})
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
