package monitor

import (
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestVolumeHistoryReturnsTheSamplesOfOneVolumeInTimeOrder(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	m := New(storetest.Open(t), fixedClock{now})
	record := func(env, volume string, age time.Duration, used int64) {
		t.Helper()
		err := m.RecordVolume(VolumeSample{TS: store.FormatTime(now.Add(-age)), Env: env,
			Volume: volume, UsedBytes: used, TotalBytes: 1000})
		if err != nil {
			t.Fatal(err)
		}
	}
	record("local", DatabaseVolume, time.Hour, 300)
	record("local", DatabaseVolume, 3*time.Hour, 100)
	record("local", DatabaseVolume, 2*time.Hour, 200)
	record("local", DatabaseVolume, 48*time.Hour, 50)
	record("local", "shared", time.Hour, 900)
	record("other", DatabaseVolume, time.Hour, 800)

	samples, err := m.VolumeHistory("local", DatabaseVolume,
		store.FormatTime(now.Add(-24*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	used := []int64{}
	for _, s := range samples {
		used = append(used, s.UsedBytes)
	}
	if len(used) != 3 || used[0] != 100 || used[1] != 200 || used[2] != 300 {
		t.Errorf("used bytes = %v, want [100 200 300]", used)
	}
}

func TestRecordVolumeRefusesASampleThatIsNotAFillLevel(t *testing.T) {
	m := New(storetest.Open(t), fixedClock{})
	for label, s := range map[string]VolumeSample{
		"no total":      {Env: "local", Volume: DatabaseVolume, UsedBytes: 1},
		"negative used": {Env: "local", Volume: DatabaseVolume, UsedBytes: -1, TotalBytes: 10},
	} {
		if err := m.RecordVolume(s); err == nil {
			t.Errorf("%s: the sample was recorded", label)
		}
	}
}
