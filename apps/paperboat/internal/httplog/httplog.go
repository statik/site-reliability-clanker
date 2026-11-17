// Package httplog writes one structured JSON log line for each HTTP request and runs
// HTTP servers with a clean shutdown.
package httplog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// Recorder wraps a ResponseWriter and remembers the status code and the body size.
type Recorder struct {
	http.ResponseWriter
	Status int
	Bytes  int
}

// WriteHeader records the status code.
func (r *Recorder) WriteHeader(status int) {
	if r.Status == 0 {
		r.Status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

// Write records the body size.
func (r *Recorder) Write(b []byte) (int, error) {
	if r.Status == 0 {
		r.Status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.Bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach Flush on the real writer. Without it the
// inventory stream stays in a buffer.
func (r *Recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Flush sends buffered data, for callers that use the http.Flusher interface.
func (r *Recorder) Flush() {
	_ = http.NewResponseController(r.ResponseWriter).Flush()
}

// NewLogger returns a JSON logger on stdout that tags each line with the service.
func NewLogger(service, version string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, nil)
	return slog.New(handler).With("service", service, "version", version)
}

// Observer receives the outcome of each request.
type Observer func(r *http.Request, status int, duration time.Duration)

type detailKey struct{}

// SetError attaches the cause of a failed request to its log line.
func SetError(r *http.Request, err error) {
	if detail, ok := r.Context().Value(detailKey{}).(*string); ok && err != nil {
		*detail = err.Error()
	}
}

// Middleware logs each request as one JSON line and reports it to observe, if set.
func Middleware(logger *slog.Logger, observe Observer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &Recorder{ResponseWriter: w}
		var detail string
		r = r.WithContext(context.WithValue(r.Context(), detailKey{}, &detail))
		next.ServeHTTP(rec, r)
		if rec.Status == 0 {
			rec.Status = http.StatusOK
		}
		duration := time.Since(start)
		if observe != nil {
			observe(r, rec.Status, duration)
		}
		level := slog.LevelInfo
		if rec.Status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.Status),
			slog.Float64("duration_ms", float64(duration.Microseconds())/1000),
			slog.Int("bytes", rec.Bytes),
			slog.String("remote", r.RemoteAddr),
		}
		if detail != "" {
			attrs = append(attrs, slog.String("error", detail))
		}
		logger.LogAttrs(r.Context(), level, "request", attrs...)
	})
}

// Serve runs an HTTP server until ctx ends, then shuts it down. Streams that are still
// open after the grace period are closed.
func Serve(ctx context.Context, addr string, handler http.Handler, logger *slog.Logger) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	failed := make(chan error, 1)
	go func() { failed <- server.ListenAndServe() }()
	logger.Info("listening", "addr", addr)
	select {
	case err := <-failed:
		return fmt.Errorf("listen on %s: %w", addr, err)
	case <-ctx.Done():
	}
	grace, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(grace); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("shut down server on %s: %w", addr, err)
	}
	_ = server.Close()
	logger.Info("stopped")
	return nil
}
