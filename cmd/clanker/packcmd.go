package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/bundle"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/packstore"
	"github.com/statik/site-reliability-clanker/internal/store"
)

const packUsage = `Usage:
  clanker pack validate DIR
      Check the application pack in the directory DIR. Prints one line for each check.
      The exit status is 1 when a check fails.
  clanker pack import URL --tag TAG [--dir NAME] [--data DIR]
      Clone the tag TAG of the git repository at URL (https, ssh, or a directory of this
      machine), check the pack in its directory NAME (default: the root), and copy it to
      DIR/packs/<name>/<version>. Start the control plane with that directory as --pack.
`

// runPack runs `clanker pack`. Its output is plain text with no prompt, so a program can
// use it as a person does.
func runPack(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("clanker pack needs a command\n\n%s", packUsage)
	}
	switch args[0] {
	case "validate":
		return packValidate(args[1:], out)
	case "import":
		return packImport(ctx, args[1:], out)
	case "help", "--help", "-h":
		fmt.Fprint(out, packUsage)
		return nil
	}
	return fmt.Errorf("clanker pack %s does not exist\n\n%s", args[0], packUsage)
}

func packValidate(args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("clanker pack validate needs one directory\n\n%s", packUsage)
	}
	app, err := packstore.LoadDir(args[0])
	if err != nil {
		fmt.Fprintf(out, "FAIL  %v\n", err)
		return errors.New("the pack is not correct")
	}
	for _, line := range packFacts(app) {
		fmt.Fprintf(out, "ok    %s\n", line)
	}
	hash, err := pack.Hash(app.Files())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "ok    hash: sha256 %s\n", hash)
	return nil
}

// packFacts returns one line for each part of a pack that the checks passed.
func packFacts(app pack.Pack) []string {
	lines := []string{
		fmt.Sprintf("manifest: %s %s (%s), schema %d", app.Name, app.Version, app.Title, app.Schema),
		fmt.Sprintf("deployment: layer %s, variable %s, chart %s", app.Layer, app.VersionVariable,
			app.Deploy.Chart),
		fmt.Sprintf("releases: repository %s, first release %s", app.Releases.Repository,
			app.Releases.First),
		fmt.Sprintf("components: %s", strings.Join(componentFacts(app), ", ")),
		fmt.Sprintf("diagnostics: %d reads, and the state of the pods and the deployments",
			len(app.Diagnostics)),
		fmt.Sprintf("checks: %s", strings.Join(checkFacts(app), ", ")),
		"version: " + versionFact(app),
		"incident: " + incidentFact(app),
	}
	for _, part := range app.CheckParts() {
		lines = append(lines, part.Name+": "+part.Detail)
	}
	for name, dir := range map[string]string{"runbooks": app.Runbooks, "eval cases": app.Evals} {
		if dir == "" {
			lines = append(lines, name+": none")
		}
	}
	return lines
}

// componentFacts returns each component with its deployment: "api (shop-api)".
func componentFacts(app pack.Pack) []string {
	facts := []string{}
	for _, component := range app.Components {
		facts = append(facts, fmt.Sprintf("%s (%s)", component.Name, component.Workload))
	}
	return facts
}

// checkFacts returns each check with what passes it. A pack sets the path and the status,
// so a person must see them.
func checkFacts(app pack.Pack) []string {
	facts := []string{}
	for _, check := range app.Checks {
		fact := fmt.Sprintf("%s %s %d", check.Name, check.Path, check.PassStatus())
		if check.Primary {
			fact += " (primary)"
		}
		facts = append(facts, fact)
	}
	return facts
}

// versionFact says where the monitor reads the version that runs.
func versionFact(app pack.Pack) string {
	if app.AppVersion.Source == pack.VersionImage {
		return fmt.Sprintf("%s, the tag of each instance of %s, from the collector",
			pack.VersionImage, app.AppVersion.Component)
	}
	return fmt.Sprintf("%s %s", app.AppVersion.Source, app.AppVersion.Path)
}

func incidentFact(app pack.Pack) string {
	if app.Incident == nil {
		return "no mitigation without a diagnosis of a model"
	}
	return fmt.Sprintf("%s on %s without a diagnosis of a model", app.Incident.Runbook,
		app.Incident.Component)
}

func packImport(ctx context.Context, args []string, out io.Writer) error {
	request := packstore.Request{}
	flags := flag.NewFlagSet("clanker pack import", flag.ContinueOnError)
	flags.StringVar(&request.Tag, "tag", "", "tag of the version to import")
	flags.StringVar(&request.Dir, "dir", "", "directory of the pack in the repository")
	flags.StringVar(&request.DataDir, "data", "", "data directory of the control plane")
	// The address can stand before the flags or after them.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		request.URL, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if request.URL == "" && flags.NArg() == 1 {
		request.URL = flags.Arg(0)
	}
	if request.URL == "" || request.Tag == "" {
		return fmt.Errorf("clanker pack import needs an address and --tag\n\n%s", packUsage)
	}
	var err error
	if request.DataDir, err = packDataDir(request.DataDir); err != nil {
		return err
	}
	result, err := packstore.Import(ctx, request)
	if err != nil {
		return err
	}
	state := "imported"
	if result.Existed {
		state = "is on this machine already, with the same files"
	}
	fmt.Fprintf(out, "pack     %s %s %s\n", result.Pack.Name, result.Pack.Version, state)
	fmt.Fprintf(out, "source   %s tag %s commit %s\n", result.Lock.URL, result.Lock.Tag,
		result.Lock.Commit)
	fmt.Fprintf(out, "sha256   %s\n", result.Lock.SHA256)
	fmt.Fprintf(out, "dir      %s\n", result.Dir)
	fmt.Fprintf(out, "start    clanker up --pack %s\n", result.Dir)
	return nil
}

// packDataDir returns the data directory as the other commands choose it: .clanker in a
// checkout, and the directory in the home of the person for a released binary.
func packDataDir(given string) (string, error) {
	if given == "" {
		given = ".clanker"
		if bundle.Present() {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("find the home directory for the data directory: %w; "+
					"set --data", err)
			}
			given = filepath.Join(home, ".clanker")
		}
	}
	return filepath.Abs(given)
}

// packLoadedKey holds the pack that the last start recorded in the audit log.
const packLoadedKey = "pack.loaded"

// recordPack writes one audit event when the control plane starts with a pack that the
// audit log does not have yet: a first start, a new version, or other files.
func recordPack(db *sql.DB, log *audit.Log, o options) error {
	origin := o.packOrigin
	now := strings.Join([]string{o.app.Name, o.app.Version, origin.Source, origin.Tag,
		origin.Commit, origin.SHA256}, " ")
	last, err := store.GetKV(db, packLoadedKey)
	if err != nil {
		return fmt.Errorf("read the recorded application pack: %w", err)
	}
	if last == now {
		return nil
	}
	from := origin.Source
	if origin.Tag != "" {
		from = fmt.Sprintf("%s tag %s", origin.Source, origin.Tag)
	}
	if _, err := log.Append(audit.Event{Type: packLoadedKey, Actor: "clanker", Env: o.env,
		Summary: fmt.Sprintf("Application pack %s %s, from %s", o.app.Name, o.app.Version, from),
		Data: map[string]any{"name": o.app.Name, "version": o.app.Version,
			"source": origin.Source, "tag": origin.Tag, "commit": origin.Commit,
			"sha256": origin.SHA256}}); err != nil {
		return fmt.Errorf("record the application pack: %w", err)
	}
	return store.SetKV(db, packLoadedKey, now)
}
