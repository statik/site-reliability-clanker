package setup

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/internal/ollama"
)

//go:embed ui
var uiFiles embed.FS

// Config is what the wizard needs from the program that starts it.
type Config struct {
	// DataDir is the data directory of the control plane on this machine.
	DataDir string
	// Application is the title of the application pack that the local target deploys.
	Application string
	// Version is the release of this binary, or "dev".
	Version string
	// DashboardURL is where the dashboard answers on this machine.
	DashboardURL string
	// InApp says that the macOS app shows the wizard in its own window. The pages then
	// say how to stop and start with the app, not with a terminal.
	InApp bool
	// StartLocal starts the control plane for the local target in this process. It
	// returns at once; the wizard waits for the dashboard.
	// The operator is the person who ran the wizard.
	StartLocal func(ctx context.Context, env, operator string) error
	// CLI runs the aws program. Nil means the real program.
	CLI CLI
	// OllamaURL is where Ollama answers on this machine. Empty means its usual address.
	OllamaURL string
	// Layers applies 00-bootstrap and destroys the layers for the installer on AWS.
	Layers Layers
	// Checkout is the checkout of this repository that the wizard runs from, or "" for a
	// released binary. The installer on AWS builds the runner and the core from a checkout.
	Checkout string
	// AWSRoot holds the directory of each environment on AWS. Empty means ~/.clanker/aws.
	AWSRoot string
}

// State is what the wizard remembers between starts. It holds no secret.
type State struct {
	Target string `json:"target"`
	Env    string `json:"env"`
	// Operator is the name of the person who sets up the local target. The control
	// plane there has no sign-in, so this name is its first approver.
	Operator  string `json:"operator"`
	Profile   string `json:"profile"`
	Region    string `json:"region"`
	Account   string `json:"account"`
	Principal string `json:"principal"`
	KeyChoice string `json:"key_choice"`
	// BedrockModels is the inference profile of each role when KeyChoice is ModelBedrock.
	BedrockModels map[string]string `json:"bedrock_models,omitempty"`
	Deployed      bool              `json:"deployed"`
	// PlaceMode says whether the control plane goes into a network that exists or into a
	// new one. Network and Subnets name the network that exists; NetworkCIDR is the
	// address range of the new one. Access says how a person reaches the control plane.
	PlaceMode   string   `json:"place_mode"`
	Network     string   `json:"network"`
	Subnets     []string `json:"subnets"`
	NetworkCIDR string   `json:"network_cidr"`
	Access      string   `json:"access"`
	// RouterGroup is the security group of the Tailscale subnet router of the network.
	RouterGroup string `json:"router_group"`
	// ZoneID and ZoneName name the public zone of the names on AWS. TagKeys are the keys
	// of the required tags: their values stay in memory and in home.tfvars. Source is the
	// checkout that the installer builds from, and Nanos the release of the kernel.
	ZoneID   string   `json:"zone_id"`
	ZoneName string   `json:"zone_name"`
	TagKeys  []string `json:"tag_keys"`
	Source   string   `json:"source"`
	Nanos    string   `json:"nanos"`
	// Installed says that the installer on AWS ended with every step, and Result is what
	// it printed at the end.
	Installed bool           `json:"installed"`
	Result    *InstallResult `json:"result,omitempty"`
}

// Server is the wizard. One person uses it, on the loopback address.
type Server struct {
	cfg   Config
	token string

	mu    sync.Mutex
	state State
	key   string
	// modelChosen says that the person chose the language model in this start. Only then
	// does the start of the control plane write the model settings.
	modelChosen bool
	review      []Change
	// found is the last survey of the account. A placement must come from it.
	found   *Discovery
	job     *job
	baseCtx context.Context
	// tags are the values of the required tags that the person gave in this start.
	tags      map[string]string
	installer *Installer
}

// New loads the saved state and makes the token of this start.
func New(ctx context.Context, cfg Config) (*Server, error) {
	if cfg.CLI == nil {
		cfg.CLI = realCLI
	}
	if cfg.OllamaURL == "" {
		cfg.OllamaURL = ollama.DefaultURL
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("make the access token: %w", err)
	}
	s := &Server{cfg: cfg, token: hex.EncodeToString(raw), baseCtx: ctx,
		installer: &Installer{CLI: cfg.CLI, Layers: cfg.Layers, Version: cfg.Version,
			Root: cfg.AWSRoot}}
	data, err := os.ReadFile(s.statePath())
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("read the saved setup state: %w", err)
	default:
		if err := json.Unmarshal(data, &s.state); err != nil {
			return nil, fmt.Errorf("decode %s: %w; remove the file to start again",
				s.statePath(), err)
		}
	}
	s.state.KeyChoice = knownChoice(s.state.KeyChoice)
	return s, nil
}

// Token is the secret that the browser must send with each request.
func (s *Server) Token() string { return s.token }

func (s *Server) statePath() string { return filepath.Join(s.cfg.DataDir, "setup.json") }

// save writes the state. The caller holds the lock.
func (s *Server) save() error {
	if err := os.MkdirAll(s.cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create the data directory %s: %w", s.cfg.DataDir, err)
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the setup state: %w", err)
	}
	if err := os.WriteFile(s.statePath(), data, 0o600); err != nil {
		return fmt.Errorf("save the setup state: %w", err)
	}
	return nil
}

func (s *Server) aws() AWS {
	return AWS{Profile: s.state.Profile, Region: s.state.Region, Env: s.state.Env,
		Run: s.cfg.CLI}
}

// Handler serves the pages and the API. listenHost is the host and port that the browser
// uses; a request for another host is refused, so that another site cannot reach the
// wizard through a DNS name that points at this machine.
func (s *Server) Handler(listenHost string) (http.Handler, error) {
	pages, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		return nil, fmt.Errorf("open the wizard pages: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(pages))
	api := func(pattern string, handler func(*http.Request) (any, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Setup-Token")),
				[]byte(s.token)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "the access " +
					"token is missing or wrong: open the address that `clanker setup` printed"})
				return
			}
			result, err := handler(r)
			if err != nil {
				writeJSON(w, statusOf(err), map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, result)
		})
	}
	api("GET /api/state", s.getState)
	api("GET /api/checks", s.getChecks)
	api("POST /api/target", s.postTarget)
	api("POST /api/model", s.postModel)
	api("GET /api/model/local", s.getLocalModel)
	api("GET /api/aws/profiles", s.getProfiles)
	api("GET /api/aws/bedrock", s.getBedrockProfiles)
	api("POST /api/aws/signin", s.postSignIn)
	api("POST /api/aws/discover", s.postDiscover)
	api("POST /api/aws/place", s.postPlace)
	api("POST /api/aws/login", s.postLogin)
	api("GET /api/aws/details", s.getDetails)
	api("POST /api/aws/details", s.postDetails)
	api("POST /api/aws/remove", s.postRemove)
	api("POST /api/review", s.postReview)
	api("POST /api/deploy", s.postDeploy)
	_, port, err := net.SplitHostPort(listenHost)
	if err != nil {
		return nil, fmt.Errorf("read the listen address %q: %w", listenHost, err)
	}
	allowed := map[string]bool{"127.0.0.1:" + port: true, "localhost:" + port: true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(w, "this address does not serve the wizard", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	}), nil
}

// userError is a mistake of the person, not of the program.
type userError struct{ message string }

func (e userError) Error() string { return e.message }

func invalid(format string, args ...any) error {
	return userError{fmt.Sprintf(format, args...)}
}

func statusOf(err error) int {
	var mistake userError
	switch {
	case errors.As(err, &mistake):
		return http.StatusBadRequest
	case errors.Is(err, ErrNeedsLogin), errors.Is(err, errBusy):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decode(r *http.Request, into any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<16)
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		return invalid("the request body is not valid JSON: %v", err)
	}
	return nil
}

// dashboardUp reports whether the control plane answers on this machine.
func (s *Server) dashboardUp(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.cfg.DashboardURL+"/api/v1/work-items", nil)
	if err != nil {
		return false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func (s *Server) waitDashboard(ctx context.Context, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if s.dashboardUp(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("the dashboard did not answer at %s in %s", s.cfg.DashboardURL, limit)
}
