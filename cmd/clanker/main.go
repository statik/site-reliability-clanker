// Command clanker is the control plane of the Site Reliability Clanker prototype.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/statik/site-reliability-clanker/internal/awsparams"
	"github.com/statik/site-reliability-clanker/internal/bundle"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/packstore"
	"github.com/statik/site-reliability-clanker/internal/runner"
	"github.com/statik/site-reliability-clanker/internal/setup"
)

// version is the release of this binary. `make release` sets it.
var version = "dev"

const usage = `clanker runs the Site Reliability Clanker control plane.

Usage:
  clanker setup                     open the first-run wizard in the browser
  clanker preflight                 check that the required tools are installed
  clanker discover --profile NAME   survey an AWS account and propose where to install; it
                                    only reads. --region NAME surveys that region
  clanker bootstrap --cloud local   apply layer 00-bootstrap with your own credentials
  clanker up                        start the control plane and the dashboard
  clanker destroy --env local       destroy every layer of an environment
  clanker reset                     destroy the local environment and remove its history
                                    and its saved setup; the next start begins again
  clanker sink                      run the local webhook sink alone
  clanker login --server URL        sign this machine in to a control plane, for git and scripts
  clanker token --server URL        print the token of this machine, for a script
  clanker pack validate DIR         check the application pack in a directory
  clanker pack import URL --tag TAG copy a version of a pack from a git repository to the
                                    data directory; --dir NAME is its directory there
  clanker model status              print the language model that a running control plane
                                    uses, and the state of the local inference
  clanker model start               start the local inference of a running control plane
  clanker model stop                stop the local inference of a running control plane

Common flags:
  --data DIR     data directory of the control plane (default .clanker in a checkout,
                 ~/.clanker for a released binary)
  --source DIR   checkout of this repository (default . in a checkout; a released binary
                 carries the files and needs no checkout)
  --pack DIR     application pack of setup and up (default: the pack that comes with the
                 control plane)
`

// options are the settings shared by the commands.
type options struct {
	// dataDir and sourceDir are empty for the core on a cloud, which uses no directory.
	dataDir   string
	sourceDir string
	// source holds the files of this repository: a checkout, or the files that a released
	// binary carries.
	source   fs.FS
	addr     string
	sinkAddr string
	env      string
	cloud    string
	demo     bool
	// packDir is the directory of the application pack: in the source files, or on this
	// machine. Empty means the one pack in the directory apps of the source files. app is
	// the pack that was read from it, packOrigin says where it came from, and packDisk is
	// its directory on this machine (empty for the core on a cloud, which has no disk).
	packDir    string
	app        pack.Pack
	packOrigin packstore.Origin
	packDisk   string
	// operator is the name of the person who ran the wizard for the local target.
	operator  string
	setupAddr string
	noBrowser bool
	// inApp says that the macOS app started the wizard and shows it in its own window.
	inApp bool
	// profile and region are the AWS CLI profile and the region of `clanker discover`.
	profile string
	region  string
}

func parse(command string, args []string) (options, error) {
	var o options
	flags := flag.NewFlagSet("clanker", flag.ContinueOnError)
	flags.StringVar(&o.dataDir, "data", "", "data directory")
	flags.StringVar(&o.sourceDir, "source", "", "checkout of this repository")
	flags.StringVar(&o.setupAddr, "setup-addr", "127.0.0.1:7069", "listen address of the wizard")
	flags.BoolVar(&o.noBrowser, "no-browser", false, "do not open the wizard in the browser")
	flags.BoolVar(&o.inApp, "app", false, "the macOS app shows the wizard: open no browser, "+
		"and stop when the app is gone")
	flags.StringVar(&o.addr, "addr", "127.0.0.1:7070", "listen address of the control plane")
	flags.StringVar(&o.sinkAddr, "sink-addr", "127.0.0.1:7071", "listen address of the sink")
	flags.StringVar(&o.env, "env", "local", "environment name")
	flags.StringVar(&o.packDir, "pack", "", "directory of the application pack, in the "+
		"source files or on this machine (default: the one pack in apps)")
	flags.StringVar(&o.cloud, "cloud", "local", "cloud of the environment")
	flags.BoolVar(&o.demo, "demo", true, "demo mode: simulator, local mail and webhook sink")
	flags.StringVar(&o.profile, "profile", "", "AWS CLI profile of the account to survey")
	flags.StringVar(&o.region, "region", "", "region to survey (default: the most used one)")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	locator := locate
	if command == "up" && o.cloud != "local" {
		locator = locateCore
	}
	if err := locator(&o); err != nil {
		return options{}, err
	}
	if _, err := fs.Stat(o.source, "platform/CONTRACT.md"); err != nil {
		return options{}, fmt.Errorf("the files of this repository are not at %q: run clanker "+
			"from the repository root or set --source", o.sourceDir)
	}
	if err := locatePack(&o); err != nil {
		return options{}, err
	}
	return o, nil
}

// locatePack reads the application pack of this start. A directory that the source files
// have is the pack that comes with the control plane. Any other directory is a pack on
// this machine, for example one that `clanker pack import` put in the data directory.
func locatePack(o *options) error {
	inSource := o.packDir == ""
	if !inSource && fs.ValidPath(o.packDir) {
		_, err := fs.Stat(o.source, path.Join(o.packDir, pack.ManifestName))
		inSource = err == nil
	}
	if !inSource {
		app, origin, err := packstore.Open(o.packDir)
		if err != nil {
			return err
		}
		o.app, o.packOrigin = app, origin
		o.packDisk, err = filepath.Abs(o.packDir)
		return err
	}
	app, err := loadPack(o.source, o.packDir)
	if err != nil {
		return err
	}
	hash, err := pack.Hash(app.Files())
	if err != nil {
		return err
	}
	o.app, o.packOrigin = app, packstore.Origin{Source: packstore.BuiltIn, SHA256: hash}
	if o.sourceDir != "" {
		o.packDisk = filepath.Join(o.sourceDir, filepath.FromSlash(app.Dir()))
	}
	return nil
}

// packsRoot is the directory of the source files that holds the application packs.
const packsRoot = "apps"

// loadPack reads the application pack. With no directory it reads the one pack in the
// directory of the packs.
func loadPack(source fs.FS, dir string) (pack.Pack, error) {
	if dir == "" {
		found, err := pack.Discover(source, packsRoot)
		if err != nil {
			return pack.Pack{}, err
		}
		dir = found
	}
	return pack.Load(source, dir)
}

// locateCore fills in the source files of the core on a cloud. That core has no disk to
// write on: a released binary reads the files that it carries from memory, and there is no
// data directory.
func locateCore(o *options) error {
	if o.dataDir != "" {
		return fmt.Errorf("the control plane on %s keeps no file: remove --data", o.cloud)
	}
	if o.sourceDir == "" && bundle.Present() {
		var err error
		o.source, err = bundle.FS()
		return err
	}
	return locateCheckout(o)
}

// locateCheckout fills in the source directory from --source or the current directory.
func locateCheckout(o *options) error {
	if o.sourceDir == "" {
		o.sourceDir = "."
	}
	var err error
	if o.sourceDir, err = filepath.Abs(o.sourceDir); err != nil {
		return fmt.Errorf("resolve source directory: %w", err)
	}
	o.source = os.DirFS(o.sourceDir)
	return nil
}

// locate fills in the data directory and the source directory. A released binary carries
// the source files: it unpacks them into the data directory and needs no checkout.
func locate(o *options) error {
	bundled := o.sourceDir == "" && bundle.Present()
	if o.dataDir == "" {
		o.dataDir = ".clanker"
		if bundled {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("find the home directory for the data directory: %w; "+
					"set --data", err)
			}
			o.dataDir = filepath.Join(home, ".clanker")
		}
	}
	var err error
	if o.dataDir, err = filepath.Abs(o.dataDir); err != nil {
		return fmt.Errorf("resolve data directory: %w", err)
	}
	if bundled {
		if err := os.MkdirAll(o.dataDir, 0o755); err != nil {
			return fmt.Errorf("create data directory %s: %w", o.dataDir, err)
		}
		if o.sourceDir, err = bundle.Extract(o.dataDir); err != nil {
			return err
		}
		o.source = os.DirFS(o.sourceDir)
		return nil
	}
	return locateCheckout(o)
}

// loadDotEnv reads KEY=VALUE lines from .env in the source directory. A variable that is
// already set in the environment wins.
func loadDotEnv(dir string) error { return loadEnvFile(filepath.Join(dir, ".env")) }

func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
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
				return fmt.Errorf("set %s from %s: %w", key, path, err)
			}
		}
	}
	return scanner.Err()
}

// preflight checks the tools of a target before anything starts, so that a first run
// fails with one clear list and not in the middle of a job.
func preflight(target string) error {
	checks, err := setup.Checks(target)
	if err != nil {
		return err
	}
	lines := []string{}
	for _, c := range checks {
		if !c.OK {
			lines = append(lines, fmt.Sprintf("  %s (%s): %s. %s", c.Name, c.Why, c.Detail, c.Fix))
		}
	}
	if len(lines) > 0 {
		return fmt.Errorf("this machine is not ready for the %s target:\n%s", target,
			strings.Join(lines, "\n"))
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
	job := runner.Job{Env: o.env, Cloud: o.cloud, Layer: "00-bootstrap", Checkout: runner.Checkout{Dir: o.sourceDir},
		Log: os.Stdout}
	outputs, err := r.ApplyDirect(ctx, job)
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
	// The teardown uses the administrator kubeconfig of the operator. Before the cluster
	// exists there is none, and the layers below the cluster need none.
	admin, err := os.ReadFile(filepath.Join(r.StateDir(o.env), "kubeconfig"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read the administrator kubeconfig of %s: %w", o.env, err)
	}
	for _, layer := range layers {
		// A layer with no state file in the state directory was never applied here.
		statePath := filepath.Join(r.StateDir(o.env), layer+".tfstate")
		if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("check the state file %s: %w", statePath, err)
		}
		fmt.Printf("Destroying %s/%s\n", o.env, layer)
		job := runner.Job{Env: o.env, Cloud: o.cloud, Layer: layer,
			Checkout: runner.Checkout{Dir: o.sourceDir}, KubeConfig: string(admin), Log: os.Stdout}
		if err := r.Destroy(ctx, job); err != nil {
			return err
		}
		if err := os.Remove(statePath); err != nil {
			return fmt.Errorf("remove state file %s: %w", statePath, err)
		}
	}
	fmt.Printf("Environment %s is destroyed.\n", o.env)
	return nil
}

func run() error {
	args := os.Args[1:]
	if len(args) == 0 {
		if !bundle.Present() {
			fmt.Print(usage)
			return nil
		}
		// A released binary that is started with no command opens the wizard.
		args = []string{"setup"}
	}
	command := args[0]
	if command == "help" || command == "--help" || command == "-h" {
		fmt.Print(usage)
		return nil
	}
	if command == "version" || command == "--version" {
		fmt.Println(version)
		return nil
	}
	if command == "runner" {
		// A runner needs no checkout and no database: it gets its job from the core.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runRunner(ctx, args[1:])
	}
	// These commands talk to a control plane somewhere else, or read a pack. They need no
	// checkout.
	switch command {
	case "model":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runModel(ctx, args[1:], os.Stdout)
	case "pack":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runPack(ctx, args[1:], os.Stdout)
	case "login":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runLogin(ctx, args[1:])
	case "token":
		return runToken(args[1:])
	case "git-credential":
		return runGitCredential(args[1:])
	}
	o, err := parse(command, args[1:])
	if err != nil {
		return err
	}
	if o.sourceDir != "" {
		if err := loadDotEnv(o.sourceDir); err != nil {
			return err
		}
	}
	if o.dataDir != "" {
		if err := loadEnvFile(filepath.Join(o.dataDir, "secrets.env")); err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch command {
	case "setup":
		return runSetup(ctx, o)
	case "discover":
		return runDiscover(ctx, o)
	case "preflight":
		if err := preflight(setup.TargetLocal); err != nil {
			return err
		}
		fmt.Println("All required tools are present and Docker answers.")
		return nil
	case "bootstrap":
		if err := preflight(setup.TargetLocal); err != nil {
			return err
		}
		return bootstrap(ctx, o)
	case "up":
		if o.cloud == "local" {
			if err := preflight(setup.TargetLocal); err != nil {
				return err
			}
		}
		if err := loadParameters(ctx, o, awsparams.NewStore); err != nil {
			return err
		}
		return up(ctx, o)
	case "destroy":
		return destroy(ctx, o)
	case "reset":
		return newResetter(os.Stdout).run(ctx, o)
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
