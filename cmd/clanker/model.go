package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/internal/ollama"
)

// ollamaStopWait is the time that the Ollama program gets to end after the interrupt.
const ollamaStopWait = 10 * time.Second

// localModelControl returns the control of the local inference for the local target. The
// server is the Ollama of the settings of the model, or the usual address of Ollama when
// the settings name no endpoint. An endpoint that is not on this machine, or that is not
// an address, gets no control: the control plane starts as before, and the settings say
// why Start and Stop are not there.
func localModelControl(getenv func(string) string, dataDir string) *ollama.Control {
	control := &ollama.Control{Client: ollama.Client{Base: ollama.DefaultURL},
		Model: ollama.Variant}
	listen := strings.TrimPrefix(ollama.DefaultURL, "http://")
	if endpoint := getenv("CLANKER_LLM_BASE_URL"); endpoint != "" {
		if model := getenv("CLANKER_LLM_MODEL"); model != "" {
			control.Model = model
		}
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Host == "" {
			control.Reason = "CLANKER_LLM_BASE_URL is not an address of the form " +
				"http://host:port/v1, so S.R.C. does not know which server to start or stop"
			return control
		}
		control.Client.Base, listen = parsed.Scheme+"://"+parsed.Host, parsed.Host
		if !onThisMachine(parsed.Hostname()) {
			control.Reason = "the model endpoint " + parsed.Host + " is not on this " +
				"machine, so S.R.C. does not start or stop it"
			return control
		}
	}
	control.Server = &ollamaServer{program: "ollama", listen: listen,
		logPath: filepath.Join(dataDir, "logs", "ollama.log")}
	return control
}

// onThisMachine reports whether a host name is a loopback address.
func onThisMachine(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// ollamaServer runs the program `ollama serve`. It stops only the program that it
// started.
type ollamaServer struct {
	// program is the name or the path of the Ollama program.
	program string
	// listen is the host and the port where the program must answer.
	listen  string
	logPath string

	mu     sync.Mutex
	cmd    *exec.Cmd
	exited chan struct{}
}

// serverEnvironment returns the environment of this process without its secrets, and with
// the settings of the server. With OLLAMA_KEEP_ALIVE=-1 the server keeps a model in
// memory until a request unloads it.
func serverEnvironment(environment []string, listen string) []string {
	kept := []string{}
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if !slices.Contains(secretSettings, name) && name != "OLLAMA_HOST" &&
			name != "OLLAMA_KEEP_ALIVE" {
			kept = append(kept, entry)
		}
	}
	return append(kept, "OLLAMA_HOST="+listen, "OLLAMA_KEEP_ALIVE=-1")
}

// Start implements ollama.Server. The program outlives the request that started it. Its
// output goes to a file, because the macOS app reads the output of this process.
func (s *ollamaServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running() {
		return nil
	}
	path, err := exec.LookPath(s.program)
	if err != nil {
		return fmt.Errorf("the program %s is not on this machine: install Ollama from "+
			"ollama.com, then start again: %w", s.program, err)
	}
	if err := os.MkdirAll(filepath.Dir(s.logPath), 0o755); err != nil {
		return fmt.Errorf("create the directory of the log %s: %w", s.logPath, err)
	}
	log, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open the log of Ollama %s: %w", s.logPath, err)
	}
	cmd := exec.Command(path, "serve")
	cmd.Env = serverEnvironment(os.Environ(), s.listen)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		log.Close()
		return fmt.Errorf("start %s serve: %w; see %s", path, err, s.logPath)
	}
	exited := make(chan struct{})
	s.cmd, s.exited = cmd, exited
	go func() {
		// The exit status is in the log of the program. A stop gives a status that is
		// not zero, so it is not an error here.
		_ = cmd.Wait()
		log.Close()
		close(exited)
	}()
	return nil
}

// running reports whether the program that Start started still runs. The caller holds
// the lock.
func (s *ollamaServer) running() bool {
	if s.cmd == nil {
		return false
	}
	select {
	case <-s.exited:
		return false
	default:
		return true
	}
}

// Started implements ollama.Server.
func (s *ollamaServer) Started() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running()
}

// Stop implements ollama.Server. It interrupts the program and waits for its end. A
// program that does not end is killed.
func (s *ollamaServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running() {
		return nil
	}
	if err := s.cmd.Process.Signal(os.Interrupt); err != nil &&
		!errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("interrupt the Ollama program %d: %w", s.cmd.Process.Pid, err)
	}
	select {
	case <-s.exited:
		return nil
	case <-time.After(ollamaStopWait):
	}
	if err := s.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill the Ollama program %d: %w", s.cmd.Process.Pid, err)
	}
	<-s.exited
	return nil
}
