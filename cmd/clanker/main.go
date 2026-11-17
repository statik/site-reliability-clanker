// Command clanker is the control plane of the Site Reliability Clanker prototype.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/runner"
)

const usage = `clanker runs the Site Reliability Clanker control plane.

Usage:
  clanker preflight                 check that the required tools are installed
  clanker bootstrap --cloud local   apply layer 00-bootstrap with your own credentials
  clanker up                        start the control plane and the dashboard
  clanker destroy --env local       destroy every layer of an environment
  clanker sink                      run the local webhook sink alone

Common flags:
  --data DIR     data directory of the control plane (default .clanker)
  --source DIR   checkout of this repository (default .)
`

// options are the settings shared by the commands.
type options struct {
	dataDir   string
	sourceDir string
	addr      string
	sinkAddr  string
	env       string
	cloud     string
	demo      bool
}

func parse(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("clanker", flag.ContinueOnError)
	fs.StringVar(&o.dataDir, "data", ".clanker", "data directory")
	fs.StringVar(&o.sourceDir, "source", ".", "checkout of this repository")
	fs.StringVar(&o.addr, "addr", "127.0.0.1:7070", "listen address of the control plane")
	fs.StringVar(&o.sinkAddr, "sink-addr", "127.0.0.1:7071", "listen address of the sink")
	fs.StringVar(&o.env, "env", "local", "environment name")
	fs.StringVar(&o.cloud, "cloud", "local", "cloud of the environment")
	fs.BoolVar(&o.demo, "demo", true, "demo mode: simulator, local mail and webhook sink")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	var err error
	if o.dataDir, err = filepath.Abs(o.dataDir); err != nil {
		return options{}, fmt.Errorf("resolve data directory: %w", err)
	}
	if o.sourceDir, err = filepath.Abs(o.sourceDir); err != nil {
		return options{}, fmt.Errorf("resolve source directory: %w", err)
	}
	if _, err := os.Stat(filepath.Join(o.sourceDir, "platform", "CONTRACT.md")); err != nil {
		return options{}, fmt.Errorf("%s is not a checkout of this repository: run clanker "+
			"from the repository root or set --source", o.sourceDir)
	}
	return o, nil
}

// loadDotEnv reads KEY=VALUE lines from .env in the source directory. A variable that is
// already set in the environment wins.
func loadDotEnv(dir string) error {
	f, err := os.Open(filepath.Join(dir, ".env"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open .env: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		key, value = strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`)
		if os.Getenv(key) == "" && value != "" {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("set %s from .env: %w", key, err)
			}
		}
	}
	return scanner.Err()
}

var requiredTools = []string{"git", "tofu", "kubectl", "helm", "opa", "docker", "kind"}

// preflight checks the tools before anything starts, so that a first run fails with one
// clear list and not in the middle of a job.
func preflight() error {
	missing := []string{}
	for _, tool := range requiredTools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("these tools are not installed: %s. Run `nix develop` in the "+
			"repository, or install them with your package manager", strings.Join(missing, ", "))
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		return fmt.Errorf("the Docker daemon does not answer: start Docker, then try again")
	}
	return nil
}

func bootstrap(ctx context.Context, o options) error {
	if o.cloud != "local" {
		return fmt.Errorf("bootstrap for cloud %q is not enabled: the %s modules are validated "+
			"but not applied. Approve the expected cost first; see docs/HANDOFF.md", o.cloud,
			o.cloud)
	}
	r, err := runner.New(o.dataDir)
	if err != nil {
		return err
	}
	fmt.Printf("Applying layer 00-bootstrap of %s with your own credentials.\n", o.env)
	job := runner.Job{Env: o.env, Cloud: o.cloud, Layer: "00-bootstrap", Checkout: o.sourceDir,
		Log: os.Stdout}
	if err := r.ApplyDirect(ctx, job); err != nil {
		return err
	}
	outputs, err := r.Outputs(o.env, "00-bootstrap")
	if err != nil {
		return err
	}
	fmt.Printf("\nBootstrap of %s is complete. It created:\n", o.env)
	fmt.Printf("  state backend:   %v\n", outputs["state_backend"])
	fmt.Printf("  evidence bucket: %v\n", outputs["evidence_bucket"])
	fmt.Printf("  identities:      %v\n", outputs["identities"])
	fmt.Println("No credential of yours is stored. Next: clanker up")
	return nil
}

func destroy(ctx context.Context, o options) error {
	r, err := runner.New(o.dataDir)
	if err != nil {
		return err
	}
	layers := slices.Clone(orchestrator.Layers)
	slices.Reverse(layers)
	admin := filepath.Join(r.StateDir(o.env), "kubeconfig")
	for _, layer := range layers {
		outputs, err := r.Outputs(o.env, layer)
		if err != nil {
			return err
		}
		if len(outputs) == 0 {
			continue
		}
		fmt.Printf("Destroying %s/%s\n", o.env, layer)
		job := runner.Job{Env: o.env, Cloud: o.cloud, Layer: layer, Checkout: o.sourceDir,
			KubeConfig: admin, Log: os.Stdout}
		if err := r.Destroy(ctx, job); err != nil {
			return err
		}
		statePath := filepath.Join(r.StateDir(o.env), layer+".tfstate")
		if err := os.Remove(statePath); err != nil {
			return fmt.Errorf("remove state file %s: %w", statePath, err)
		}
	}
	fmt.Printf("Environment %s is destroyed.\n", o.env)
	return nil
}

func run() error {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		return nil
	}
	command := os.Args[1]
	if command == "help" || command == "--help" || command == "-h" {
		fmt.Print(usage)
		return nil
	}
	o, err := parse(os.Args[2:])
	if err != nil {
		return err
	}
	if err := loadDotEnv(o.sourceDir); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch command {
	case "preflight":
		if err := preflight(); err != nil {
			return err
		}
		fmt.Println("All required tools are present and Docker answers.")
		return nil
	case "bootstrap":
		if err := preflight(); err != nil {
			return err
		}
		return bootstrap(ctx, o)
	case "up":
		if err := preflight(); err != nil {
			return err
		}
		return up(ctx, o)
	case "destroy":
		return destroy(ctx, o)
	case "sink":
		return runSink(ctx, o.sinkAddr)
	default:
		return fmt.Errorf("command %q does not exist\n\n%s", command, usage)
	}
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "clanker:", err)
		os.Exit(1)
	}
}
