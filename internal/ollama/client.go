// Package ollama talks to an Ollama server over HTTP, and controls the model that the
// control plane uses on it. The package starts no program: the part that starts the server
// comes from outside as a Server.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultURL is where Ollama answers on a machine by default.
	DefaultURL = "http://127.0.0.1:11434"
	// Variant is the name of the model that the wizard makes for the control plane: the
	// model of the wizard with a larger context.
	Variant = "src-local"
	// loadTimeout is the time that Ollama gets to read a model into memory.
	loadTimeout = 10 * time.Minute
)

// Client talks to the Ollama server at Base.
type Client struct{ Base string }

// get reads one JSON answer. Ollama answers these calls at once.
func (c Client) get(ctx context.Context, path string, into any) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return fmt.Errorf("build the request for Ollama: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("ask Ollama at %s: %w", c.Base, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Ollama answered HTTP %d for %s", response.StatusCode, path)
	}
	if err := json.NewDecoder(response.Body).Decode(into); err != nil {
		return fmt.Errorf("decode the answer of Ollama for %s: %w", path, err)
	}
	return nil
}

// Version returns the version of Ollama, or an error when Ollama does not answer.
func (c Client) Version(ctx context.Context) (string, error) {
	var answer struct{ Version string }
	if err := c.get(ctx, "/api/version", &answer); err != nil {
		return "", err
	}
	return answer.Version, nil
}

// listed reports whether the list at path names a model. Ollama lists a model without a
// tag as "name:latest".
func (c Client) listed(ctx context.Context, path, model string) (bool, error) {
	var answer struct {
		Models []struct{ Name string }
	}
	if err := c.get(ctx, path, &answer); err != nil {
		return false, err
	}
	for _, held := range answer.Models {
		if held.Name == model || held.Name == model+":latest" {
			return true, nil
		}
	}
	return false, nil
}

// Has reports whether Ollama holds a model.
func (c Client) Has(ctx context.Context, model string) (bool, error) {
	return c.listed(ctx, "/api/tags", model)
}

// Loaded reports whether Ollama has a model in memory now.
func (c Client) Loaded(ctx context.Context, model string) (bool, error) {
	return c.listed(ctx, "/api/ps", model)
}

// keep sends a request with no prompt, which changes only how long Ollama keeps the model
// in memory.
func (c Client) keep(ctx context.Context, model string, keepAlive int) error {
	ctx, cancel := context.WithTimeout(ctx, loadTimeout)
	defer cancel()
	response, err := c.post(ctx, "/api/generate", map[string]any{"model": model,
		"keep_alive": keepAlive, "stream": false})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var answer struct{ Error string }
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		return fmt.Errorf("decode the answer of Ollama for /api/generate: %w", err)
	}
	if answer.Error != "" {
		return fmt.Errorf("Ollama failed at /api/generate: %s", answer.Error)
	}
	return nil
}

// Load reads a model into memory. Ollama keeps it there until Unload.
func (c Client) Load(ctx context.Context, model string) error { return c.keep(ctx, model, -1) }

// Unload removes a model from memory.
func (c Client) Unload(ctx context.Context, model string) error { return c.keep(ctx, model, 0) }

// streamLine is one line of a streamed answer of Ollama.
type streamLine struct {
	Status    string
	Error     string
	Digest    string
	Total     int64
	Completed int64
}

// post sends a request and returns the open answer. The caller closes the body.
func (c Client) post(ctx context.Context, path string, body any) (*http.Response, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode the request for Ollama: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+path,
		bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("build the request for Ollama: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("ask Ollama at %s: %w", c.Base, err)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		text, _ := io.ReadAll(io.LimitReader(response.Body, 600))
		return nil, fmt.Errorf("Ollama answered HTTP %d for %s: %s", response.StatusCode, path,
			strings.TrimSpace(string(text)))
	}
	return response, nil
}

// stream posts a request and gives each line of the answer to onLine. Ollama can answer
// HTTP 200 and report the failure in a later line, so stream reads to the end.
func (c Client) stream(ctx context.Context, path string, body any,
	onLine func(streamLine)) error {
	response, err := c.post(ctx, path, body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	lines := bufio.NewScanner(response.Body)
	lines.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for lines.Scan() {
		if len(bytes.TrimSpace(lines.Bytes())) == 0 {
			continue
		}
		var line streamLine
		if err := json.Unmarshal(lines.Bytes(), &line); err != nil {
			return fmt.Errorf("decode a line of Ollama for %s: %w", path, err)
		}
		if line.Error != "" {
			return fmt.Errorf("Ollama failed at %s: %s", path, line.Error)
		}
		onLine(line)
	}
	if err := lines.Err(); err != nil {
		return fmt.Errorf("read the answer of Ollama for %s: %w", path, err)
	}
	return nil
}

// Pull downloads a model. progress gets the bytes that are there and the bytes of all
// layers that Ollama named so far.
func (c Client) Pull(ctx context.Context, model string,
	progress func(completed, total int64)) error {
	type layer struct{ completed, total int64 }
	layers := map[string]layer{}
	succeeded := false
	err := c.stream(ctx, "/api/pull", map[string]any{"model": model}, func(line streamLine) {
		if line.Status == "success" {
			succeeded = true
		}
		if line.Digest == "" || line.Total == 0 {
			return
		}
		layers[line.Digest] = layer{line.Completed, line.Total}
		var completed, total int64
		for _, each := range layers {
			completed, total = completed+each.completed, total+each.total
		}
		progress(completed, total)
	})
	if err != nil {
		return err
	}
	if !succeeded {
		return fmt.Errorf("the download of %s ended before Ollama reported success: "+
			"start it again, and Ollama continues where it stopped", model)
	}
	return nil
}

// CreateVariant makes the model name from the model from, with a context of numCtx
// tokens. The variant shares the weights of the model, so it needs almost no space.
func (c Client) CreateVariant(ctx context.Context, name, from string, numCtx int) error {
	succeeded := false
	err := c.stream(ctx, "/api/create", map[string]any{"model": name, "from": from,
		"parameters": map[string]any{"num_ctx": numCtx}}, func(line streamLine) {
		if line.Status == "success" {
			succeeded = true
		}
	})
	if err != nil {
		return err
	}
	if !succeeded {
		return fmt.Errorf("Ollama did not report that it created %s", name)
	}
	return nil
}
