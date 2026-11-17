package domain

import (
	"fmt"
	"strings"
	"time"
)

// Delivery kinds and status values.
const (
	KindDelivery   = "delivery"
	KindCollection = "collection"

	DeliveryScheduled = "scheduled"
	DeliveryOut       = "out"
	DeliveryDone      = "done"
	DeliveryFailed    = "failed"
)

// Timing of the delivery cycle. The values are short so a demonstration shows the full cycle.
const (
	// DispatchDelay is the time from booking to the start of the delivery window.
	DispatchDelay = 20 * time.Second
	// WindowLength is the length of the delivery window that the customer sees.
	WindowLength = 30 * time.Minute
	// TransitTime is how long a van is on the road.
	TransitTime = 20 * time.Second
)

// Drivers are the delivery drivers. A job gets a driver by its number.
var Drivers = []string{"Marlow", "Pike", "Coral", "Finn", "Delta", "Brook"}

// DriverFor returns the driver for the nth job.
func DriverFor(n int64) string {
	index := n % int64(len(Drivers))
	if index < 0 {
		index = -index
	}
	return Drivers[index]
}

// ValidateDeliveryTransition reports whether a delivery can move between two status values.
func ValidateDeliveryTransition(from, to string) error {
	allowed := map[string][]string{
		DeliveryScheduled: {DeliveryOut, DeliveryFailed},
		DeliveryOut:       {DeliveryDone, DeliveryFailed},
	}
	switch to {
	case DeliveryScheduled, DeliveryOut, DeliveryDone, DeliveryFailed:
	default:
		return fmt.Errorf("%w: unknown delivery status %q", ErrInvalid, to)
	}
	for _, next := range allowed[from] {
		if next == to {
			return nil
		}
	}
	return fmt.Errorf("%w: delivery cannot go from %s to %s", ErrConflict, from, to)
}

// NextDeliveryStatus returns the status the worker moves a delivery to, if it is time.
// startedAt is the time the van left; it is only read when the status is out.
func NextDeliveryStatus(status string, windowStart, startedAt, now time.Time) (string, bool) {
	switch status {
	case DeliveryScheduled:
		if !now.Before(windowStart) {
			return DeliveryOut, true
		}
	case DeliveryOut:
		if now.Sub(startedAt) >= TransitTime {
			return DeliveryDone, true
		}
	}
	return "", false
}

// DeliveryEffect is what a delivery status change does to the fish and the rental.
// An empty Rental means the rental status stays as it is.
type DeliveryEffect struct {
	Fish   string
	Rental string
}

// EffectOf returns the effect of a delivery of the given kind reaching the given status.
func EffectOf(kind, to string) (DeliveryEffect, error) {
	effects := map[string]map[string]DeliveryEffect{
		KindDelivery: {
			DeliveryOut:    {Fish: FishInTransit},
			DeliveryDone:   {Fish: FishRented, Rental: RentalActive},
			DeliveryFailed: {Fish: FishQuarantined, Rental: RentalCancelled},
		},
		KindCollection: {
			DeliveryOut:    {Fish: FishInTransit},
			DeliveryDone:   {Fish: FishAvailable, Rental: RentalReturned},
			DeliveryFailed: {Fish: FishRented},
		},
	}
	byStatus, ok := effects[kind]
	if !ok {
		return DeliveryEffect{}, fmt.Errorf("%w: unknown delivery kind %q", ErrInvalid, kind)
	}
	effect, ok := byStatus[to]
	if !ok {
		return DeliveryEffect{}, fmt.Errorf("%w: status %q has no effect", ErrInvalid, to)
	}
	return effect, nil
}

// ManifestInfo is the content of a delivery manifest.
type ManifestInfo struct {
	DeliveryID  int64
	Kind        string
	RentalID    int64
	Driver      string
	WindowStart time.Time
	WindowEnd   time.Time
	Customer    string
	Address     string
	Fish        string
	Species     string
	Tank        string
	IssuedAt    time.Time
}

// ManifestName returns the file name of a manifest inside the manifests directory.
func ManifestName(deliveryID int64) string {
	return fmt.Sprintf("delivery-%d.txt", deliveryID)
}

// Manifest renders the manifest document a driver carries.
func Manifest(m ManifestInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "PAPERBOAT FISH RENTAL - %s MANIFEST #%d\n", strings.ToUpper(m.Kind), m.DeliveryID)
	b.WriteString(strings.Repeat("=", 52) + "\n")
	fmt.Fprintf(&b, "Rental:    #%d\n", m.RentalID)
	fmt.Fprintf(&b, "Driver:    %s\n", m.Driver)
	fmt.Fprintf(&b, "Window:    %s to %s\n", stamp(m.WindowStart), stamp(m.WindowEnd))
	fmt.Fprintf(&b, "Customer:  %s\n", m.Customer)
	fmt.Fprintf(&b, "Address:   %s\n", m.Address)
	fmt.Fprintf(&b, "Fish:      %s (%s)\n", m.Fish, m.Species)
	fmt.Fprintf(&b, "Home tank: %s\n", m.Tank)
	fmt.Fprintf(&b, "Issued:    %s\n", stamp(m.IssuedAt))
	b.WriteString("\nKeep the transport bag upright and out of direct sun.\n")
	return b.String()
}

// Certificate renders the health certificate for one fish. condition is nil before 1.1.0.
func Certificate(fish, species, status string, ageDays int, condition *int, at time.Time) string {
	var b strings.Builder
	b.WriteString("PAPERBOAT FISH RENTAL - HEALTH CERTIFICATE\n")
	b.WriteString(strings.Repeat("=", 42) + "\n")
	fmt.Fprintf(&b, "Fish:      %s (%s)\n", fish, species)
	fmt.Fprintf(&b, "Status:    %s\n", status)
	fmt.Fprintf(&b, "Age:       %d days\n", ageDays)
	if condition != nil {
		fmt.Fprintf(&b, "Condition: %d of 100\n", *condition)
	}
	fmt.Fprintf(&b, "Checked:   %s\n", stamp(at))
	return b.String()
}

func stamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04 UTC")
}
