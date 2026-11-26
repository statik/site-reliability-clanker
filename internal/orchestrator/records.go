package orchestrator

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/planfacts"
)

// The release records of the application pack are in the platform repository, at
// pack.RecordPath. A record says whether its release changes the schema of the database.
// The core does not roll back a change to such a release by itself, and it warns the
// approver of a plan that lowers the version across one.

// rollbackRefusal returns why the core does not roll back the change of a work item whose
// verification failed, or "" when it rolls it back. It refuses when the pull request sets
// the version variable of the application to a release whose record says that it changed
// the database schema: the old release can fail on the new schema, or damage the data. It
// also refuses when it cannot read the change or the record, because it cannot tell.
func (o *Orchestrator) rollbackRefusal(wi WorkItem) string {
	const next = "the control plane does not roll back; restore the backup from before " +
		"the apply (T3) or fix forward"
	version, err := o.versionSet(wi)
	if err == nil && version != "" {
		var changed bool
		changed, err = o.changesSchema(wi.Commit, version)
		if err == nil && changed {
			return fmt.Sprintf("release %s changed the database schema; %s", version, next)
		}
	}
	if err != nil {
		return fmt.Sprintf("the control plane cannot tell whether the change altered the "+
			"database schema (%v); %s", err, next)
	}
	return ""
}

// versionSet returns the release that the pull request of a work item sets the version
// variable of the application to, or "" when the change does not set it.
func (o *Orchestrator) versionSet(wi WorkItem) (string, error) {
	after, err := o.versionAt(wi.Env, wi.Commit)
	if err != nil || after == "" {
		return "", err
	}
	before, err := o.versionAt(wi.Env, wi.BaseCommit)
	if err != nil || before == after {
		return "", err
	}
	return after, nil
}

// versionAt returns the value of the version variable of the application in the
// terraform.tfvars of its layer at a commit, or "" when the file or the line is not there.
func (o *Orchestrator) versionAt(env, commit string) (string, error) {
	path := fmt.Sprintf("platform/stacks/%s/%s/terraform.tfvars", o.Envs[env].Cloud,
		o.App.Layer)
	content, err := o.Forge.ReadFile(PlatformRepo, commit, path)
	if errors.Is(err, forge.ErrFileNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s at %s: %w", path, commit, err)
	}
	line := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(o.App.VersionVariable) +
		`\s*=\s*(.*)$`)
	match := line.FindStringSubmatch(content)
	if match == nil {
		return "", nil
	}
	return strings.Trim(strings.TrimSpace(match[1]), `"`), nil
}

// changesSchema reports whether the record of a release at a commit of the platform
// repository says that the release changes the database schema. A release with no record
// does not.
func (o *Orchestrator) changesSchema(commit, version string) (bool, error) {
	if !pack.IsRelease(version) {
		return false, nil
	}
	path := o.App.RecordPath(version)
	raw, err := o.Forge.ReadFile(PlatformRepo, commit, path)
	if errors.Is(err, forge.ErrFileNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read the record %s: %w", path, err)
	}
	record, err := pack.ParseRecord([]byte(raw))
	if err != nil {
		return false, fmt.Errorf("the record %s: %w", path, err)
	}
	return *record.SchemaChange, nil
}

// versionWarnings warns the approver of a plan of the application layer that lowers the
// version across a release whose record, at the commit of the work item, says that it
// changed the database schema. A drift plan does that after an upgrade whose apply failed
// in a migration: main still names the old release, and the new one runs.
func (o *Orchestrator) versionWarnings(wi WorkItem, planJSON []byte) ([]string, error) {
	running, planned, err := planfacts.OutputChange(planJSON, o.App.VersionVariable)
	if err != nil || !pack.Newer(running, planned) {
		return nil, err
	}
	dir := o.App.RecordsDir()
	entries, err := o.Forge.Tree(PlatformRepo, wi.Commit, dir)
	if errors.Is(err, forge.ErrDirectoryNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list the release records in %s: %w", dir, err)
	}
	crossed := []string{}
	for _, entry := range entries {
		version, isRecord := strings.CutSuffix(entry.Name, ".json")
		if !isRecord || !pack.Newer(version, planned) || pack.Newer(version, running) {
			continue
		}
		changed, err := o.changesSchema(wi.Commit, version)
		if err != nil {
			return nil, err
		}
		if changed {
			crossed = append(crossed, version)
		}
	}
	if len(crossed) == 0 {
		return nil, nil
	}
	releases := "release " + crossed[0] + ", which"
	if len(crossed) > 1 {
		releases = "releases " + strings.Join(crossed, ", ") + ", which each"
	}
	return []string{fmt.Sprintf("This plan lowers %s from %s to %s across %s changed the "+
		"database schema. Release %s can fail on the newer schema or damage the data. "+
		"Restore the backup from before the upgrade (T3) or fix forward instead.",
		o.App.VersionVariable, running, planned, releases, planned)}, nil
}
