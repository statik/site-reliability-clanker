// Package ollamatest is a stand-in for Ollama in tests. No test talks to the Ollama of
// the machine.
package ollamatest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
)

// Fake is a stand-in for an Ollama server. It records the requests that change the
// memory.
type Fake struct {
	mu sync.Mutex
	// down makes every request fail, as a server that does not run.
	down    bool
	held    []string
	loaded  []string
	changes []map[string]any
	url     string
}

// New returns a stand-in that holds the models. It runs until the test ends.
func New(t *testing.T, models ...string) *Fake {
	t.Helper()
	fake := &Fake{held: models}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"0.40.16"}`))
	})
	mux.HandleFunc("GET /api/tags", fake.list(func() []string { return fake.held }))
	mux.HandleFunc("GET /api/ps", fake.list(func() []string { return fake.loaded }))
	mux.HandleFunc("POST /api/generate", fake.generate)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,
		r *http.Request) {
		if fake.isDown() {
			http.Error(w, "the server does not run", http.StatusBadGateway)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	fake.url = server.URL
	return fake
}

// URL is the address of the stand-in.
func (f *Fake) URL() string { return f.url }

func (f *Fake) isDown() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.down
}

// SetDown makes the stand-in answer as a server that does not run, or run again.
func (f *Fake) SetDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

// SetLoaded sets the models in memory. Ollama names a model without a tag "name:latest".
func (f *Fake) SetLoaded(models ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loaded = models
}

// Loaded returns the models in memory.
func (f *Fake) Loaded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.loaded)
}

// Changes returns the bodies of the requests that changed the memory.
func (f *Fake) Changes() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.changes)
}

func (f *Fake) list(models func() []string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		rows := []map[string]string{}
		for _, name := range models() {
			rows = append(rows, map[string]string{"name": name, "model": name})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": rows})
	}
}

// generate does what Ollama does with a request that has no prompt: a keep_alive of 0
// removes the model from memory, and any other value reads it into memory.
func (f *Fake) generate(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	model, _ := body["model"].(string)
	if !slices.Contains(f.held, model) && !slices.Contains(f.held, model+":latest") {
		http.Error(w, `{"error":"model '`+model+`' not found"}`, http.StatusNotFound)
		return
	}
	f.changes = append(f.changes, body)
	name := model + ":latest"
	f.loaded = slices.DeleteFunc(f.loaded, func(each string) bool { return each == name })
	reason := "unload"
	if keep, _ := body["keep_alive"].(float64); keep != 0 {
		f.loaded, reason = append(f.loaded, name), "load"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"model": model, "response": "", "done": true,
		"done_reason": reason})
}

// Server is a stand-in for the Ollama program. Start makes the Fake answer, when there
// is one.
type Server struct {
	mu      sync.Mutex
	Fake    *Fake
	Running bool
	Starts  int
	Stops   int
	// StartErr makes Start fail.
	StartErr error
}

// Start implements ollama.Server.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.StartErr != nil {
		return s.StartErr
	}
	s.Starts++
	s.Running = true
	if s.Fake != nil {
		s.Fake.SetDown(false)
	}
	return nil
}

// Started implements ollama.Server.
func (s *Server) Started() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Running
}

// Stop implements ollama.Server.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Stops++
	s.Running = false
	if s.Fake != nil {
		s.Fake.SetDown(true)
	}
	return nil
}
