package api

import (
	"github.com/statik/site-reliability-clanker/internal/detect"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// storageForecasts returns one future entry for each environment whose database volume is
// full inside the warning horizon. The entry links to the work item of the newest storage
// finding when there is one.
func (s *Server) storageForecasts(now string) ([]map[string]any, error) {
	findings, err := s.Detector.Findings()
	if err != nil {
		return nil, err
	}
	entries := []map[string]any{}
	for _, env := range s.envNames() {
		forecast, ok, err := s.Detector.StorageWarning(env)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		// A volume that is full already stays on the list at the present time.
		entry := map[string]any{"ts": max(store.FormatTime(forecast.FullAt), now), "env": env,
			"kind": "forecast", "title": detect.StorageTitle(env, forecast),
			"source": "Volume samples"}
		for _, f := range findings {
			if f.Detector == "storage" && f.Env == env {
				if f.WorkItem != "" {
					entry["work_item"] = f.WorkItem
				}
				break
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
