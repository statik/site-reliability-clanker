package setup

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/statik/site-reliability-clanker/internal/ollama"
)

// The choices of the step "Language model". They are the values of State.KeyChoice.
// ModelBedrock is Amazon Bedrock in the account of the AWS target.
const (
	ModelHosted  = "live"
	ModelLocal   = "local"
	ModelBedrock = "bedrock"
)

// knownChoice returns the choice when this version has it, and no choice when it does
// not. An earlier version had a third choice, the recorded answers; a saved state with
// that value makes the person choose again.
func knownChoice(choice string) string {
	if choice == ModelHosted || choice == ModelLocal || choice == ModelBedrock {
		return choice
	}
	return ""
}

// The model on this machine. Ollama serves it. The variant is the same model with a larger
// context: with its small default context, Ollama cuts a long prompt and reports nothing.
const (
	localModel      = "qwen3.8:27b"
	localVariant    = ollama.Variant
	localContext    = 32768
	localModelBytes = 18 << 30
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

func (s *Server) ollama() ollama.Client { return ollama.Client{Base: s.cfg.OllamaURL} }

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
	version, err := s.ollama().Version(ctx)
	if err == nil {
		server.OK, server.Detail = true, "version "+version
		found.Present, err = s.ollama().Has(ctx, localModel)
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
		if err := s.ollama().Pull(ctx, localModel, s.setProgress); err != nil {
			return err
		}
		log("The download is complete.")
	}
	log(fmt.Sprintf("Creating the variant %s with a context of %d tokens.", localVariant,
		localContext))
	if err := s.ollama().CreateVariant(ctx, localVariant, localModel,
		localContext); err != nil {
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
	s.state.KeyChoice, s.state.BedrockModels = choice, nil
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
	content := strings.Join(append(lines, ""), "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("save the model settings in %s: %w", path, err)
	}
	return nil
}
