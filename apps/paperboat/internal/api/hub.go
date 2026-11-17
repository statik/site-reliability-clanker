package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/store"
)

const (
	subscriberBuffer = 64
	heartbeatEvery   = 15 * time.Second
	listenRetryEvery = 2 * time.Second
)

// Hub fans fish status changes out to the open inventory streams of this process.
type Hub struct {
	mu          sync.Mutex
	subscribers map[chan string]struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{subscribers: map[chan string]struct{}{}}
}

// Subscribe returns a channel of event payloads and a function that ends the subscription.
func (h *Hub) Subscribe() (<-chan string, func()) {
	ch := make(chan string, subscriberBuffer)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subscribers, ch)
		h.mu.Unlock()
	}
}

// Publish sends a payload to each subscriber. A subscriber that does not keep up loses the
// event; the browser resynchronises from /api/inventory.
func (h *Hub) Publish(payload string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subscribers {
		select {
		case ch <- payload:
		default:
		}
	}
}

// ListenForFishStatus feeds the hub from Postgres notifications until ctx ends. The
// database trigger sends one notification for each status change, whichever process made
// the change, so each api replica sees them all.
func ListenForFishStatus(ctx context.Context, dsn string, hub *Hub, logger *slog.Logger) {
	for ctx.Err() == nil {
		err := store.Listen(ctx, dsn, store.FishStatusChannel, hub.Publish)
		if ctx.Err() != nil {
			return
		}
		logger.Error("inventory stream lost its database connection; retrying",
			"error", err.Error())
		select {
		case <-ctx.Done():
		case <-time.After(listenRetryEvery):
		}
	}
}

// handleStream serves GET /api/inventory/stream as server-sent events.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	events, cancel := s.hub.Subscribe()
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher := http.NewResponseController(w)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	_ = flusher.Flush()
	heartbeat := time.NewTicker(heartbeatEvery)
	defer heartbeat.Stop()
	for {
		var err error
		select {
		case <-r.Context().Done():
			return
		case <-s.stop:
			return
		case payload := <-events:
			_, err = fmt.Fprintf(w, "event: fish_status\ndata: %s\n\n", payload)
		case <-heartbeat.C:
			_, err = fmt.Fprint(w, ": heartbeat\n\n")
		}
		if err != nil || flusher.Flush() != nil {
			return
		}
	}
}
