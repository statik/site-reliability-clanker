package pack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing/fstest"
)

// The directories of a pack in the platform repository. The core puts every deploy file
// of a pack below PlatformDir, at these fixed names, so a pack cannot write a file of the
// platform and the layer finds each part at the same place for every pack.
const (
	platformAppsDir = "apps"
	chartDir        = "chart"
	valuesDir       = "values"
	extrasDir       = "extras"
	recordsDir      = "releases"
	archivesDir     = "charts"
)

// Limits of a release record and of a chart archive.
const (
	maxRecordBytes  = 64 << 10
	maxArchiveBytes = 4 << 20
)

var (
	// releasePattern is the version in the name of a release note or record. A suffix
	// marks a release that the demo builds to fail: 1.2.0-bad.
	releasePattern  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[a-z][a-z0-9]*)?$`)
	archivePattern  = regexp.MustCompile(`^` + archivesDir + `/[A-Za-z0-9][A-Za-z0-9._-]{0,99}\.tgz$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	digestPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	chartPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	semverPattern   = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]{1,40})?$`)
	chartURLPattern = regexp.MustCompile(
		`^(https|oci)://[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?(:[0-9]{1,5})?(/[A-Za-z0-9._~-]+){0,10}$`)
)

// Record is the release record of one release: the file <version>.json beside the note of
// the release. It says what the layer installs for that version of the application.
//
// The chart of a record has one of two forms, or none:
//   - A chart archive in the pack: Chart and ChartSHA256. The layer checks the SHA-256 of
//     the archive before it installs it.
//   - A chart in a repository: ChartRepository, ChartName, ChartVersion and ChartDigest.
//     The runner fetches it at apply. For an oci:// repository the digest is the digest of
//     the OCI manifest of the chart, which Helm pins and checks. For an https://
//     repository it is the `digest` of the chart in the index of the repository (the
//     SHA-256 of the archive), with "sha256:" before it; Helm does not check it, so it
//     records the archive that a person reviewed.
//
// Without a chart the layer installs the chart of deploy.chart.
type Record struct {
	Chart           string `json:"chart,omitempty"`
	ChartSHA256     string `json:"chart_sha256,omitempty"`
	ChartRepository string `json:"chart_repository,omitempty"`
	ChartName       string `json:"chart_name,omitempty"`
	ChartVersion    string `json:"chart_version,omitempty"`
	ChartDigest     string `json:"chart_digest,omitempty"`
	// Values are Helm values for this release, for example the image tag and digest.
	Values json.RawMessage `json:"values,omitempty"`
	// SchemaChange says that the release changes the schema of the database, so the core
	// does not roll it back by itself. It is required.
	SchemaChange *bool `json:"schema_change"`
}

// NamesChart reports whether the record names a chart of its own.
func (r Record) NamesChart() bool { return r.Chart != "" || r.ChartRepository != "" }

// release is one release of a pack: its note and its record, when it has one.
type release struct {
	record *Record
	// raw is the file of the record, as the pack has it.
	raw []byte
}

// PlatformDir returns the directory of the pack in the platform repository.
func (p Pack) PlatformDir() string { return path.Join(platformAppsDir, p.Name) }

// RecordsDir returns the directory of the release records in the platform repository.
func (p Pack) RecordsDir() string { return path.Join(p.PlatformDir(), recordsDir) }

// RecordPath returns the path of the record of a release in the platform repository.
func (p Pack) RecordPath(version string) string {
	return path.Join(p.RecordsDir(), version+".json")
}

// HasRecords reports whether the releases of the pack have records.
func (p Pack) HasRecords() bool { return len(p.records) > 0 }

// RecordVersions returns the versions that have a record, in the order of the names.
func (p Pack) RecordVersions() []string {
	versions := make([]string, 0, len(p.records))
	for version := range p.records {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	return versions
}

// Record returns the record of a release of the pack.
func (p Pack) Record(version string) (Record, bool) {
	entry, ok := p.records[version]
	if !ok {
		return Record{}, false
	}
	return *entry.record, true
}

// IsRelease reports whether a version can be the name of a release: its note and its
// record have that name.
func IsRelease(version string) bool { return releasePattern.MatchString(version) }

// Newer reports whether version a is newer than version b. A suffix such as "-bad" does not
// change the order.
func Newer(a, b string) bool {
	va, vb := versionOrder(a), versionOrder(b)
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

// versionOrder turns "1.2.0-bad" into comparable numbers.
func versionOrder(v string) [3]int {
	var out [3]int
	core, _, _ := strings.Cut(v, "-")
	for i, part := range strings.SplitN(core, ".", 3) {
		out[i], _ = strconv.Atoi(part)
	}
	return out
}

// ReleaseOf returns the version of a file in the directory of the release notes, and
// whether the file is a release note. A record or another file is not a release.
func (p Pack) ReleaseOf(file string) (string, bool) {
	version, ok := strings.CutSuffix(file, ".md")
	return version, ok && releasePattern.MatchString(version)
}

// loadReleases reads the directory of the releases: one note <version>.md for each
// release and, for a pack with records, one record <version>.json beside each note. It
// returns the records by version.
func (p Pack) loadReleases() (map[string]release, error) {
	dir := p.Releases.Directory
	entries, err := fs.ReadDir(p.files, dir)
	if err != nil {
		return nil, fmt.Errorf("the pack does not have the directory %s", dir)
	}
	notes, records := []string{}, map[string]release{}
	for _, entry := range entries {
		version, extension, err := releaseFile(dir, entry)
		if err != nil {
			return nil, err
		}
		if extension == ".md" {
			notes = append(notes, version)
			continue
		}
		if records[version], err = p.readRecord(path.Join(dir, entry.Name())); err != nil {
			return nil, err
		}
	}
	return records, p.checkReleaseSet(notes, records)
}

// releaseFile returns the version and the extension of a file in the directory of the
// releases. Only a note <version>.md and a record <version>.json can be there.
func releaseFile(dir string, entry fs.DirEntry) (string, string, error) {
	name := entry.Name()
	extension := path.Ext(name)
	version := strings.TrimSuffix(name, extension)
	if !entry.Type().IsRegular() || !releasePattern.MatchString(version) ||
		(extension != ".md" && extension != ".json") {
		return "", "", fmt.Errorf("%s/%s is not a note <version>.md or a record "+
			"<version>.json; a version must match %s", dir, name, releasePattern)
	}
	return version, extension, nil
}

// checkReleaseSet checks that the notes and the records pair up, and the rule of the main
// chart: deploy.chart is required unless every release has a record that names a chart.
func (p Pack) checkReleaseSet(notes []string, records map[string]release) error {
	if len(records) > 0 {
		if err := pairUp(notes, records); err != nil {
			return err
		}
	}
	if p.Deploy.Chart == "" && !everyReleaseNamesAChart(notes, records) {
		return fmt.Errorf("deploy.chart is required unless every release has a record that " +
			"names a chart")
	}
	return nil
}

// pairUp checks that each record has a note and each note has a record.
func pairUp(notes []string, records map[string]release) error {
	for _, version := range (Pack{records: records}).RecordVersions() {
		if !slices.Contains(notes, version) {
			return fmt.Errorf("the record %s.json has no note %s.md", version, version)
		}
	}
	for _, version := range notes {
		if _, ok := records[version]; !ok {
			return fmt.Errorf("the release %s has no record %s.json; when a pack has "+
				"records, every release must have one", version, version)
		}
	}
	return nil
}

func everyReleaseNamesAChart(notes []string, records map[string]release) bool {
	if len(records) == 0 {
		return false
	}
	for _, version := range notes {
		if found, ok := records[version]; !ok || !found.record.NamesChart() {
			return false
		}
	}
	return true
}

// readRecord reads and checks one record. Like the manifest, it refuses a field that it
// does not know.
func (p Pack) readRecord(file string) (release, error) {
	info, err := fs.Stat(p.files, file)
	if err != nil {
		return release{}, err
	}
	if info.Size() > maxRecordBytes {
		return release{}, fmt.Errorf("%s has %d bytes; the limit is %d", file, info.Size(),
			maxRecordBytes)
	}
	raw, err := fs.ReadFile(p.files, file)
	if err != nil {
		return release{}, err
	}
	record, err := ParseRecord(raw)
	if err == nil {
		err = p.checkChart(record)
	}
	if err != nil {
		return release{}, fmt.Errorf("%s: %w", file, err)
	}
	return release{record: &record, raw: raw}, nil
}

// ParseRecord reads one release record from the content of its file. Like the manifest, it
// refuses a field that it does not know, and schema_change is required. It does not check
// the chart of the record against the files of a pack; Load does that.
func ParseRecord(raw []byte) (Record, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return Record{}, fmt.Errorf("not a correct release record: %w", err)
	}
	if decoder.More() {
		return Record{}, fmt.Errorf("text after the record object")
	}
	if record.SchemaChange == nil {
		return Record{}, fmt.Errorf("schema_change is required: true when the release " +
			"changes the schema of the database, false when it does not")
	}
	if values := bytes.TrimSpace(record.Values); len(values) > 0 && values[0] != '{' {
		return Record{}, fmt.Errorf("values must be an object of Helm values")
	}
	return record, nil
}

// checkChart checks the chart of a record: an archive of the pack, a chart in a
// repository, or none.
func (p Pack) checkChart(record Record) error {
	switch {
	case record.Chart != "" && record.ChartRepository != "":
		return fmt.Errorf("a record names a chart archive or a chart repository, not both")
	case record.Chart != "":
		return p.checkArchive(record)
	case record.ChartRepository != "":
		return checkRemoteChart(record)
	case record.ChartSHA256 != "" || hasRemoteFields(record):
		return fmt.Errorf("a record with no chart and no chart_repository has no other " +
			"field of a chart")
	}
	return nil
}

// hasRemoteFields reports whether a record has a field of a chart in a repository, other
// than the repository.
func hasRemoteFields(record Record) bool {
	return record.ChartName != "" || record.ChartVersion != "" || record.ChartDigest != ""
}

// checkArchive checks a chart archive of the pack against the SHA-256 of its record.
func (p Pack) checkArchive(record Record) error {
	if hasRemoteFields(record) {
		return fmt.Errorf("a record with a chart archive has no chart_name, chart_version " +
			"or chart_digest")
	}
	if !archivePattern.MatchString(record.Chart) {
		return fmt.Errorf("chart is %q; it must match %s", record.Chart, archivePattern)
	}
	if !sha256Pattern.MatchString(record.ChartSHA256) {
		return fmt.Errorf("chart_sha256 is %q; it must be 64 lowercase hexadecimal characters",
			record.ChartSHA256)
	}
	got, err := p.archiveSHA256(record.Chart)
	if err != nil {
		return err
	}
	if got != record.ChartSHA256 {
		return fmt.Errorf("the chart archive %s has the SHA-256 %s, and the record says %s",
			record.Chart, got, record.ChartSHA256)
	}
	return nil
}

// archiveSHA256 returns the SHA-256 of a chart archive of the pack.
func (p Pack) archiveSHA256(file string) (string, error) {
	info, err := fs.Stat(p.files, file)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("the chart archive %s is not a file of the pack", file)
	}
	if info.Size() > maxArchiveBytes {
		return "", fmt.Errorf("the chart archive %s has %d bytes; the limit is %d", file,
			info.Size(), maxArchiveBytes)
	}
	content, err := fs.ReadFile(p.files, file)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

// checkRemoteChart checks a chart in a repository. The address has no user, no query and
// no step out of its path, and the version and the digest are exact.
func checkRemoteChart(record Record) error {
	if record.ChartSHA256 != "" {
		return fmt.Errorf("a record with a chart_repository has chart_digest, not chart_sha256")
	}
	url := record.ChartRepository
	if !chartURLPattern.MatchString(url) || hasDotStep(url) {
		return fmt.Errorf("chart_repository is %q; it must be an https:// or oci:// address "+
			"that matches %s, with no \".\" or \"..\" step", url, chartURLPattern)
	}
	if !chartPattern.MatchString(record.ChartName) {
		return fmt.Errorf("chart_name is %q; it must match %s", record.ChartName, chartPattern)
	}
	if !semverPattern.MatchString(record.ChartVersion) {
		return fmt.Errorf("chart_version is %q; it must be one exact version that matches %s",
			record.ChartVersion, semverPattern)
	}
	if !digestPattern.MatchString(record.ChartDigest) {
		return fmt.Errorf("chart_digest is %q; it must match %s", record.ChartDigest,
			digestPattern)
	}
	return nil
}

// PlatformFiles returns the deploy files of the pack, by their directory in the platform
// repository. Every directory is below PlatformDir: the main chart, the values, the extras
// chart, the release records and the chart archives that the records name.
func (p Pack) PlatformFiles() (map[string]fs.FS, error) {
	trees := map[string]fs.FS{}
	for name, dir := range map[string]string{chartDir: p.Deploy.Chart,
		valuesDir: p.Deploy.Values, extrasDir: p.Deploy.Extras} {
		if dir == "" {
			continue
		}
		tree, err := fs.Sub(p.files, dir)
		if err != nil {
			return nil, fmt.Errorf("open %s of the pack: %w", dir, err)
		}
		trees[path.Join(p.PlatformDir(), name)] = tree
	}
	if !p.HasRecords() {
		return trees, nil
	}
	records, archives, err := p.recordFiles()
	if err != nil {
		return nil, err
	}
	trees[path.Join(p.PlatformDir(), recordsDir)] = records
	if len(archives) > 0 {
		trees[path.Join(p.PlatformDir(), archivesDir)] = archives
	}
	return trees, nil
}

// recordFiles returns the records of the pack, by <version>.json, and the chart archives
// that they name, by the name of the file.
func (p Pack) recordFiles() (fstest.MapFS, fstest.MapFS, error) {
	records, archives := fstest.MapFS{}, fstest.MapFS{}
	for version, entry := range p.records {
		records[version+".json"] = &fstest.MapFile{Data: entry.raw, Mode: 0o644}
		if entry.record.Chart == "" {
			continue
		}
		content, err := fs.ReadFile(p.files, entry.record.Chart)
		if err != nil {
			return nil, nil, fmt.Errorf("read the chart archive %s: %w", entry.record.Chart, err)
		}
		archives[path.Base(entry.record.Chart)] = &fstest.MapFile{Data: content, Mode: 0o644}
	}
	return records, archives, nil
}
