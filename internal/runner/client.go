package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/internal/jobspec"
)

// Client is a runner that works for a core: it asks the core for the one job of its token,
// runs it, and sends back the log and the result.
type Client struct {
	// CoreURL is the address of the control plane, for example http://127.0.0.1:7070.
	CoreURL string
	// Token is the token of the one job.
	Token  string
	Runner *Runner
	HTTP   *http.Client
}

const (
	logInterval       = 500 * time.Millisecond
	heartbeatInterval = 10 * time.Second
)

func (c *Client) call(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(c.CoreURL, "/")+
		"/runner/v1/"+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build the request for %s: %w", path, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.Token)
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read the answer of %s %s: %w", method, path, err)
	}
	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: the core answered %d: %s", method, path, response.StatusCode,
			strings.TrimSpace(string(answer)))
	}
	return answer, nil
}

// logSender sends what a job prints to the core, in pieces.
type logSender struct {
	client *Client
	ctx    context.Context

	mu      sync.Mutex
	pending []byte
}

func (l *logSender) Write(text []byte) (int, error) {
	l.mu.Lock()
	l.pending = append(l.pending, text...)
	l.mu.Unlock()
	return len(text), nil
}

func (l *logSender) flush() error {
	l.mu.Lock()
	text := l.pending
	l.pending = nil
	l.mu.Unlock()
	if len(text) == 0 {
		return nil
	}
	_, err := l.client.call(l.ctx, http.MethodPost, "job/log", text)
	return err
}

// Serve runs the one job of the token and reports its result. It returns an error only
// when the runner could not get the job or could not report.
func (c *Client) Serve(ctx context.Context) error {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 2 * time.Minute}
	}
	raw, err := c.call(ctx, http.MethodGet, "job", nil)
	if err != nil {
		return err
	}
	var spec jobspec.Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return fmt.Errorf("decode the job: %w", err)
	}
	log := &logSender{client: c, ctx: ctx}
	stop := make(chan struct{})
	var background sync.WaitGroup
	background.Add(1)
	go func() {
		defer background.Done()
		send, beat := time.NewTicker(logInterval), time.NewTicker(heartbeatInterval)
		defer send.Stop()
		defer beat.Stop()
		for {
			select {
			case <-stop:
				return
			case <-send.C:
				_ = log.flush()
			case <-beat.C:
				_, _ = c.call(ctx, http.MethodPost, "job/heartbeat", nil)
			}
		}
	}()
	result := c.run(ctx, spec, log)
	close(stop)
	background.Wait()
	if err := log.flush(); err != nil {
		return fmt.Errorf("send the end of the log: %w", err)
	}
	if spec.Kind == jobspec.KindPlan && result.Status == jobspec.StatusOK {
		if _, err := c.call(ctx, http.MethodPut, "job/plan", result.Plan); err != nil {
			return err
		}
		if _, err := c.call(ctx, http.MethodPut, "job/plan-json", result.PlanJSON); err != nil {
			return err
		}
	}
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode the result: %w", err)
	}
	_, err = c.call(ctx, http.MethodPost, "job/result", body)
	return err
}

func failed(err error) jobspec.Result {
	status := jobspec.StatusFailed
	if errors.Is(err, ErrStalePlan) {
		status = jobspec.StatusStale
	}
	return jobspec.Result{Status: status, Error: err.Error()}
}

// run executes the job. Every failure is in the result.
func (c *Client) run(ctx context.Context, spec jobspec.Spec, log io.Writer) jobspec.Result {
	result, err := c.execute(ctx, spec, log)
	if err != nil {
		result = failed(err)
	} else {
		result.Status = jobspec.StatusOK
	}
	result.ToolDigest = c.Runner.ToolDigest()
	return result
}

func (c *Client) execute(ctx context.Context, spec jobspec.Spec, log io.Writer) (jobspec.Result, error) {
	if len(spec.Backend) > 0 {
		return jobspec.Result{}, errors.New("this runner keeps the state on its own disk and " +
			"cannot use the state backend that the job names")
	}
	names := make([]string, 0, len(spec.Variables))
	for name := range spec.Variables {
		names = append(names, name)
	}
	sort.Strings(names)
	c.Runner.ExtraEnv = nil
	for _, name := range names {
		c.Runner.ExtraEnv = append(c.Runner.ExtraEnv, name+"="+spec.Variables[name])
	}
	checkout := Checkout{ID: spec.CheckoutID}
	if spec.Kind != jobspec.KindDiagnose {
		archive, err := c.call(ctx, http.MethodGet, "job/checkout", nil)
		if err != nil {
			return jobspec.Result{}, err
		}
		checkout.Archive = archive
	}
	job := Job{Env: spec.Env, Cloud: spec.Cloud, Layer: spec.Layer, Checkout: checkout,
		KubeConfig: spec.KubeConfig, Log: log}
	switch spec.Kind {
	case jobspec.KindPlan:
		plan, err := c.Runner.Plan(ctx, job)
		if err != nil {
			return jobspec.Result{}, err
		}
		return jobspec.Result{Plan: plan.Plan, PlanJSON: plan.JSON, PlanSHA256: plan.SHA256}, nil
	case jobspec.KindApply:
		plan, err := c.call(ctx, http.MethodGet, "job/plan", nil)
		if err != nil {
			return jobspec.Result{}, err
		}
		outputs, err := c.Runner.Apply(ctx, job, plan, spec.PlanSHA256)
		return jobspec.Result{Outputs: outputs}, err
	case jobspec.KindRunbook:
		return jobspec.Result{}, c.Runner.Runbook(ctx, RunbookJob{Env: spec.Env, Checkout: checkout,
			Name: spec.Runbook, Params: spec.Params, KubeConfig: spec.KubeConfig, Log: log})
	case jobspec.KindDiagnose:
		outputs := make([]jobspec.CommandOutput, 0, len(spec.Commands))
		for _, args := range spec.Commands {
			raw, err := c.Runner.Kubectl(ctx, spec.KubeConfig, args...)
			output := jobspec.CommandOutput{Output: string(raw)}
			if err != nil {
				output.Failure = err.Error()
			}
			outputs = append(outputs, output)
		}
		return jobspec.Result{Commands: outputs}, nil
	default:
		return jobspec.Result{}, fmt.Errorf("the job kind %q does not exist", spec.Kind)
	}
}
