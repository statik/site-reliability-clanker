package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
)

var now = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func TestAgeDays(t *testing.T) {
	cases := []struct {
		name  string
		hatch time.Time
		want  int
	}{
		{"hatched today", now, 0},
		{"almost one day", now.Add(-23 * time.Hour), 0},
		{"one day", now.Add(-24 * time.Hour), 1},
		{"one year", now.AddDate(-1, 0, 0), 365},
		{"hatch date in the future", now.AddDate(0, 0, 3), 0},
	}
	for _, c := range cases {
		if got := domain.AgeDays(c.hatch, now); got != c.want {
			t.Errorf("%s: AgeDays = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestCondition(t *testing.T) {
	cases := []struct {
		age, limit, want int
	}{
		{0, 400, 100},
		{200, 400, 65},
		{400, 400, 30},
		{500, 400, 13},
		{600, 400, 0},
		{5000, 400, 0},
		{-5, 400, 100},
		{100, 0, 100},
	}
	for _, c := range cases {
		if got := domain.Condition(c.age, c.limit); got != c.want {
			t.Errorf("Condition(%d, %d) = %d, want %d", c.age, c.limit, got, c.want)
		}
	}
}

func TestConditionNeverRisesWithAge(t *testing.T) {
	previous := 100
	for age := 0; age <= 3000; age++ {
		got := domain.Condition(age, 900)
		if got > previous || got < 0 || got > 100 {
			t.Fatalf("Condition(%d, 900) = %d after %d", age, got, previous)
		}
		previous = got
	}
}

func TestShouldRetire(t *testing.T) {
	cases := []struct {
		status     string
		age, limit int
		want       bool
	}{
		{domain.FishAvailable, 401, 400, true},
		{domain.FishQuarantined, 401, 400, true},
		{domain.FishAvailable, 400, 400, false},
		{domain.FishRented, 900, 400, false},
		{domain.FishInTransit, 900, 400, false},
		{domain.FishReserved, 900, 400, false},
		{domain.FishAvailable, 900, 0, false},
	}
	for _, c := range cases {
		if got := domain.ShouldRetire(c.status, c.age, c.limit); got != c.want {
			t.Errorf("ShouldRetire(%s, %d, %d) = %v, want %v", c.status, c.age, c.limit, got, c.want)
		}
	}
}

func TestAgeFish(t *testing.T) {
	longAgo := now.Add(-time.Hour)
	recent := now.Add(-time.Second)
	cases := []struct {
		name  string
		level int
		in    domain.AgingInput
		want  domain.AgingResult
	}{
		{
			"1.0.0 does not score or retire an old fish", 1,
			domain.AgingInput{Status: domain.FishAvailable, AgeDays: 900, RetireAfterDays: 400,
				StatusChangedAt: longAgo},
			domain.AgingResult{Status: domain.FishAvailable},
		},
		{
			"1.0.0 releases a fish after quarantine", 1,
			domain.AgingInput{Status: domain.FishQuarantined, StatusChangedAt: longAgo},
			domain.AgingResult{Status: domain.FishAvailable, Changed: true},
		},
		{
			"quarantine is not over yet", 1,
			domain.AgingInput{Status: domain.FishQuarantined, StatusChangedAt: recent},
			domain.AgingResult{Status: domain.FishQuarantined},
		},
		{
			"1.1.0 retires an old fish and scores it", 2,
			domain.AgingInput{Status: domain.FishAvailable, Condition: 100, AgeDays: 500,
				RetireAfterDays: 400, StatusChangedAt: longAgo},
			domain.AgingResult{Status: domain.FishRetired, Condition: 13, Changed: true},
		},
		{
			"1.1.0 retires an old fish in quarantine, it does not release it", 2,
			domain.AgingInput{Status: domain.FishQuarantined, Condition: 13, AgeDays: 500,
				RetireAfterDays: 400, StatusChangedAt: longAgo},
			domain.AgingResult{Status: domain.FishRetired, Condition: 13, Changed: true},
		},
		{
			"1.1.0 scores a rented old fish but leaves it with the customer", 2,
			domain.AgingInput{Status: domain.FishRented, Condition: 100, AgeDays: 500,
				RetireAfterDays: 400, StatusChangedAt: longAgo},
			domain.AgingResult{Status: domain.FishRented, Condition: 13, Changed: true},
		},
		{
			"1.1.0 changes nothing when the score is current", 2,
			domain.AgingInput{Status: domain.FishAvailable, Condition: 65, AgeDays: 200,
				RetireAfterDays: 400, StatusChangedAt: longAgo},
			domain.AgingResult{Status: domain.FishAvailable, Condition: 65},
		},
		{
			"a retired fish stays as it is", 2,
			domain.AgingInput{Status: domain.FishRetired, Condition: 40, AgeDays: 900,
				RetireAfterDays: 400, StatusChangedAt: longAgo},
			domain.AgingResult{Status: domain.FishRetired, Condition: 40},
		},
	}
	for _, c := range cases {
		if got := domain.AgeFish(c.level, c.in, now); got != c.want {
			t.Errorf("%s: AgeFish = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestRentalPrice(t *testing.T) {
	if got, err := domain.RentalPrice(450, 3); err != nil || got != 1350 {
		t.Errorf("RentalPrice(450, 3) = %d, %v; want 1350", got, err)
	}
	for _, days := range []int{0, -1, domain.MaxRentalDays + 1} {
		if _, err := domain.RentalPrice(450, days); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("RentalPrice(450, %d) error = %v, want ErrInvalid", days, err)
		}
	}
	if _, err := domain.RentalPrice(-1, 3); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("negative price error = %v, want ErrInvalid", err)
	}
}

func TestCanReserve(t *testing.T) {
	if err := domain.CanReserve(domain.FishAvailable); err != nil {
		t.Errorf("available fish: %v", err)
	}
	blocked := []string{domain.FishReserved, domain.FishRented, domain.FishInTransit,
		domain.FishQuarantined, domain.FishRetired}
	for _, status := range blocked {
		if err := domain.CanReserve(status); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("CanReserve(%s) = %v, want ErrConflict", status, err)
		}
	}
}

func TestCanReturn(t *testing.T) {
	if err := domain.CanReturn(domain.RentalActive, false); err != nil {
		t.Errorf("active rental: %v", err)
	}
	if err := domain.CanReturn(domain.RentalActive, true); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("second collection = %v, want ErrConflict", err)
	}
	for _, status := range []string{domain.RentalReserved, domain.RentalReturned,
		domain.RentalCancelled} {
		if err := domain.CanReturn(status, false); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("CanReturn(%s) = %v, want ErrConflict", status, err)
		}
	}
}

func TestValidateDeliveryTransition(t *testing.T) {
	allowed := [][2]string{
		{domain.DeliveryScheduled, domain.DeliveryOut},
		{domain.DeliveryScheduled, domain.DeliveryFailed},
		{domain.DeliveryOut, domain.DeliveryDone},
		{domain.DeliveryOut, domain.DeliveryFailed},
	}
	for _, c := range allowed {
		if err := domain.ValidateDeliveryTransition(c[0], c[1]); err != nil {
			t.Errorf("%s to %s: %v", c[0], c[1], err)
		}
	}
	conflicts := [][2]string{
		{domain.DeliveryScheduled, domain.DeliveryDone},
		{domain.DeliveryOut, domain.DeliveryOut},
		{domain.DeliveryDone, domain.DeliveryOut},
		{domain.DeliveryFailed, domain.DeliveryDone},
		{domain.DeliveryOut, domain.DeliveryScheduled},
	}
	for _, c := range conflicts {
		err := domain.ValidateDeliveryTransition(c[0], c[1])
		if !errors.Is(err, domain.ErrConflict) {
			t.Errorf("%s to %s = %v, want ErrConflict", c[0], c[1], err)
		}
	}
	err := domain.ValidateDeliveryTransition(domain.DeliveryScheduled, "teleported")
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("unknown status = %v, want ErrInvalid", err)
	}
}

func TestNextDeliveryStatus(t *testing.T) {
	cases := []struct {
		name        string
		status      string
		windowStart time.Time
		startedAt   time.Time
		want        string
		due         bool
	}{
		{"window not open", domain.DeliveryScheduled, now.Add(time.Second), time.Time{}, "", false},
		{"window opens now", domain.DeliveryScheduled, now, time.Time{}, domain.DeliveryOut, true},
		{"van still on the road", domain.DeliveryOut, now.Add(-time.Minute),
			now.Add(-domain.TransitTime + time.Second), "", false},
		{"van arrived", domain.DeliveryOut, now.Add(-time.Minute),
			now.Add(-domain.TransitTime), domain.DeliveryDone, true},
		{"done stays done", domain.DeliveryDone, now.Add(-time.Hour), now.Add(-time.Hour), "", false},
		{"failed stays failed", domain.DeliveryFailed, now.Add(-time.Hour), time.Time{}, "", false},
	}
	for _, c := range cases {
		got, due := domain.NextDeliveryStatus(c.status, c.windowStart, c.startedAt, now)
		if got != c.want || due != c.due {
			t.Errorf("%s: got %q %v, want %q %v", c.name, got, due, c.want, c.due)
		}
	}
}

func TestEffectOf(t *testing.T) {
	cases := []struct {
		kind, to string
		want     domain.DeliveryEffect
	}{
		{domain.KindDelivery, domain.DeliveryOut, domain.DeliveryEffect{Fish: domain.FishInTransit}},
		{domain.KindDelivery, domain.DeliveryDone,
			domain.DeliveryEffect{Fish: domain.FishRented, Rental: domain.RentalActive}},
		{domain.KindDelivery, domain.DeliveryFailed,
			domain.DeliveryEffect{Fish: domain.FishQuarantined, Rental: domain.RentalCancelled}},
		{domain.KindCollection, domain.DeliveryOut,
			domain.DeliveryEffect{Fish: domain.FishInTransit}},
		{domain.KindCollection, domain.DeliveryDone,
			domain.DeliveryEffect{Fish: domain.FishAvailable, Rental: domain.RentalReturned}},
		{domain.KindCollection, domain.DeliveryFailed,
			domain.DeliveryEffect{Fish: domain.FishRented}},
	}
	for _, c := range cases {
		got, err := domain.EffectOf(c.kind, c.to)
		if err != nil || got != c.want {
			t.Errorf("EffectOf(%s, %s) = %+v, %v; want %+v", c.kind, c.to, got, err, c.want)
		}
	}
	if _, err := domain.EffectOf("airdrop", domain.DeliveryOut); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("unknown kind = %v, want ErrInvalid", err)
	}
	_, err := domain.EffectOf(domain.KindDelivery, domain.DeliveryScheduled)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("scheduled has no effect; got %v, want ErrInvalid", err)
	}
}

func TestDriverFor(t *testing.T) {
	for _, n := range []int64{0, 1, 5, 6, 7, 1000, -3} {
		driver := domain.DriverFor(n)
		if driver == "" {
			t.Errorf("DriverFor(%d) is empty", n)
		}
	}
	if domain.DriverFor(1) != domain.DriverFor(1+int64(len(domain.Drivers))) {
		t.Error("drivers do not repeat in a cycle")
	}
}

func TestManifestAndCertificate(t *testing.T) {
	manifest := domain.Manifest(domain.ManifestInfo{
		DeliveryID: 7, Kind: domain.KindCollection, RentalID: 3, Driver: "Pike",
		Customer: "Ada Brine", Address: "3 Mooring Lane", Fish: "Dory", Species: "Betta",
		Tank: "Kelp Room", WindowStart: now, WindowEnd: now.Add(domain.WindowLength), IssuedAt: now,
	})
	for _, want := range []string{"COLLECTION MANIFEST #7", "Rental:    #3", "Pike", "Ada Brine",
		"Dory (Betta)", "Kelp Room", "2026-11-16 12:00 UTC to 2026-11-16 12:30 UTC"} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest lacks %q:\n%s", want, manifest)
		}
	}
	if domain.ManifestName(7) != "delivery-7.txt" {
		t.Errorf("ManifestName(7) = %s", domain.ManifestName(7))
	}
	condition := 42
	with := domain.Certificate("Dory", "Betta", domain.FishAvailable, 90, &condition, now)
	if !strings.Contains(with, "Condition: 42 of 100") {
		t.Errorf("certificate lacks the condition:\n%s", with)
	}
	without := domain.Certificate("Dory", "Betta", domain.FishAvailable, 90, nil, now)
	if strings.Contains(without, "Condition") {
		t.Errorf("1.0.0 certificate shows a condition:\n%s", without)
	}
}

func TestEscalatedPriority(t *testing.T) {
	due := now.Add(-domain.EscalateAfter)
	cases := []struct {
		name     string
		status   string
		priority string
		since    time.Time
		want     string
		raised   bool
	}{
		{"low goes to normal", domain.TicketOpen, "low", due, "normal", true},
		{"normal goes to high", domain.TicketOpen, "normal", due, "high", true},
		{"high goes to urgent", domain.TicketOpen, "high", due, "urgent", true},
		{"urgent is the top", domain.TicketOpen, "urgent", due, "", false},
		{"not open long enough", domain.TicketOpen, "low", due.Add(time.Second), "", false},
		{"waiting tickets do not escalate", domain.TicketWaiting, "low", due, "", false},
		{"resolved tickets do not escalate", domain.TicketResolved, "low", due, "", false},
		{"unknown priority is left alone", domain.TicketOpen, "whenever", due, "", false},
	}
	for _, c := range cases {
		got, raised := domain.EscalatedPriority(c.status, c.priority, c.since, now)
		if got != c.want || raised != c.raised {
			t.Errorf("%s: got %q %v, want %q %v", c.name, got, raised, c.want, c.raised)
		}
	}
}

func TestValidators(t *testing.T) {
	if err := domain.ValidatePriority("high"); err != nil {
		t.Errorf("priority high: %v", err)
	}
	if err := domain.ValidateChannel("chat"); err != nil {
		t.Errorf("channel chat: %v", err)
	}
	if err := domain.ValidateTicketStatus(domain.TicketWaiting); err != nil {
		t.Errorf("status waiting: %v", err)
	}
	bad := []error{
		domain.ValidatePriority("asap"),
		domain.ValidateChannel("fax"),
		domain.ValidateTicketStatus("closed"),
		domain.ValidatePriority(""),
	}
	for i, err := range bad {
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("bad value %d: got %v, want ErrInvalid", i, err)
		}
	}
}
