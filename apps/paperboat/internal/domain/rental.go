package domain

import "fmt"

// Rental status values.
const (
	RentalReserved  = "reserved"
	RentalActive    = "active"
	RentalReturned  = "returned"
	RentalCancelled = "cancelled"
)

// MaxRentalDays is the longest rental a customer can book.
const MaxRentalDays = 90

// RentalPrice returns the price in cents for a rental of the given length.
func RentalPrice(dailyPriceCents, days int) (int, error) {
	if days < 1 || days > MaxRentalDays {
		return 0, fmt.Errorf("%w: days must be from 1 to %d, got %d", ErrInvalid, MaxRentalDays, days)
	}
	if dailyPriceCents < 0 {
		return 0, fmt.Errorf("%w: daily price %d is negative", ErrInvalid, dailyPriceCents)
	}
	return dailyPriceCents * days, nil
}

// CanReserve reports whether a fish in the given status can start a new rental.
func CanReserve(fishStatus string) error {
	if fishStatus != FishAvailable {
		return fmt.Errorf("%w: fish is %s, not available", ErrConflict, fishStatus)
	}
	return nil
}

// CanReturn reports whether a customer can ask for collection of a rental.
func CanReturn(rentalStatus string, collectionPending bool) error {
	if rentalStatus != RentalActive {
		return fmt.Errorf("%w: rental is %s, not active", ErrConflict, rentalStatus)
	}
	if collectionPending {
		return fmt.Errorf("%w: a collection is already on its way", ErrConflict)
	}
	return nil
}
