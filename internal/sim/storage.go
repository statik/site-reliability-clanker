package sim

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/statik/site-reliability-clanker/internal/detect"
	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/store"
)

const (
	// storageSceneDays is the number of daily samples that the scene feeds.
	storageSceneDays = 8
	// The scene starts at 61 percent of the volume and adds 2 percent each day, so the
	// volume is full 12.5 days after the last sample: inside the warning horizon. The odd
	// start keeps the proposed size away from a whole GiB, where rounding could go two ways
	// when the detector loop reads the samples before the scene ends.
	storageSceneStartPercent = 61
	storageSceneDailyPercent = 2
)

var declaredStorage = regexp.MustCompile(`(?m)^` + detect.StorageVariable + `\s*=\s*(\d+)\s*$`)

// declaredStorageGB reads the size of the database volume from the code on main, so that
// the samples of the scene agree with the size that a change would edit.
func (s *Simulator) declaredStorageGB() (int64, error) {
	path := fmt.Sprintf("platform/stacks/%s/30-data/terraform.tfvars", s.Orch.Envs[s.Env].Cloud)
	tfvars, err := s.Forge.ReadFile(orchestrator.PlatformRepo, "main", path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	match := declaredStorage.FindStringSubmatch(tfvars)
	if match == nil {
		return 0, fmt.Errorf("%s does not set %s to a whole number", path,
			detect.StorageVariable)
	}
	size, err := strconv.ParseInt(match[1], 10, 32)
	if err != nil || size < 1 {
		return 0, fmt.Errorf("%s sets %s to %s, which is not a size", path,
			detect.StorageVariable, match[1])
	}
	return size, nil
}

// storageGrowth feeds one sample of the database volume for each of eight days. The scene
// writes the samples itself: no probe measures the volume of the local target.
func (s *Simulator) storageGrowth(ctx context.Context) error {
	sizeGB, err := s.declaredStorageGB()
	if err != nil {
		return err
	}
	total := sizeGB << 30
	for day := range int64(storageSceneDays) {
		if err := s.Advance(ctx, 24*time.Hour); err != nil {
			return err
		}
		used := total * (storageSceneStartPercent + storageSceneDailyPercent*day) / 100
		if err := s.Monitor.RecordVolume(monitor.VolumeSample{
			TS: store.FormatTime(s.Clock.Now()), Env: s.Env, Volume: monitor.DatabaseVolume,
			UsedBytes: used, TotalBytes: total}); err != nil {
			return err
		}
	}
	s.emit("sim.volume_samples", fmt.Sprintf("Simulated samples: the database volume of "+
		"%d GiB grows by %d percent of its size each day for %d days", sizeGB,
		storageSceneDailyPercent, storageSceneDays),
		map[string]any{"volume": monitor.DatabaseVolume, "total_bytes": total,
			"samples": storageSceneDays})
	return s.Detector.CheckStorage(ctx, s.Env)
}
