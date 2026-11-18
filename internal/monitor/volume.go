package monitor

import (
	"fmt"
)

// DatabaseVolume names the data volume of the database in the volume samples.
const DatabaseVolume = "postgres-data"

// VolumeSample is the fill level of one volume at one time.
type VolumeSample struct {
	TS         string `json:"ts"`
	Env        string `json:"env"`
	Volume     string `json:"volume"`
	UsedBytes  int64  `json:"used_bytes"`
	TotalBytes int64  `json:"total_bytes"`
}

// RecordVolume stores a volume sample beside the health history.
func (m *Monitor) RecordVolume(s VolumeSample) error {
	if s.TotalBytes <= 0 || s.UsedBytes < 0 {
		return fmt.Errorf("record volume sample for %s/%s: used %d and total %d are not a "+
			"fill level", s.Env, s.Volume, s.UsedBytes, s.TotalBytes)
	}
	_, err := m.DB.Exec(`INSERT INTO volume_samples (ts, env, volume, used_bytes, total_bytes)
		VALUES ($1,$2,$3,$4,$5)`, s.TS, s.Env, s.Volume, s.UsedBytes, s.TotalBytes)
	if err != nil {
		return fmt.Errorf("record volume sample for %s/%s: %w", s.Env, s.Volume, err)
	}
	return nil
}

// VolumeHistory returns the samples of one volume of env at or after from, oldest first.
func (m *Monitor) VolumeHistory(env, volume, from string) ([]VolumeSample, error) {
	rows, err := m.DB.Query(`SELECT ts, env, volume, used_bytes, total_bytes FROM volume_samples
		WHERE env = $1 AND volume = $2 AND ts >= $3 ORDER BY ts ASC`, env, volume, from)
	if err != nil {
		return nil, fmt.Errorf("read volume history of %s/%s: %w", env, volume, err)
	}
	defer rows.Close()
	samples := []VolumeSample{}
	for rows.Next() {
		var s VolumeSample
		if err := rows.Scan(&s.TS, &s.Env, &s.Volume, &s.UsedBytes, &s.TotalBytes); err != nil {
			return nil, fmt.Errorf("scan volume sample: %w", err)
		}
		samples = append(samples, s)
	}
	return samples, rows.Err()
}
