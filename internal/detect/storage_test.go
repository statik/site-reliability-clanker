package detect

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

const (
	mib = int64(1) << 20
	gib = int64(1) << 30
)

var day0 = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

// daily returns one sample for each day from day0 with the given used bytes.
func daily(total int64, used ...int64) []monitor.VolumeSample {
	samples := []monitor.VolumeSample{}
	for i, u := range used {
		samples = append(samples, monitor.VolumeSample{
			TS:  store.FormatTime(day0.Add(time.Duration(i) * 24 * time.Hour)),
			Env: "local", Volume: monitor.DatabaseVolume, UsedBytes: u, TotalBytes: total})
	}
	return samples
}

func TestForecastVolumeWithSteadyGrowth(t *testing.T) {
	// 1000 MiB on day 0 and 40 MiB more each day: 2048 MiB is reached after 26.2 days.
	samples := daily(2*gib, 1000*mib, 1040*mib, 1080*mib, 1120*mib, 1160*mib, 1200*mib)
	f, ok := ForecastVolume(samples)
	if !ok {
		t.Fatal("no forecast for steady growth")
	}
	if math.Abs(f.GrowthPerDay-float64(40*mib)) > 1 {
		t.Errorf("growth = %.0f bytes each day, want %d", f.GrowthPerDay, 40*mib)
	}
	want := day0.Add(time.Duration(26.2 * 24 * float64(time.Hour)))
	if diff := f.FullAt.Sub(want); diff < -time.Minute || diff > time.Minute {
		t.Errorf("full at %s, want %s", f.FullAt, want)
	}
	if f.UsedBytes != 1200*mib || f.TotalBytes != 2*gib {
		t.Errorf("used %d of %d, want the newest sample", f.UsedBytes, f.TotalBytes)
	}
}

func TestForecastVolumeWithNoisyGrowth(t *testing.T) {
	// The line through these samples has a slope of 40 MiB each day; no sample is on it.
	samples := daily(2*gib, 1010*mib, 1030*mib, 1090*mib, 1110*mib, 1170*mib, 1190*mib)
	f, ok := ForecastVolume(samples)
	if !ok {
		t.Fatal("no forecast for noisy growth")
	}
	slope := f.GrowthPerDay / float64(mib)
	if slope < 38 || slope > 42 {
		t.Errorf("growth = %.1f MiB each day, want about 40", slope)
	}
	days := f.FullAt.Sub(day0).Hours() / 24
	if days < 24 || days > 28 {
		t.Errorf("full after %.1f days, want about 26", days)
	}
}

func TestForecastVolumeGivesNoForecast(t *testing.T) {
	sameTime := daily(2*gib, 100*mib, 200*mib, 300*mib, 400*mib, 500*mib, 600*mib)
	for i := range sameTime {
		sameTime[i].TS = store.FormatTime(day0)
	}
	cases := map[string][]monitor.VolumeSample{
		"flat":            daily(2*gib, 500*mib, 500*mib, 500*mib, 500*mib, 500*mib, 500*mib),
		"shrinking":       daily(2*gib, 900*mib, 880*mib, 860*mib, 840*mib, 820*mib, 800*mib),
		"too few samples": daily(2*gib, 1000*mib, 1100*mib, 1200*mib, 1300*mib, 1400*mib),
		"no samples":      nil,
		"one time":        sameTime,
	}
	for label, samples := range cases {
		if f, ok := ForecastVolume(samples); ok {
			t.Errorf("%s: forecast %+v, want none", label, f)
		}
	}
}

func TestForecastVolumeThatIsFullAlready(t *testing.T) {
	samples := daily(2*gib, 1800*mib, 1900*mib, 2000*mib, 2048*mib, 2048*mib, 2048*mib)
	f, ok := ForecastVolume(samples)
	if !ok {
		t.Fatal("no forecast for a full volume")
	}
	if want := day0.Add(5 * 24 * time.Hour); !f.FullAt.Equal(want) {
		t.Errorf("full at %s, want the time of the newest sample %s", f.FullAt, want)
	}
}

func TestGrownSizeHoldsNinetyDaysOfGrowth(t *testing.T) {
	cases := []struct {
		label        string
		used, total  int64
		growthPerDay int64
		want         int
	}{
		// 1508 MiB + 90 x 40 MiB = 5108 MiB, which needs 5 GiB.
		{"rounds up to the next GiB", 1508 * mib, 2 * gib, 40 * mib, 5},
		// 1024 MiB + 90 x 1 MiB fits in 2 GiB, so the step is the least step.
		{"grows by at least 1 GiB", 1024 * mib, 2 * gib, 1 * mib, 3},
		// 2048 MiB + 90 x 1024 MiB = 92 GiB with no part left over.
		{"keeps an exact size", 2 * gib, 2 * gib, 1 * gib, 92},
	}
	for _, c := range cases {
		f := VolumeForecast{UsedBytes: c.used, TotalBytes: c.total,
			GrowthPerDay: float64(c.growthPerDay)}
		if got := f.GrownSizeGB(); got != c.want {
			t.Errorf("%s: grown size = %d GiB, want %d", c.label, got, c.want)
		}
	}
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type storageFixture struct {
	detector *Detector
	raised   chan Finding
}

// newStorageFixture returns a detector whose clock is at the newest of the samples.
func newStorageFixture(t *testing.T, samples []monitor.VolumeSample) storageFixture {
	t.Helper()
	db := storetest.Open(t)
	clock := fixedClock{store.ParseTime(samples[len(samples)-1].TS)}
	fixture := storageFixture{raised: make(chan Finding, 4)}
	fixture.detector = &Detector{DB: db, Audit: audit.New(db, clock),
		Monitor: monitor.New(db, clock), Clock: clock,
		OnFinding: func(_ context.Context, f Finding) { fixture.raised <- f }}
	fixture.record(t, samples)
	return fixture
}

func (s storageFixture) record(t *testing.T, samples []monitor.VolumeSample) {
	t.Helper()
	for _, sample := range samples {
		if err := s.detector.Monitor.RecordVolume(sample); err != nil {
			t.Fatal(err)
		}
	}
}

// later moves the clock of the detector forward.
func (s storageFixture) later(d time.Duration) {
	s.detector.Clock = fixedClock{s.detector.Clock.Now().Add(d)}
}

func (s storageFixture) check(t *testing.T) []Finding {
	t.Helper()
	if err := s.detector.CheckStorage(context.Background(), "local"); err != nil {
		t.Fatal(err)
	}
	findings, err := s.detector.Findings()
	if err != nil {
		t.Fatal(err)
	}
	return findings
}

func (s storageFixture) workItem(t *testing.T, id, state, findingKey string) {
	t.Helper()
	_, err := s.detector.DB.Exec(`INSERT INTO work_items (id, title, kind, env, state,
		created_at, updated_at) VALUES ($1, 'Grow', $2, 'local', $3, 'then', 'then')
		ON CONFLICT (id) DO UPDATE SET state = excluded.state`, id, StorageKind, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.detector.SetWorkItem(findingKey, id, "drafted"); err != nil {
		t.Fatal(err)
	}
}

// fillsIn13Days is 8 daily samples of a 2 GiB volume: 1228 MiB and 40 MiB more each day.
func fillsIn13Days() []monitor.VolumeSample {
	used := []int64{}
	for i := range int64(8) {
		used = append(used, (1228+40*i)*mib)
	}
	return daily(2*gib, used...)
}

func TestCheckStorageRaisesOneFindingWithTheNumbers(t *testing.T) {
	fixture := newStorageFixture(t, fillsIn13Days())
	findings := fixture.check(t)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	raised := <-fixture.raised
	if raised.Key != "storage:local:postgres-data:2Gi:2026-11-08" {
		t.Errorf("key = %q", raised.Key)
	}
	// 1508 MiB + 90 x 40 MiB = 5108 MiB, which needs 5 GiB.
	want := map[string]any{"kind": "storage-growth", "layer": "30-data",
		"variable": "postgres_storage_gb", "value": "5", "running": "2",
		"used_bytes": 1508 * mib, "total_bytes": 2 * gib, "full_at": "2026-11-21T12:00:00.000Z"}
	for name, value := range want {
		if raised.Detail[name] != value {
			t.Errorf("detail %s = %v, want %v", name, raised.Detail[name], value)
		}
	}
	wantTitle := "Database volume of local is full on 2026-11-21: 1.47 GiB of 2.00 GiB used, " +
		"growth 40 MiB each day"
	if raised.Title != wantTitle {
		t.Errorf("title = %q, want %q", raised.Title, wantTitle)
	}

	if again := fixture.check(t); len(again) != 1 {
		t.Errorf("a second check gives %d findings, want 1", len(again))
	}
}

func TestCheckStorageWaitsForTheOpenWorkItem(t *testing.T) {
	fixture := newStorageFixture(t, fillsIn13Days())
	first := fixture.check(t)
	if len(first) != 1 {
		t.Fatalf("findings = %d, want 1", len(first))
	}
	// The volume is 3 GiB now and still fills inside the horizon: a new key.
	grown := fillsIn13Days()
	for i := range grown {
		grown[i].TotalBytes = 3 * gib
		grown[i].UsedBytes += 1024 * mib
		grown[i].TS = store.FormatTime(store.ParseTime(grown[i].TS).Add(time.Minute))
	}
	fixture.detector.Clock = fixedClock{store.ParseTime(grown[len(grown)-1].TS)}
	if _, err := fixture.detector.DB.Exec(`DELETE FROM volume_samples`); err != nil {
		t.Fatal(err)
	}
	fixture.record(t, grown)

	if findings := fixture.check(t); len(findings) != 1 {
		t.Errorf("with no work item yet: %d findings, want 1", len(findings))
	}
	fixture.workItem(t, "wi-1", "AwaitingApproval", first[0].Key)
	if findings := fixture.check(t); len(findings) != 1 {
		t.Errorf("with an open work item: %d findings, want 1", len(findings))
	}
	fixture.workItem(t, "wi-1", "Done", first[0].Key)
	fixture.later(StorageQuietPeriod + time.Hour)
	findings := fixture.check(t)
	if len(findings) != 2 {
		t.Fatalf("after the work item is done: %d findings, want 2", len(findings))
	}
	if findings[0].Key != "storage:local:postgres-data:3Gi:2026-11-15" {
		t.Errorf("key of the new finding = %q", findings[0].Key)
	}
}

func TestCheckStorageIsQuietForAWeekAfterARejection(t *testing.T) {
	fixture := newStorageFixture(t, fillsIn13Days())
	first := fixture.check(t)
	if len(first) != 1 {
		t.Fatalf("findings = %d, want 1", len(first))
	}
	fixture.workItem(t, "wi-1", "Rejected", first[0].Key)
	if findings := fixture.check(t); len(findings) != 1 {
		t.Errorf("at once after the rejection: %d findings, want 1", len(findings))
	}
	fixture.later(StorageQuietPeriod - time.Hour)
	if findings := fixture.check(t); len(findings) != 1 {
		t.Errorf("one hour before the end of the quiet period: %d findings, want 1",
			len(findings))
	}
	fixture.later(2 * time.Hour)
	findings := fixture.check(t)
	if len(findings) != 2 {
		t.Fatalf("after the quiet period: %d findings, want 2", len(findings))
	}
	if findings[0].Key != "storage:local:postgres-data:2Gi:2026-11-15" {
		t.Errorf("key of the new finding = %q", findings[0].Key)
	}
}

func TestCheckStorageIsQuietOutsideTheWarningHorizon(t *testing.T) {
	quiet := map[string][]monitor.VolumeSample{
		// 1 MiB each day fills the last 1500 MiB in about four years.
		"slow growth": daily(2*gib, 500*mib, 501*mib, 502*mib, 503*mib, 504*mib, 505*mib),
		"too few":     daily(2*gib, 1700*mib, 1800*mib, 1900*mib),
	}
	for label, samples := range quiet {
		fixture := newStorageFixture(t, samples)
		if findings := fixture.check(t); len(findings) != 0 {
			t.Errorf("%s: %d findings, want 0", label, len(findings))
		}
	}
}

func TestCheckStorageReadsOnlyTheLookBackPeriod(t *testing.T) {
	// Fast growth that ended 20 days before the newest samples must not count.
	old := daily(2*gib, 100*mib, 400*mib, 700*mib, 1000*mib, 1300*mib, 1600*mib)
	flat := daily(2*gib, 1600*mib, 1600*mib, 1600*mib, 1600*mib, 1600*mib, 1600*mib)
	for i := range flat {
		flat[i].TS = store.FormatTime(store.ParseTime(flat[i].TS).Add(25 * 24 * time.Hour))
	}
	fixture := newStorageFixture(t, append(old, flat...))
	if findings := fixture.check(t); len(findings) != 0 {
		t.Errorf("%d findings, want 0: only flat samples are in the look-back period",
			len(findings))
	}
}
