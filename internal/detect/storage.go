package detect

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/store"
)

const (
	// StorageLookBack is the period of samples that the forecast reads.
	StorageLookBack = 14 * 24 * time.Hour
	// StorageMinSamples is the least number of samples that gives a forecast.
	StorageMinSamples = 6
	// StorageWarningHorizon is how far ahead a full volume raises a finding.
	StorageWarningHorizon = 30 * 24 * time.Hour
	// StorageQuietPeriod is the time after a storage finding in which the check raises no
	// other one. It stops a new finding at once after a person rejects the change.
	StorageQuietPeriod = 7 * 24 * time.Hour
	// StorageRunwayDays is the number of days of growth that the proposed size must hold.
	StorageRunwayDays = 90

	// StorageKind is the kind of the finding and of its work item.
	StorageKind = "storage-growth"
	// StorageVariable is the tfvars setting of layer 30-data with the size in GiB.
	StorageVariable = "postgres_storage_gb"

	bytesPerGiB = 1 << 30
	bytesPerMiB = 1 << 20
)

// VolumeForecast is the result of a line through the samples of a volume.
type VolumeForecast struct {
	// UsedBytes and TotalBytes are the values of the newest sample.
	UsedBytes  int64
	TotalBytes int64
	// GrowthPerDay is the slope of the line in bytes for each day.
	GrowthPerDay float64
	// FullAt is the time at which the line reaches TotalBytes. For a volume that is full
	// already it is the time of the newest sample.
	FullAt time.Time
}

// ForecastVolume fits a least-squares line through the used bytes of the samples, oldest
// first, and gives the time at which the volume is full. There is no forecast with fewer
// than StorageMinSamples samples, with samples that all have one time, or when the use
// does not grow.
func ForecastVolume(samples []monitor.VolumeSample) (VolumeForecast, bool) {
	n := float64(len(samples))
	if len(samples) < StorageMinSamples {
		return VolumeForecast{}, false
	}
	first := store.ParseTime(samples[0].TS)
	var sumX, sumY, sumXX, sumXY float64
	for _, s := range samples {
		x := store.ParseTime(s.TS).Sub(first).Hours() / 24
		y := float64(s.UsedBytes)
		sumX, sumY, sumXX, sumXY = sumX+x, sumY+y, sumXX+x*x, sumXY+x*y
	}
	spread := n*sumXX - sumX*sumX
	if spread <= 0 {
		return VolumeForecast{}, false
	}
	slope := (n*sumXY - sumX*sumY) / spread
	if slope <= 0 {
		return VolumeForecast{}, false
	}
	intercept := (sumY - slope*sumX) / n
	last := samples[len(samples)-1]
	lastAt := store.ParseTime(last.TS)
	daysToFull := (float64(last.TotalBytes) - intercept) / slope
	fullAt := first.Add(time.Duration(daysToFull * 24 * float64(time.Hour)))
	if last.UsedBytes >= last.TotalBytes || fullAt.Before(lastAt) {
		fullAt = lastAt
	}
	return VolumeForecast{UsedBytes: last.UsedBytes, TotalBytes: last.TotalBytes,
		GrowthPerDay: slope, FullAt: fullAt}, true
}

// SizeGB returns the size of the volume in whole GiB, rounded up.
func (f VolumeForecast) SizeGB() int {
	return int(math.Ceil(float64(f.TotalBytes) / bytesPerGiB))
}

// GrownSizeGB returns the size to propose: the smallest whole number of GiB that holds the
// used bytes and StorageRunwayDays days of growth. It is always at least 1 GiB more than
// the size now, because a proposal that changes nothing does not help.
func (f VolumeForecast) GrownSizeGB() int {
	needed := float64(f.UsedBytes) + f.GrowthPerDay*StorageRunwayDays
	return max(int(math.Ceil(needed/bytesPerGiB)), f.SizeGB()+1)
}

// Numbers gives the forecast as text for a finding and for the dashboard.
func (f VolumeForecast) Numbers() string {
	return fmt.Sprintf("%.2f GiB of %.2f GiB used, growth %.0f MiB each day",
		float64(f.UsedBytes)/bytesPerGiB, float64(f.TotalBytes)/bytesPerGiB,
		f.GrowthPerDay/bytesPerMiB)
}

// StorageWarning returns the forecast for the database volume of env when the volume is
// full inside the warning horizon. The second result is false when there is no such forecast.
func (d *Detector) StorageWarning(env string) (VolumeForecast, bool, error) {
	now := d.Clock.Now()
	samples, err := d.Monitor.VolumeHistory(env, monitor.DatabaseVolume,
		store.FormatTime(now.Add(-StorageLookBack)))
	if err != nil {
		return VolumeForecast{}, false, err
	}
	forecast, ok := ForecastVolume(samples)
	if !ok || forecast.FullAt.After(now.Add(StorageWarningHorizon)) {
		return VolumeForecast{}, false, nil
	}
	return forecast, true, nil
}

// StorageTitle is the title of the finding and of the forecast on the timeline.
func StorageTitle(env string, f VolumeForecast) string {
	return fmt.Sprintf("Database volume of %s is full on %s: %s", env,
		f.FullAt.UTC().Format("2006-01-02"), f.Numbers())
}

// storageFindingWaits reports whether a storage finding of env has no work item yet or has
// a work item that is not in a final state.
func (d *Detector) storageFindingWaits(env string) (bool, error) {
	var waiting int
	err := d.DB.QueryRow(`SELECT count(*) FROM findings f
		LEFT JOIN work_items w ON w.id = f.work_item
		WHERE f.detector = 'storage' AND f.env = $1 AND (f.work_item = ''
			OR w.state NOT IN ('Done', 'Failed', 'RolledBack', 'Rejected'))`, env).Scan(&waiting)
	if err != nil {
		return false, fmt.Errorf("read the storage findings of %s: %w", env, err)
	}
	return waiting > 0, nil
}

// storageIsQuiet reports whether the newest storage finding of env is younger than the
// quiet period.
func (d *Detector) storageIsQuiet(env string, now time.Time) (bool, error) {
	var newest string
	err := d.DB.QueryRow(`SELECT COALESCE(max(at), '') FROM findings
		WHERE detector = 'storage' AND env = $1`, env).Scan(&newest)
	if err != nil {
		return false, fmt.Errorf("read the newest storage finding of %s: %w", env, err)
	}
	return newest != "" && now.Sub(store.ParseTime(newest)) < StorageQuietPeriod, nil
}

// CheckStorage opens one finding when the forecast says that the database volume is full
// inside the warning horizon. Code computes the size to propose; no model does. A finding
// whose work item is not finished stops a second one. After the work item ends, also by a
// rejection, the check stays quiet until the quiet period after the finding is over.
func (d *Detector) CheckStorage(ctx context.Context, env string) error {
	forecast, ok, err := d.StorageWarning(env)
	if err != nil || !ok {
		return err
	}
	waits, err := d.storageFindingWaits(env)
	if err != nil || waits {
		return err
	}
	now := d.Clock.Now()
	quiet, err := d.storageIsQuiet(env, now)
	if err != nil || quiet {
		return err
	}
	return d.open(ctx, Finding{Detector: "storage", Env: env,
		// The day is in the key so that a later finding for the same size is a new one.
		Key: fmt.Sprintf("storage:%s:%s:%dGi:%s", env, monitor.DatabaseVolume,
			forecast.SizeGB(), now.UTC().Format("2006-01-02")),
		Title: StorageTitle(env, forecast),
		Detail: map[string]any{"kind": StorageKind, "layer": "30-data",
			"variable": StorageVariable, "value": strconv.Itoa(forecast.GrownSizeGB()),
			"running": strconv.Itoa(forecast.SizeGB()), "used_bytes": forecast.UsedBytes,
			"total_bytes":          forecast.TotalBytes,
			"growth_bytes_per_day": int64(forecast.GrowthPerDay),
			"full_at":              store.FormatTime(forecast.FullAt),
			"runway_days":          StorageRunwayDays}})
}
