// Package domain holds the Paperboat business rules as pure functions.
package domain

import "errors"

// Error kinds. Callers wrap them with detail and the API maps them to HTTP status codes.
var (
	ErrInvalid  = errors.New("invalid request")
	ErrConflict = errors.New("conflict")
	ErrNotFound = errors.New("not found")
)
