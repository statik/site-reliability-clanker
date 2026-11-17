package domain

import "time"

// Fish status values.
const (
	FishAvailable   = "available"
	FishReserved    = "reserved"
	FishRented      = "rented"
	FishInTransit   = "in_transit"
	FishQuarantined = "quarantined"
	FishRetired     = "retired"
)

// QuarantinePeriod is how long a fish stays in quarantine before it is rentable again.
// The value is short so a demonstration shows the full cycle.
const QuarantinePeriod = 2 * time.Minute

// LevelAging is the feature level that adds condition scores and retirement (release 1.1.0).
const LevelAging = 2

// AgeDays returns the whole days between the hatch date and now, never negative.
func AgeDays(hatch, now time.Time) int {
	if now.Before(hatch) {
		return 0
	}
	return int(now.Sub(hatch).Hours() / 24)
}

// Condition returns a score from 100 (newly hatched) to 0. A fish at the retirement age of
// its species scores 30; the score reaches 0 at about 1.43 times that age.
func Condition(ageDays, retireAfterDays int) int {
	if retireAfterDays <= 0 || ageDays <= 0 {
		return 100
	}
	score := 100 - (70*ageDays)/retireAfterDays
	return min(100, max(0, score))
}

// ShouldRetire reports whether the aging job retires a fish. Only fish that are in a tank
// retire; a fish with a customer or on a van retires after it comes back.
func ShouldRetire(status string, ageDays, retireAfterDays int) bool {
	if retireAfterDays <= 0 || ageDays <= retireAfterDays {
		return false
	}
	return status == FishAvailable || status == FishQuarantined
}

// AgingInput is the state of one fish that the aging job reads.
type AgingInput struct {
	Status          string
	Condition       int
	AgeDays         int
	RetireAfterDays int
	StatusChangedAt time.Time
}

// AgingResult is the state the aging job writes when Changed is true.
type AgingResult struct {
	Status    string
	Condition int
	Changed   bool
}

// AgeFish applies one aging pass to one fish. Level 1 only releases fish from quarantine.
// From LevelAging it also updates the condition score and retires fish past the age limit.
func AgeFish(level int, in AgingInput, now time.Time) AgingResult {
	out := AgingResult{Status: in.Status, Condition: in.Condition}
	if in.Status == FishRetired {
		return out
	}
	if level >= LevelAging {
		out.Condition = Condition(in.AgeDays, in.RetireAfterDays)
		if ShouldRetire(in.Status, in.AgeDays, in.RetireAfterDays) {
			out.Status = FishRetired
		}
	}
	quarantineOver := now.Sub(in.StatusChangedAt) >= QuarantinePeriod
	if out.Status == FishQuarantined && quarantineOver {
		out.Status = FishAvailable
	}
	out.Changed = out.Status != in.Status || out.Condition != in.Condition
	return out
}
