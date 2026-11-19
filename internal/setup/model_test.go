package setup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeOllama is a stand-in for Ollama. It records the requests that change something.
type fakeOllama struct {
	mu      sync.Mutex
	models  []string
	pulled  []string
	created []map[string]any
	// pullLines and createLines are the streamed answers.
	pullLines   []string
	createLines []string
}

func (f *fakeOllama) serve(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"0.40.16"}`))
	})
	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		held := []map[string]string{}
		for _, name := range f.models {
			held = append(held, map[string]string{"name": name})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": held})
	})
	stream := func(lines *[]string, record func(map[string]any)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			defer f.mu.Unlock()
			record(body)
			_, _ = w.Write([]byte(strings.Join(*lines, "\n") + "\n"))
		}
	}
	mux.HandleFunc("POST /api/pull", stream(&f.pullLines, func(body map[string]any) {
		f.pulled = append(f.pulled, body["model"].(string))
	}))
	mux.HandleFunc("POST /api/create", stream(&f.createLines, func(body map[string]any) {
		f.created = append(f.created, body)
	}))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

func newFakeOllama(models ...string) *fakeOllama {
	return &fakeOllama{models: models,
		pullLines: []string{`{"status":"pulling manifest"}`,
			`{"status":"pulling aa","digest":"sha256:aa","total":1000,"completed":250}`,
			`{"status":"pulling bb","digest":"sha256:bb","total":3000,"completed":3000}`,
			`{"status":"pulling aa","digest":"sha256:aa","total":1000,"completed":1000}`,
			`{"status":"success"}`},
		createLines: []string{`{"status":"using existing layer sha256:aa"}`, `{"status":"success"}`}}
}

// machine sets the memory and the free disk space of the test machine, in GiB.
func machine(t *testing.T, memoryGB, diskGB uint64) {
	t.Helper()
	oldMemory, oldDisk := MemoryBytes, DiskFreeBytes
	t.Cleanup(func() { MemoryBytes, DiskFreeBytes = oldMemory, oldDisk })
	MemoryBytes = func() (uint64, error) { return memoryGB * gibibyte, nil }
	DiskFreeBytes = func() (uint64, error) { return diskGB * gibibyte, nil }
}

// modelServer is a wizard for the local target whose Ollama is the stand-in, and whose
// control plane already answers, so that the start creates nothing.
func modelServer(t *testing.T, ollamaURL string) (*Server, http.Handler) {
	t.Helper()
	dashboard := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(dashboard.Close)
	oldLook, oldDocker := LookPath, DockerAnswers
	t.Cleanup(func() { LookPath, DockerAnswers = oldLook, oldDocker })
	LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	DockerAnswers = func() bool { return true }
	server, err := New(context.Background(), Config{DataDir: t.TempDir(), Version: "v9.9.9",
		DashboardURL: dashboard.URL, OllamaURL: ollamaURL})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := server.Handler("127.0.0.1:7069")
	if err != nil {
		t.Fatal(err)
	}
	post(t, server, handler, "/api/target", `{"target":"local","operator":"Sam Lee"}`)
	return server, handler
}

func waitJob(t *testing.T, server *Server) *JobView {
	t.Helper()
	for range 500 {
		server.mu.Lock()
		current := server.job
		server.mu.Unlock()
		if view := current.view(); view != nil && view.Status != "running" {
			return view
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the job did not end")
	return nil
}

// deploy reviews and starts the control plane, and returns the lines of secrets.env.
func deploy(t *testing.T, server *Server, handler http.Handler) []string {
	t.Helper()
	post(t, server, handler, "/api/review", "")
	if code, body := post(t, server, handler, "/api/deploy", ""); code != http.StatusOK {
		t.Fatalf("deploy: status %d, %v", code, body)
	}
	if view := waitJob(t, server); view.Status != "done" {
		t.Fatalf("deploy failed: %s", view.Error)
	}
	data, err := os.ReadFile(filepath.Join(server.cfg.DataDir, "secrets.env"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func clearModelEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"OPENROUTER_API_KEY", "CLANKER_LLM_BASE_URL",
		"CLANKER_LLM_MODEL", "CLANKER_LLM_API_KEY", "CLANKER_LLM_MODEL_AUTHOR"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
}

func TestLocalModelChecksReportTheMachine(t *testing.T) {
	cases := []struct {
		name         string
		models       []string
		memory, disk uint64
		missing      []string
	}{
		{"model is there, disk is full", []string{localModel}, 64, 1, []string{}},
		{"model is missing, disk has room", []string{"other:1b"}, 32, 40, []string{}},
		{"model is missing, disk is full", nil, 32, 10, []string{"Disk"}},
		{"small memory", []string{localModel}, 16, 40, []string{"Memory"}},
	}
	for _, c := range cases {
		machine(t, c.memory, c.disk)
		server, _ := modelServer(t, newFakeOllama(c.models...).serve(t))
		found := server.localModelChecks(context.Background())
		if missing := Missing(found.Checks); !slices.Equal(missing, c.missing) {
			t.Errorf("%s: missing = %v, want %v", c.name, missing, c.missing)
		}
		if found.Present != slices.Contains(c.models, localModel) {
			t.Errorf("%s: present = %v", c.name, found.Present)
		}
	}
}

func TestLocalModelChecksSayThatOllamaDoesNotAnswer(t *testing.T) {
	machine(t, 64, 100)
	server, handler := modelServer(t, "http://127.0.0.1:1")
	found := server.localModelChecks(context.Background())
	if found.Ready || found.Checks[0].OK || found.Checks[0].Fix == "" {
		t.Fatalf("checks = %+v", found.Checks)
	}
	post(t, server, handler, "/api/model", `{"choice":"local"}`)
	if view := waitJob(t, server); view.Status != "failed" || !strings.Contains(view.Error, "Ollama") {
		t.Fatalf("job = %+v", view)
	}
	if server.state.KeyChoice != "" {
		t.Fatalf("the choice was saved although the model is not ready: %q", server.state.KeyChoice)
	}
}

func TestLocalModelIsPulledOnlyWhenItIsMissing(t *testing.T) {
	machine(t, 64, 100)
	for _, present := range []bool{true, false} {
		ollama := newFakeOllama()
		if present {
			ollama.models = []string{localModel}
		}
		server, handler := modelServer(t, ollama.serve(t))
		if code, body := post(t, server, handler, "/api/model", `{"choice":"local"}`); code != 200 {
			t.Fatalf("status %d, %v", code, body)
		}
		view := waitJob(t, server)
		if view.Status != "done" {
			t.Fatalf("present=%v: the job failed: %s", present, view.Error)
		}
		if pulled := len(ollama.pulled) == 1; pulled == present {
			t.Errorf("present=%v: pulled = %v", present, ollama.pulled)
		}
		if !present && (view.Completed != 4000 || view.Total != 4000) {
			t.Errorf("progress = %d of %d, want 4000 of 4000", view.Completed, view.Total)
		}
		if len(ollama.created) != 1 {
			t.Fatalf("present=%v: created = %v", present, ollama.created)
		}
		created := ollama.created[0]
		size, _ := created["parameters"].(map[string]any)["num_ctx"].(float64)
		if created["model"] != localVariant || created["from"] != localModel || size != 32768 {
			t.Errorf("create request = %v", created)
		}
		if server.state.KeyChoice != ModelLocal {
			t.Errorf("choice = %q", server.state.KeyChoice)
		}
	}
}

func TestAFailureInTheStreamOfOllamaFailsTheJob(t *testing.T) {
	machine(t, 64, 100)
	cases := map[string]func(*fakeOllama){
		"pull reports an error": func(f *fakeOllama) {
			f.pullLines = []string{`{"status":"pulling manifest"}`, `{"error":"no space left"}`}
		},
		"pull ends early": func(f *fakeOllama) {
			f.pullLines = []string{`{"status":"pulling manifest"}`}
		},
		"create reports an error": func(f *fakeOllama) {
			f.createLines = []string{`{"error":"model not found"}`}
		},
	}
	for name, change := range cases {
		ollama := newFakeOllama()
		change(ollama)
		server, handler := modelServer(t, ollama.serve(t))
		post(t, server, handler, "/api/model", `{"choice":"local"}`)
		if view := waitJob(t, server); view.Status != "failed" {
			t.Errorf("%s: the job is %s", name, view.Status)
		}
		if server.state.KeyChoice != "" {
			t.Errorf("%s: the choice was saved", name)
		}
	}
}

func TestLocalModelIsRefusedForAWS(t *testing.T) {
	server, handler := modelServer(t, newFakeOllama(localModel).serve(t))
	post(t, server, handler, "/api/target", `{"target":"aws","env":"prod-1"}`)
	code, body := post(t, server, handler, "/api/model", `{"choice":"local"}`)
	if code != http.StatusBadRequest || !strings.Contains(body["error"].(string), "this machine") {
		t.Fatalf("status %d, %v", code, body)
	}
	if code, _ := post(t, server, handler, "/api/model", `{"choice":"ollama"}`); code != 400 {
		t.Fatalf("a choice that does not exist: status %d", code)
	}
}

func TestEachChoiceRemovesTheSettingsOfTheOthers(t *testing.T) {
	clearModelEnv(t)
	machine(t, 64, 100)
	ollama := newFakeOllama(localModel)
	url := ollama.serve(t)
	server, handler := modelServer(t, url)
	const key = "sk-or-v1-0123456789abcdef0123456789"
	secrets := filepath.Join(server.cfg.DataDir, "secrets.env")
	if err := os.WriteFile(secrets, []byte("CLANKER_SIGNING_KEY=keep\n"+
		"CLANKER_LLM_MODEL_AUTHOR=old\nexport CLANKER_LLM_API_KEY=old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLANKER_LLM_MODEL_AUTHOR", "old")

	post(t, server, handler, "/api/model", `{"choice":"live","key":"`+key+`"}`)
	want := []string{"CLANKER_SIGNING_KEY=keep", "OPENROUTER_API_KEY=" + key}
	if lines := deploy(t, server, handler); !slices.Equal(lines, want) {
		t.Fatalf("hosted key: secrets.env = %v, want %v", lines, want)
	}
	if os.Getenv("OPENROUTER_API_KEY") != key || os.Getenv("CLANKER_LLM_MODEL_AUTHOR") != "" {
		t.Fatal("hosted key: the environment of the process is wrong")
	}

	post(t, server, handler, "/api/model", `{"choice":"local"}`)
	waitJob(t, server)
	want = []string{"CLANKER_SIGNING_KEY=keep", "CLANKER_LLM_BASE_URL=" + url + "/v1",
		"CLANKER_LLM_MODEL=src-local"}
	if lines := deploy(t, server, handler); !slices.Equal(lines, want) {
		t.Fatalf("local model: secrets.env = %v, want %v", lines, want)
	}
	if _, set := os.LookupEnv("OPENROUTER_API_KEY"); set ||
		os.Getenv("CLANKER_LLM_MODEL") != "src-local" {
		t.Fatal("local model: the environment of the process is wrong")
	}
	if info, _ := os.Stat(secrets); info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets.env has mode %o, want 600", info.Mode().Perm())
	}

	post(t, server, handler, "/api/model", `{"choice":"recorded"}`)
	want = []string{"CLANKER_SIGNING_KEY=keep"}
	if lines := deploy(t, server, handler); !slices.Equal(lines, want) {
		t.Fatalf("recorded answers: secrets.env = %v, want %v", lines, want)
	}
	if _, set := os.LookupEnv("CLANKER_LLM_BASE_URL"); set {
		t.Fatal("recorded answers: the address of the local model is still set")
	}
}

func TestAStartWithoutANewChoiceKeepsTheSavedKey(t *testing.T) {
	clearModelEnv(t)
	server, handler := modelServer(t, newFakeOllama().serve(t))
	const key = "sk-or-v1-0123456789abcdef0123456789"
	post(t, server, handler, "/api/model", `{"choice":"live","key":"`+key+`"}`)
	deploy(t, server, handler)
	want := []string{"OPENROUTER_API_KEY=" + key}
	if lines := deploy(t, server, handler); !slices.Equal(lines, want) {
		t.Fatalf("after the second start: secrets.env = %v, want %v", lines, want)
	}
}

func TestDefaultAddressOfTheLocalModelIsTheContract(t *testing.T) {
	server, err := New(context.Background(), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	settings := server.modelSettings(ModelLocal, "")
	if settings["CLANKER_LLM_BASE_URL"] != "http://127.0.0.1:11434/v1" ||
		settings["CLANKER_LLM_MODEL"] != "src-local" {
		t.Fatalf("settings = %v", settings)
	}
}
