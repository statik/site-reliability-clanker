package pack_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
)

var archive = []byte("a chart archive\n")

// archiveSum is the SHA-256 of archive, as a record names it.
func archiveSum() string {
	sum := sha256.Sum256(archive)
	return hex.EncodeToString(sum[:])
}

const (
	digest       = "sha256:513e0b68126eaa28eb89c1eb3fcbde1e01fa055cd439e1d766924e5fe248fac2"
	remoteRecord = `{"chart_repository": "oci://ghcr.io/coder/chart", "chart_name": "coder", ` +
		`"chart_version": "2.38.11", "chart_digest": "` + digest + `", ` +
		`"values": {"coder": {"image": {"tag": "v2.38.11"}}}, "schema_change": true}`
)

// vendored returns a record with the chart archive charts/harbor.tgz.
func vendored() string {
	return `{"chart": "charts/harbor.tgz", "chart_sha256": "` + archiveSum() +
		`", "schema_change": false}`
}

// withRecords returns the files of a pack with the notes 3.4.0 and 3.5.0, a chart
// archive, and a record with the given content for each version in records.
func withRecords(t *testing.T, change func(map[string]any),
	records map[string]string) fstest.MapFS {
	t.Helper()
	files := packFiles(manifest(t, change))
	files["p/notes/3.5.0.md"] = &fstest.MapFile{Data: []byte("# 3.5.0\n")}
	files["p/charts/harbor.tgz"] = &fstest.MapFile{Data: archive}
	for version, content := range records {
		files["p/notes/"+version+".json"] = &fstest.MapFile{Data: []byte(content)}
	}
	return files
}

func keep(map[string]any) {}

func noChart(m map[string]any) { delete(m["deploy"].(map[string]any), "chart") }

func TestAPackLoadsTheRecordsOfItsReleases(t *testing.T) {
	files := withRecords(t, keep, map[string]string{"3.4.0": vendored(), "3.5.0": remoteRecord})
	app, err := pack.Load(files, "p")
	if err != nil {
		t.Fatalf("the pack does not load: %v", err)
	}
	if !app.HasRecords() || !slices.Equal(app.RecordVersions(), []string{"3.4.0", "3.5.0"}) {
		t.Fatalf("records %v", app.RecordVersions())
	}
	remote, ok := app.Record("3.5.0")
	if !ok || remote.ChartRepository != "oci://ghcr.io/coder/chart" || remote.ChartName != "coder" ||
		remote.ChartVersion != "2.38.11" || remote.ChartDigest != digest ||
		!*remote.SchemaChange || !remote.NamesChart() {
		t.Errorf("record 3.5.0 = %+v", remote)
	}
	local, _ := app.Record("3.4.0")
	if local.Chart != "charts/harbor.tgz" || local.ChartSHA256 != archiveSum() ||
		*local.SchemaChange {
		t.Errorf("record 3.4.0 = %+v", local)
	}
	if _, ok := app.Record("9.9.9"); ok {
		t.Error("a release with no record has one")
	}
	if got := app.RecordPath("3.5.0"); got != "apps/harbor/releases/3.5.0.json" {
		t.Errorf("RecordPath = %q", got)
	}
}

func TestAReleaseRecordCanNameAChartInAnHTTPSRepository(t *testing.T) {
	https := `{"chart_repository": "https://helm.coder.com/v2", "chart_name": "coder", ` +
		`"chart_version": "2.38.11", "chart_digest": "sha256:` + archiveSum() +
		`", "schema_change": true}`
	files := withRecords(t, keep, map[string]string{"3.4.0": https,
		"3.5.0": `{"values": {"a": 1}, "schema_change": false}`})
	if _, err := pack.Load(files, "p"); err != nil {
		t.Fatalf("the pack does not load: %v", err)
	}
}

func TestLoadRefusesARecordThatIsNotSafe(t *testing.T) {
	with := func(field, value string) string {
		return strings.Replace(remoteRecord, `"chart_name"`, `"`+field+`": `+value+`, "chart_name"`, 1)
	}
	replace := func(old, value string) string { return strings.Replace(remoteRecord, old, value, 1) }
	cases := map[string]struct {
		record, want string
	}{
		"a field that does not exist": {with("hooks", `"x"`), "unknown field"},
		"text after the record":       {remoteRecord + "{}", "text after the record"},
		"not JSON":                    {"chart: x", "not a correct release record"},
		"no schema_change":            {`{"values": {}}`, "schema_change is required"},
		"values that are a list": {`{"values": [1], "schema_change": true}`,
			"values must be an object"},
		"values that are null": {`{"values": null, "schema_change": true}`,
			"values must be an object"},
		"an archive with no SHA-256": {`{"chart": "charts/harbor.tgz", "schema_change": true}`,
			"chart_sha256 is \"\""},
		"an archive that the pack does not have": {strings.Replace(vendored(), "harbor.tgz",
			"other.tgz", 1), "charts/other.tgz is not a file of the pack"},
		"an archive with another SHA-256": {strings.Replace(vendored(), archiveSum(),
			strings.Repeat("0", 64), 1), "has the SHA-256 " + archiveSum()},
		"a SHA-256 in capitals": {strings.Replace(vendored(), archiveSum(),
			strings.ToUpper(archiveSum()), 1), "64 lowercase hexadecimal"},
		"an archive outside the charts directory": {strings.Replace(vendored(),
			"charts/harbor.tgz", "chart/../charts/harbor.tgz", 1), "chart is \"chart/../"},
		"an archive in the platform": {strings.Replace(vendored(), "charts/harbor.tgz",
			"../../platform/x.tgz", 1), "chart is \"../../platform/x.tgz\""},
		"an archive that is not a tgz": {strings.Replace(vendored(), "charts/harbor.tgz",
			"charts/harbor.sh", 1), "chart is \"charts/harbor.sh\""},
		"an archive and a repository": {strings.Replace(vendored(), `"schema_change"`,
			`"chart_repository": "oci://ghcr.io/x", "schema_change"`, 1), "not both"},
		"an archive with a version": {strings.Replace(vendored(), `"schema_change"`,
			`"chart_version": "1.0.0", "schema_change"`, 1), "has no chart_name, chart_version"},
		"a repository with a SHA-256 of an archive": {with("chart_sha256", `"`+archiveSum()+`"`),
			"has chart_digest, not chart_sha256"},
		"a repository over plain HTTP": {replace("oci://ghcr.io/coder/chart",
			"http://helm.coder.com/v2"), "chart_repository is \"http://"},
		"a repository with a user": {replace("oci://ghcr.io/coder/chart",
			"https://user:secret@helm.coder.com/v2"), "chart_repository"},
		"a repository with a query": {replace("oci://ghcr.io/coder/chart",
			"https://helm.coder.com/v2?token=x"), "chart_repository"},
		"a repository with a step up": {replace("oci://ghcr.io/coder/chart",
			"https://helm.coder.com/v2/../x"), "no \".\" or \"..\" step"},
		"a repository on another scheme": {replace("oci://ghcr.io/coder/chart",
			"file:///etc"), "chart_repository"},
		"a chart name with a path": {replace(`"chart_name": "coder"`,
			`"chart_name": "../coder"`), "chart_name is \"../coder\""},
		"a range of chart versions": {replace(`"chart_version": "2.38.11"`,
			`"chart_version": "^2.38.0"`), "chart_version is \"^2.38.0\""},
		"a chart version with a v": {replace(`"chart_version": "2.38.11"`,
			`"chart_version": "v2.38.11"`), "chart_version is \"v2.38.11\""},
		"a digest with no algorithm": {replace(digest, strings.TrimPrefix(digest, "sha256:")),
			"chart_digest is"},
		"a repository with no digest": {replace(`"chart_digest": "`+digest+`", `, ""),
			"chart_digest is \"\""},
		"a chart field and no chart": {`{"chart_digest": "` + digest +
			`", "schema_change": true}`, "has no other field of a chart"},
	}
	for name, tc := range cases {
		files := withRecords(t, keep, map[string]string{"3.4.0": tc.record,
			"3.5.0": vendored()})
		_, err := pack.Load(files, "p")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want one with %q", name, err, tc.want)
		}
	}
}

func TestLoadChecksThatTheNotesAndTheRecordsPairUp(t *testing.T) {
	plain := `{"schema_change": false}`
	cases := map[string]struct {
		change  func(map[string]any)
		records map[string]string
		extra   map[string]string
		want    string
	}{
		"a note with no record when the pack has records": {keep,
			map[string]string{"3.4.0": plain}, nil, "the release 3.5.0 has no record"},
		"a record with no note": {keep, map[string]string{"3.4.0": plain, "3.5.0": plain,
			"3.6.0": plain}, nil, "the record 3.6.0.json has no note"},
		"a file that is not a note or a record": {keep, nil,
			map[string]string{"p/notes/README.txt": "x"}, "notes/README.txt is not a note"},
		"a note with a name that is not a version": {keep, nil,
			map[string]string{"p/notes/latest.md": "x"}, "notes/latest.md is not a note"},
		"a directory in the releases": {keep, nil,
			map[string]string{"p/notes/3.6.0/x.md": "x"}, "notes/3.6.0 is not a note"},
		"no chart and no records": {noChart, nil, nil,
			"deploy.chart is required unless every release has a record that names a chart"},
		"no chart and a record with no chart": {noChart, map[string]string{"3.4.0": plain,
			"3.5.0": vendored()}, nil, "deploy.chart is required"},
		"a record over the limit": {keep, map[string]string{"3.4.0": `{"values": {"a": "` +
			strings.Repeat("x", 64<<10) + `"}, "schema_change": true}`, "3.5.0": plain}, nil,
			"the limit is"},
	}
	for name, tc := range cases {
		files := withRecords(t, tc.change, tc.records)
		for file, content := range tc.extra {
			files[file] = &fstest.MapFile{Data: []byte(content)}
		}
		_, err := pack.Load(files, "p")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want one with %q", name, err, tc.want)
		}
	}
}

func TestAPackWhoseRecordsAllNameAChartNeedsNoMainChart(t *testing.T) {
	files := withRecords(t, noChart, map[string]string{"3.4.0": vendored(),
		"3.5.0": remoteRecord})
	delete(files, "p/chart/Chart.yaml")
	app, err := pack.Load(files, "p")
	if err != nil {
		t.Fatalf("the pack does not load: %v", err)
	}
	if app.Deploy.Chart != "" {
		t.Errorf("deploy.chart = %q", app.Deploy.Chart)
	}
	trees, err := app.PlatformFiles()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := trees["apps/harbor/chart"]; ok {
		t.Error("the platform files have a main chart")
	}
}

// Every deploy file of a pack goes below apps/<name>/ of the platform repository, at a
// name that the core chooses: a pack cannot choose a path there.
func TestThePlatformFilesOfAPackAreBelowItsDirectory(t *testing.T) {
	app := packtest.SecondWithRecords(t)
	trees, err := app.PlatformFiles()
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for dir, tree := range trees {
		err := fs.WalkDir(tree, ".", func(name string, entry fs.DirEntry, err error) error {
			if err == nil && !entry.IsDir() {
				got = append(got, dir+"/"+name)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(got)
	want := []string{"apps/harbor/chart/Chart.yaml", "apps/harbor/charts/harbor-3.5.0.tgz",
		"apps/harbor/extras/Chart.yaml", "apps/harbor/extras/templates/job.yaml",
		"apps/harbor/releases/3.4.0.json", "apps/harbor/releases/3.5.0.json",
		"apps/harbor/values/values-aws.yaml", "apps/harbor/values/values.yaml"}
	if !slices.Equal(got, want) {
		t.Errorf("platform files\n got %v\nwant %v", got, want)
	}
	content, err := fs.ReadFile(trees["apps/harbor/charts"], "harbor-3.5.0.tgz")
	if err != nil || string(content) != string(packtest.Archive) {
		t.Errorf("the archive has %q, %v", content, err)
	}
	none, err := packtest.Second(t).PlatformFiles()
	if err != nil || none["apps/harbor/releases"] != nil || none["apps/harbor/charts"] != nil {
		t.Errorf("a pack with no records has records in the platform: %v, %v", none, err)
	}
}

func TestParseRecordReadsTheFileOfARecordFromTheForge(t *testing.T) {
	record, err := pack.ParseRecord([]byte(remoteRecord))
	if err != nil || !*record.SchemaChange || record.ChartName != "coder" {
		t.Fatalf("record %+v, %v", record, err)
	}
	refused := map[string]string{
		`{"schema_change": true, "hooks": "x"}`: "unknown field",
		`{"values": {}}`:                        "schema_change is required",
		`{"schema_change": false}{}`:            "text after the record",
		`{"values": [], "schema_change": true}`: "values must be an object",
		`schema_change: true`:                   "not a correct release record",
	}
	for raw, want := range refused {
		if _, err := pack.ParseRecord([]byte(raw)); err == nil ||
			!strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %v, want one with %q", raw, err, want)
		}
	}
}

func TestReleaseNamesAndTheirOrder(t *testing.T) {
	for version, want := range map[string]bool{"3.5.0": true, "1.2.0-bad": true,
		"v3.5.0": false, "3.5": false, "../3.5.0": false, "3.5.0-rc.1": false} {
		if got := pack.IsRelease(version); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", version, got, want)
		}
	}
	newer := [][2]string{{"3.5.0", "3.4.9"}, {"10.0.0", "9.9.9"}, {"3.4.10", "3.4.9"},
		{"4.0.0", "3.99.99"}}
	for _, pair := range newer {
		if !pack.Newer(pair[0], pair[1]) || pack.Newer(pair[1], pair[0]) {
			t.Errorf("want %s newer than %s, and not the other way", pair[0], pair[1])
		}
	}
	if pack.Newer("1.2.0-bad", "1.2.0") || pack.Newer("1.2.0", "1.2.0-bad") ||
		pack.Newer("3.5.0", "3.5.0") {
		t.Error("a suffix, or the same version, must not be newer")
	}
}
