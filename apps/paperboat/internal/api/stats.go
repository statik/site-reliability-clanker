package api

import (
	"math"
	"net/http"
	"slices"
	"sync"
	"time"
)

// durationWindow is the number of recent requests that p95 is computed over.
const durationWindow = 1024

// Stats counts requests since process start.
type Stats struct {
	mu        sync.Mutex
	requests  int64
	errors    int64
	durations []float64
	next      int
}

// Record adds one request. Set timed to false for long-lived streams, which count as a
// request but would distort the latency figure.
func (s *Stats) Record(status int, duration time.Duration, timed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	if status >= http.StatusInternalServerError {
		s.errors++
	}
	if !timed {
		return
	}
	ms := float64(duration.Microseconds()) / 1000
	if len(s.durations) < durationWindow {
		s.durations = append(s.durations, ms)
		return
	}
	s.durations[s.next] = ms
	s.next = (s.next + 1) % durationWindow
}

// Snapshot returns the request count, the error count and the p95 latency in milliseconds.
func (s *Stats) Snapshot() (requests, errors int64, p95 float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests, s.errors, Percentile(s.durations, 95)
}

// Percentile returns the nearest-rank percentile of values, or 0 for no values.
func Percentile(values []float64, percent float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	rank := int(math.Ceil(percent / 100 * float64(len(sorted))))
	rank = min(len(sorted), max(1, rank))
	return math.Round(sorted[rank-1]*100) / 100
}
