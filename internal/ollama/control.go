package ollama

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Server is the Ollama program on this machine. It comes from outside the core of the
// control plane, because it starts a program.
type Server interface {
	// Start starts the program. It returns before the program answers.
	Start() error
	// Started reports whether Start started the program and the program still runs.
	Started() bool
	// Stop stops the program that Start started.
	Stop() error
}

// Refusal is the answer to a request that this control plane cannot do in its state.
type Refusal struct{ Reason string }

// Error implements error.
func (r Refusal) Error() string { return r.Reason }

// noControl is the reason of a control plane that has no Server.
const noControl = "this control plane cannot start or stop a model server: it does not run " +
	"on the machine of a person"

// State is what a person sees of the local inference.
type State struct {
	// Available says that the control plane can start and stop the local inference.
	Available bool `json:"available"`
	// Reason says why it cannot.
	Reason string `json:"reason,omitempty"`
	URL    string `json:"url,omitempty"`
	// Running says that the server answers.
	Running bool   `json:"running"`
	Version string `json:"version,omitempty"`
	// Model is the model that the control plane uses on the server.
	Model        string `json:"model,omitempty"`
	ModelPresent bool   `json:"model_present"`
	ModelLoaded  bool   `json:"model_loaded"`
	// StartedBySRC says that this control plane started the server, so Stop stops it.
	StartedBySRC bool `json:"started_by_src"`
}

// Control starts and stops the local inference: the Ollama server and the model of the
// control plane in its memory. A nil Control, or one with no Server, controls nothing.
type Control struct {
	Client Client
	// Model is the model that the control plane uses.
	Model string
	// Server is nil when this control plane cannot start a program for the endpoint.
	Server Server
	// Reason says why Server is nil. Empty gives the usual reason.
	Reason string
	// StartWait is how long a server that Start started gets to answer. Zero is 30 seconds.
	StartWait time.Duration

	mu sync.Mutex
}

func (c *Control) available() bool { return c != nil && c.Server != nil }

func (c *Control) refusal() Refusal {
	if c == nil || c.Reason == "" {
		return Refusal{Reason: noControl}
	}
	return Refusal{Reason: c.Reason}
}

// State reads the state of the local inference. A server that does not answer is a state
// and not an error.
func (c *Control) State(ctx context.Context) (State, error) {
	if !c.available() {
		state := State{Reason: c.refusal().Reason}
		if c != nil {
			state.Model = c.Model
		}
		return state, nil
	}
	state := State{Available: true, URL: c.Client.Base, Model: c.Model,
		StartedBySRC: c.Server.Started()}
	version, err := c.Client.Version(ctx)
	if err != nil {
		return state, nil
	}
	state.Running, state.Version = true, version
	if state.ModelPresent, err = c.Client.Has(ctx, c.Model); err != nil {
		return State{}, err
	}
	if state.ModelLoaded, err = c.Client.Loaded(ctx, c.Model); err != nil {
		return State{}, err
	}
	return state, nil
}

// waitForServer waits until the server that Start started answers.
func (c *Control) waitForServer(ctx context.Context) error {
	limit := c.StartWait
	if limit == 0 {
		limit = 30 * time.Second
	}
	deadline := time.Now().Add(limit)
	for {
		if _, err := c.Client.Version(ctx); err == nil {
			return nil
		}
		if !c.Server.Started() {
			return fmt.Errorf("the Ollama server ended before it answered at %s", c.Client.Base)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the Ollama server did not answer at %s in %s", c.Client.Base,
				limit)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for the Ollama server: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// ensureServer starts the server when it does not answer. It reports whether it started
// the server.
func (c *Control) ensureServer(ctx context.Context) (bool, error) {
	if _, err := c.Client.Version(ctx); err == nil {
		return false, nil
	}
	if err := c.Server.Start(); err != nil {
		return false, fmt.Errorf("start the Ollama server: %w", err)
	}
	if err := c.waitForServer(ctx); err != nil {
		return false, c.stopAfter(err)
	}
	return true, nil
}

// stopAfter stops the server that a failed start left, and returns the cause.
func (c *Control) stopAfter(cause error) error {
	if err := c.Server.Stop(); err != nil {
		return fmt.Errorf("%w; and the server that S.R.C. started did not stop: %v", cause, err)
	}
	return cause
}

// load reads the model into the memory of a server that answers.
func (c *Control) load(ctx context.Context) error {
	present, err := c.Client.Has(ctx, c.Model)
	if err != nil {
		return err
	}
	if !present {
		return Refusal{Reason: fmt.Sprintf("the Ollama server at %s does not hold the model "+
			"%s: run `clanker setup` and choose the model on this machine", c.Client.Base,
			c.Model)}
	}
	if err := c.Client.Load(ctx, c.Model); err != nil {
		return fmt.Errorf("load the model %s: %w", c.Model, err)
	}
	return nil
}

// Start makes sure that the server runs and that the model is in its memory. It returns
// what it did, in a sentence for a person. A server that Start started does not stay
// when the model cannot be loaded.
func (c *Control) Start(ctx context.Context) (string, error) {
	if !c.available() {
		return "", c.refusal()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	started, err := c.ensureServer(ctx)
	if err != nil {
		return "", err
	}
	server := "the server already ran"
	if started {
		server = "S.R.C. started the server"
	}
	if err := c.load(ctx); err != nil {
		if started {
			return "", c.stopAfter(err)
		}
		return "", err
	}
	return fmt.Sprintf("%s; the model %s is loaded", server, c.Model), nil
}

// unload removes the model from the memory of a server that answers. It returns what it
// did.
func (c *Control) unload(ctx context.Context) (string, error) {
	loaded, err := c.Client.Loaded(ctx, c.Model)
	if err != nil {
		return "", err
	}
	if !loaded {
		return fmt.Sprintf("the model %s was not in memory", c.Model), nil
	}
	if err := c.Client.Unload(ctx, c.Model); err != nil {
		return "", fmt.Errorf("unload the model %s: %w", c.Model, err)
	}
	return fmt.Sprintf("the model %s is unloaded", c.Model), nil
}

// Stop removes the model from memory, and stops the server only when this control plane
// started it. It returns what it did, in a sentence for a person.
func (c *Control) Stop(ctx context.Context) (string, error) {
	if !c.available() {
		return "", c.refusal()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	done := []string{}
	_, err := c.Client.Version(ctx)
	running := err == nil
	if running {
		model, err := c.unload(ctx)
		if err != nil {
			return "", err
		}
		done = append(done, model)
	}
	switch {
	case c.Server.Started():
		if err := c.Server.Stop(); err != nil {
			return "", fmt.Errorf("stop the Ollama server: %w", err)
		}
		done = append(done, "the server that S.R.C. started is stopped")
	case running:
		done = append(done, "the server was not started by S.R.C. and still runs")
	default:
		done = append(done, "the server does not run, and there is nothing to stop")
	}
	return strings.Join(done, "; "), nil
}
