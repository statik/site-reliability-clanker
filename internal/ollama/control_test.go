package ollama_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/ollama"
	"github.com/statik/site-reliability-clanker/internal/ollama/ollamatest"
)

const model = ollama.Variant

// control returns a control of a stand-in for Ollama that holds the models, and the
// stand-in for the program.
func control(t *testing.T, models ...string) (*ollama.Control, *ollamatest.Fake,
	*ollamatest.Server) {
	t.Helper()
	fake := ollamatest.New(t, models...)
	server := &ollamatest.Server{Fake: fake}
	return &ollama.Control{Client: ollama.Client{Base: fake.URL()}, Model: model,
		Server: server, StartWait: 2 * time.Second}, fake, server
}

func state(t *testing.T, c *ollama.Control) ollama.State {
	t.Helper()
	found, err := c.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestTheStateFollowsTheServerAndTheModel(t *testing.T) {
	c, fake, _ := control(t)
	fake.SetDown(true)
	if got := state(t, c); !got.Available || got.Running || got.Version != "" ||
		got.ModelPresent || got.ModelLoaded || got.Model != model || got.URL != fake.URL() {
		t.Errorf("server down: %+v", got)
	}
	fake.SetDown(false)
	if got := state(t, c); !got.Running || got.Version != "0.40.16" || got.ModelPresent ||
		got.ModelLoaded {
		t.Errorf("server up with no model: %+v", got)
	}
	c, fake, _ = control(t, model+":latest")
	if got := state(t, c); !got.Running || !got.ModelPresent || got.ModelLoaded {
		t.Errorf("model present: %+v", got)
	}
	fake.SetLoaded("other:latest")
	if got := state(t, c); got.ModelLoaded {
		t.Errorf("another model in memory counts as this one: %+v", got)
	}
	fake.SetLoaded(model + ":latest")
	if got := state(t, c); !got.ModelPresent || !got.ModelLoaded {
		t.Errorf("model loaded: %+v", got)
	}
}

func TestStartLoadsTheModelOnAServerThatRuns(t *testing.T) {
	c, fake, server := control(t, model+":latest")
	done, err := c.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if server.Starts != 0 {
		t.Errorf("Start started a program %d times for a server that answers", server.Starts)
	}
	if want := "the server already ran; the model src-local is loaded"; done != want {
		t.Errorf("done = %q, want %q", done, want)
	}
	changes := fake.Changes()
	if len(changes) != 1 || changes[0]["model"] != model || changes[0]["keep_alive"] != -1.0 ||
		changes[0]["stream"] != false {
		t.Fatalf("requests = %v, want one load that keeps the model in memory", changes)
	}
	if _, has := changes[0]["prompt"]; has {
		t.Errorf("the load has a prompt: %v", changes[0])
	}
	if got := state(t, c); !got.ModelLoaded || got.StartedBySRC {
		t.Errorf("state after the start: %+v", got)
	}
}

func TestStartStartsAServerThatDoesNotAnswer(t *testing.T) {
	c, fake, server := control(t, model+":latest")
	fake.SetDown(true)
	done, err := c.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if server.Starts != 1 {
		t.Errorf("the program started %d times, want 1", server.Starts)
	}
	if want := "S.R.C. started the server; the model src-local is loaded"; done != want {
		t.Errorf("done = %q, want %q", done, want)
	}
	if got := state(t, c); !got.Running || !got.ModelLoaded || !got.StartedBySRC {
		t.Errorf("state after the start: %+v", got)
	}
}

func TestStartReportsAProgramThatDoesNotStart(t *testing.T) {
	c, fake, server := control(t, model+":latest")
	fake.SetDown(true)
	server.StartErr = errors.New("ollama is not installed")
	_, err := c.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "start the Ollama server: ollama is not") {
		t.Fatalf("err = %v", err)
	}
	var refusal ollama.Refusal
	if errors.As(err, &refusal) {
		t.Errorf("a failed start is a refusal: %v", err)
	}
}

func TestStartStopsAServerThatNeverAnswers(t *testing.T) {
	c, fake, _ := control(t, model+":latest")
	fake.SetDown(true)
	silent := &ollamatest.Server{}
	c.Server, c.StartWait = silent, 150*time.Millisecond
	_, err := c.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "did not answer at "+fake.URL()) {
		t.Fatalf("err = %v", err)
	}
	if silent.Stops != 1 || silent.Running {
		t.Errorf("the program that did not answer still runs: %+v", silent)
	}
}

func TestStartRefusesAModelThatTheServerDoesNotHold(t *testing.T) {
	c, fake, _ := control(t, "other:latest")
	_, err := c.Start(context.Background())
	var refusal ollama.Refusal
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, "clanker setup") ||
		!strings.Contains(refusal.Reason, model) {
		t.Fatalf("err = %v, want a refusal that names the model and clanker setup", err)
	}
	if len(fake.Changes()) != 0 {
		t.Errorf("Start asked Ollama to load a model that it does not hold")
	}
}

func TestAStartThatCannotLoadTheModelLeavesNoServerThatItStarted(t *testing.T) {
	c, fake, server := control(t, "other:latest")
	fake.SetDown(true)
	_, err := c.Start(context.Background())
	var refusal ollama.Refusal
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, "clanker setup") {
		t.Fatalf("err = %v, want the refusal of a model that is absent", err)
	}
	if server.Starts != 1 || server.Stops != 1 || server.Running {
		t.Errorf("the server that the start left: %+v, want started once and stopped", server)
	}
	if got := state(t, c); got.Running || got.StartedBySRC {
		t.Errorf("state after the failed start: %+v", got)
	}
	// A server that ran before the start is not the server of S.R.C., and stays.
	fake.SetDown(false)
	if _, err := c.Start(context.Background()); !errors.As(err, &refusal) {
		t.Fatalf("err = %v", err)
	}
	if server.Stops != 1 {
		t.Errorf("a failed start stopped a server that S.R.C. did not start")
	}
}

func TestStopUnloadsTheModelAndLeavesAServerThatItDidNotStart(t *testing.T) {
	c, fake, server := control(t, model+":latest")
	fake.SetLoaded(model+":latest", "other:latest")
	done, err := c.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := "the model src-local is unloaded; the server was not started by S.R.C. and " +
		"still runs"
	if done != want {
		t.Errorf("done = %q, want %q", done, want)
	}
	if server.Stops != 0 {
		t.Errorf("Stop stopped a server that S.R.C. did not start")
	}
	changes := fake.Changes()
	if len(changes) != 1 || changes[0]["model"] != model || changes[0]["keep_alive"] != 0.0 {
		t.Fatalf("requests = %v, want one unload", changes)
	}
	if got := fake.Loaded(); len(got) != 1 || got[0] != "other:latest" {
		t.Errorf("in memory after the stop: %v, want only the other model", got)
	}
	if got := state(t, c); !got.Running || got.ModelLoaded {
		t.Errorf("state after the stop: %+v", got)
	}
}

func TestStopStopsTheServerThatItStarted(t *testing.T) {
	c, fake, server := control(t, model+":latest")
	fake.SetDown(true)
	if _, err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	done, err := c.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := "the model src-local is unloaded; the server that S.R.C. started is stopped"
	if done != want {
		t.Errorf("done = %q, want %q", done, want)
	}
	if server.Stops != 1 {
		t.Errorf("the program stopped %d times, want 1", server.Stops)
	}
	if got := state(t, c); got.Running || got.StartedBySRC {
		t.Errorf("state after the stop: %+v", got)
	}
}

func TestStopSendsNoUnloadForAModelThatIsNotInMemory(t *testing.T) {
	c, fake, _ := control(t, model+":latest")
	done, err := c.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(done, "the model src-local was not in memory; ") {
		t.Errorf("done = %q", done)
	}
	if len(fake.Changes()) != 0 {
		t.Errorf("requests = %v, want none", fake.Changes())
	}
}

func TestStopWithNoServerSaysThatNothingRuns(t *testing.T) {
	c, fake, server := control(t, model+":latest")
	fake.SetDown(true)
	done, err := c.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if done != "the server does not run, and there is nothing to stop" || server.Stops != 0 {
		t.Errorf("done = %q, stops = %d", done, server.Stops)
	}
}

func TestAControlWithNoServerRefuses(t *testing.T) {
	fake := ollamatest.New(t, model+":latest")
	fake.SetLoaded(model + ":latest")
	remote := &ollama.Control{Client: ollama.Client{Base: fake.URL()}, Model: model,
		Reason: "the endpoint is on another machine"}
	for name, c := range map[string]*ollama.Control{"nil": nil, "no server": remote} {
		got := state(t, c)
		if got.Available || got.Reason == "" || got.Running {
			t.Errorf("%s: state = %+v, want not available with a reason", name, got)
		}
		for call, act := range map[string]func(context.Context) (string, error){
			"start": c.Start, "stop": c.Stop} {
			_, err := act(context.Background())
			var refusal ollama.Refusal
			if !errors.As(err, &refusal) || refusal.Reason != got.Reason {
				t.Errorf("%s %s: err = %v, want the refusal %q", name, call, err, got.Reason)
			}
		}
	}
	if len(fake.Changes()) != 0 || len(fake.Loaded()) != 1 {
		t.Errorf("a control with no server changed the memory: %v", fake.Changes())
	}
}
