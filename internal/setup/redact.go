package setup

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"sync"
)

// hidden replaces a private value in the output of the installer.
const hidden = "(hidden)"

// redactor removes private values from text: the values of the required tags, which can
// hold a person's address, and the model key. OpenTofu prints the tags of each resource in
// its plan, and an error of a program can quote its input.
type redactor struct {
	values []string
}

func newRedactor(values ...string) redactor {
	kept := []string{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			kept = append(kept, value)
		}
	}
	// A longer value first, so that a value inside another one leaves no part of it.
	slices.SortFunc(kept, func(a, b string) int { return len(b) - len(a) })
	return redactor{values: slices.Compact(kept)}
}

func (r redactor) text(text string) string {
	for _, value := range r.values {
		text = strings.ReplaceAll(text, value, hidden)
	}
	return text
}

// err returns an error with the same text, redacted. It keeps no wrapped error, whose text
// could hold a value.
func (r redactor) err(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(r.text(err.Error()))
}

// lineWriter sends each complete line that is written to it to a function, redacted.
type lineWriter struct {
	mu      sync.Mutex
	pending []byte
	send    func(string)
	redact  redactor
}

func (w *lineWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, data...)
	for {
		end := bytes.IndexByte(w.pending, '\n')
		if end < 0 {
			return len(data), nil
		}
		w.send(w.redact.text(strings.TrimRight(string(w.pending[:end]), "\r")))
		w.pending = w.pending[end+1:]
	}
}

// Flush sends a last line that has no line end.
func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) > 0 {
		w.send(w.redact.text(string(w.pending)))
		w.pending = nil
	}
}
