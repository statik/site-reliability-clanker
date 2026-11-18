package setup

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// The choices of the step "Language model". They are the values of State.KeyChoice.
const (
	ModelHosted   = "live"
	ModelLocal    = "local"
	ModelRecorded = "recorded"
)

// The model on this machine. Ollama serves it. The variant is the same model with a larger
// context: with its small default context, Ollama cuts a long prompt and reports nothing.
const (
	defaultOllamaURL = "http://127.0.0.1:11434"
	localModel       = "qwen3.8:27b"
	localVariant     = "src-local"
	localContext     = 32768
	localModelBytes  = 18 << 30
	// A machine with 32 GB of memory reports a little less than 32 GiB on Linux.
	minMemoryBytes = 30 << 30
	gibibyte       = 1 << 30
)

// MemoryBytes returns the memory of this machine. Tests replace it.
var MemoryBytes = machineMemory

// DiskFreeBytes returns the free space of the disk that holds the models of Ollama. Tests
// replace it.
var DiskFreeBytes = func() (uint64, error) {
	dir := os.Getenv("OLLAMA_MODELS")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, fmt.Errorf("find the home directory: %w", err)
		}
		dir = home
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0, fmt.Errorf("read the free space of %s: %w", dir, err)
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

// ollama talks to the Ollama program on this machine.
type ollama struct{ base string }

func (s *Server) ollama() ollama { return ollama{base: s.cfg.OllamaURL} }

// get reads one JSON answer. Ollama answers these calls at once.
func (o ollama) get(ctx context.Context, path string, into any) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, o.base+path, nil)
	if err != nil {
		return fmt.Errorf("build the request for Ollama: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("ask Ollama at %s: %w", o.base, err)
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

// version returns the version of Ollama, or an error when Ollama does not answer.
func (o ollama) version(ctx context.Context) (string, error) {
	var answer struct{ Version string }
	if err := o.get(ctx, "/api/version", &answer); err != nil {
		return "", err
	}
	return answer.Version, nil
}

// has reports whether Ollama holds a model. Ollama lists a model without a tag as
// "name:latest".
func (o ollama) has(ctx context.Context, model string) (bool, error) {
	var answer struct {
		Models []struct{ Name string }
	}
	if err := o.get(ctx, "/api/tags", &answer); err != nil {
		return false, err
	}
	for _, held := range answer.Models {
		if held.Name == model || held.Name == model+":latest" {
			return true, nil
		}
	}
	return false, nil
}

// streamLine is one line of a streamed answer of Ollama.
type streamLine struct {
	Status    string
	Error     string
	Digest    string
	Total     int64
	Completed int64
}

// stream posts a request and gives each line of the answer to onLine. Ollama can answer
// HTTP 200 and report the failure in a later line, so stream reads to the end.
func (o ollama) stream(ctx context.Context, path string, body any,
	onLine func(streamLine)) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode the request for Ollama: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base+path,
		bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("build the request for Ollama: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("ask Ollama at %s: %w", o.base, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		text, _ := io.ReadAll(io.LimitReader(response.Body, 600))
		return fmt.Errorf("Ollama answered HTTP %d for %s: %s", response.StatusCode, path,
			strings.TrimSpace(string(text)))
	}
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

// pull downloads a model. progress gets the bytes that are there and the bytes of all
// layers that Ollama named so far.
func (o ollama) pull(ctx context.Context, model string,
	progress func(completed, total int64)) error {
	type layer struct{ completed, total int64 }
	layers := map[string]layer{}
	succeeded := false
	err := o.stream(ctx, "/api/pull", map[string]any{"model": model}, func(line streamLine) {
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

// createVariant makes the variant of the model with the larger context. The variant shares
// the weights of the model, so it needs almost no space.
func (o ollama) createVariant(ctx context.Context) error {
	succeeded := false
	err := o.stream(ctx, "/api/create", map[string]any{"model": localVariant, "from": localModel,
		"parameters": map[string]any{"num_ctx": localContext}}, func(line streamLine) {
		if line.Status == "success" {
			succeeded = true
		}
	})
	if err != nil {
		return err
	}
	if !succeeded {
		return fmt.Errorf("Ollama did not report that it created %s", localVariant)
	}
	return nil
}

// LocalModel is what the page shows for the model on this machine.
type LocalModel struct {
	Checks  []Check `json:"checks"`
	Ready   bool    `json:"ready"`
	Present bool    `json:"present"`
	Model   string  `json:"model"`
}

// localModelChecks reports whether this machine can run the model.
func (s *Server) localModelChecks(ctx context.Context) LocalModel {
	found := LocalModel{Model: localModel}
	server := Check{Name: "Ollama", Why: "serves the model on this machine"}
	version, err := s.ollama().version(ctx)
	if err == nil {
		server.OK, server.Detail = true, "version "+version
		found.Present, err = s.ollama().has(ctx, localModel)
	}
	if err != nil {
		server.OK, server.Detail = false, "does not answer at "+s.cfg.OllamaURL
		server.Fix = "Install Ollama from ollama.com and start it, then check again."
	}
	found.Checks = []Check{server, memoryCheck(), diskCheck(found.Present)}
	found.Ready = len(Missing(found.Checks)) == 0
	return found
}

func memoryCheck() Check {
	check := Check{Name: "Memory", Why: "the model needs a machine with 32 GB"}
	memory, err := MemoryBytes()
	switch {
	case err != nil:
		check.Detail, check.Fix = "not known: "+err.Error(), "Use a hosted key."
	case memory < minMemoryBytes:
		check.Detail = fmt.Sprintf("%d GB", memory/gibibyte)
		check.Fix = "Use a hosted key on this machine."
	default:
		check.OK, check.Detail = true, fmt.Sprintf("%d GB", memory/gibibyte)
	}
	return check
}

func diskCheck(present bool) Check {
	check := Check{Name: "Disk", Why: "the model takes about 18 GB"}
	if present {
		check.OK, check.Detail = true, "the model is already on this machine"
		return check
	}
	free, err := DiskFreeBytes()
	switch {
	case err != nil:
		check.Detail, check.Fix = "not known: "+err.Error(), "Use a hosted key."
	case free < localModelBytes:
		check.Detail = fmt.Sprintf("%d GB free", free/gibibyte)
		check.Fix = "Free about 18 GB, then check again."
	default:
		check.OK, check.Detail = true, fmt.Sprintf("%d GB free", free/gibibyte)
	}
	return check
}

func (s *Server) getLocalModel(r *http.Request) (any, error) {
	return s.localModelChecks(r.Context()), nil
}

// prepareLocalModel downloads the model when Ollama does not hold it, and makes the
// variant with the larger context.
func (s *Server) prepareLocalModel(ctx context.Context, log func(string)) error {
	found := s.localModelChecks(ctx)
	if !found.Ready {
		return fmt.Errorf("this machine cannot run the model: %s",
			strings.Join(Missing(found.Checks), ", "))
	}
	if found.Present {
		log("Ollama already holds " + localModel + ".")
	} else {
		log("Downloading " + localModel + " through Ollama. This is about 18 GB.")
		if err := s.ollama().pull(ctx, localModel, s.setProgress); err != nil {
			return err
		}
		log("The download is complete.")
	}
	log(fmt.Sprintf("Creating the variant %s with a context of %d tokens.", localVariant,
		localContext))
	if err := s.ollama().createVariant(ctx); err != nil {
		return err
	}
	log("The model is ready.")
	return s.chooseModel(ModelLocal, "")
}

// chooseModel records the choice of this session. The settings go to secrets.env when the
// control plane starts.
func (s *Server) chooseModel(choice, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.key, s.modelChosen = key, true
	s.state.KeyChoice = choice
	return s.save()
}

// modelSettings returns the lines of secrets.env for a choice.
func (s *Server) modelSettings(choice, key string) map[string]string {
	switch choice {
	case ModelHosted:
		return map[string]string{"OPENROUTER_API_KEY": key}
	case ModelLocal:
		return map[string]string{"CLANKER_LLM_BASE_URL": s.cfg.OllamaURL + "/v1",
			"CLANKER_LLM_MODEL": localVariant}
	default:
		return map[string]string{}
	}
}

// isModelSetting reports whether a name of the environment belongs to one of the choices.
func isModelSetting(name string) bool {
	return name == "OPENROUTER_API_KEY" || strings.HasPrefix(name, "CLANKER_LLM_")
}

// otherLines returns the lines of secrets.env that are not a setting of the model.
func otherLines(content string) []string {
	kept := []string{}
	for _, line := range strings.Split(content, "\n") {
		name, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(line), "export "), "=")
		if strings.TrimSpace(line) != "" && !isModelSetting(strings.TrimSpace(name)) {
			kept = append(kept, line)
		}
	}
	return kept
}

// unsetModelSettings removes the settings of every choice from this process.
func unsetModelSettings() error {
	for _, entry := range os.Environ() {
		if name, _, _ := strings.Cut(entry, "="); isModelSetting(name) {
			if err := os.Unsetenv(name); err != nil {
				return fmt.Errorf("remove %s from this process: %w", name, err)
			}
		}
	}
	return nil
}

// saveModelSettings writes the settings of a choice to secrets.env in the data directory,
// and sets them for this process. `clanker up` reads the file. It removes the settings of
// the other choices from both places and keeps every other line of the file.
func (s *Server) saveModelSettings(choice, key string) error {
	if err := os.MkdirAll(s.cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create the data directory %s: %w", s.cfg.DataDir, err)
	}
	path := filepath.Join(s.cfg.DataDir, "secrets.env")
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := unsetModelSettings(); err != nil {
		return err
	}
	lines := otherLines(string(old))
	settings := s.modelSettings(choice, key)
	for _, name := range slices.Sorted(maps.Keys(settings)) {
		lines = append(lines, name+"="+settings[name])
		if err := os.Setenv(name, settings[name]); err != nil {
			return fmt.Errorf("set %s for this process: %w", name, err)
		}
	}
	content := ""
	if len(lines) > 0 {
		content = strings.Join(lines, "\n") + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("save the model settings in %s: %w", path, err)
	}
	return nil
}
