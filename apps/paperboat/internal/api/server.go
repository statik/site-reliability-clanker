// Package api is the REST API and the live inventory stream.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/buildinfo"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/httplog"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/store"
)

// Addr is the listen address of the api service.
const Addr = ":8081"

const maxBodyBytes = 1 << 20

// Store is the database boundary that the handlers use.
type Store interface {
	Ping(ctx context.Context) error
	SchemaVersion(ctx context.Context) (int, error)
	Counts(ctx context.Context) (store.Counts, error)
	Inventory(ctx context.Context, now time.Time) ([]store.Fish, error)
	Tanks(ctx context.Context) ([]store.Tank, error)
	Customers(ctx context.Context) ([]store.Customer, error)
	Rentals(ctx context.Context) ([]store.Rental, error)
	CreateRental(ctx context.Context, in store.NewRental, now time.Time) (store.Rental, error)
	ReturnRental(ctx context.Context, id int64, now time.Time) (store.Rental, error)
	Deliveries(ctx context.Context) ([]store.Delivery, error)
	Delivery(ctx context.Context, id int64) (store.Delivery, error)
	TransitionDelivery(ctx context.Context, id int64, to string, now time.Time,
	) (store.Delivery, error)
	SetManifestPath(ctx context.Context, id int64, path string) error
	Tickets(ctx context.Context) ([]store.Ticket, error)
	CreateTicket(ctx context.Context, in store.NewTicket) (store.Ticket, error)
	Interactions(ctx context.Context, ticketID int64) ([]store.Interaction, error)
	AddInteraction(ctx context.Context, ticketID int64, in store.NewInteraction,
	) (store.Interaction, error)
}

// Config is the configuration of the api service.
type Config struct {
	Build      buildinfo.Info
	AdminToken string
	DataDir    string
}

// Server holds the handlers and their in-memory state.
type Server struct {
	cfg    Config
	db     Store
	logger *slog.Logger
	stats  *Stats
	fault  *Fault
	hub    *Hub
	stop   <-chan struct{}
	now    func() time.Time
	random func() float64
	sleep  func(time.Duration)
}

// NewServer returns a server. Streams end when stop closes.
func NewServer(cfg Config, db Store, hub *Hub, logger *slog.Logger, stop <-chan struct{},
) *Server {
	return &Server{
		cfg: cfg, db: db, logger: logger, hub: hub, stop: stop,
		stats: &Stats{}, fault: &Fault{},
		now: time.Now, random: rand.Float64, sleep: time.Sleep,
	}
}

// Run serves the API until ctx ends.
func Run(ctx context.Context, cfg Config, db Store, dsn string, logger *slog.Logger) error {
	hub := NewHub()
	go ListenForFishStatus(ctx, dsn, hub, logger)
	server := NewServer(cfg, db, hub, logger, ctx.Done())
	return httplog.Serve(ctx, Addr, server.Handler(), logger)
}

// Handler returns the full handler: logging and counting, then fault injection, then routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/version", s.handleVersion)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/inventory", s.handleInventory)
	mux.HandleFunc("GET /api/inventory/stream", s.handleStream)
	mux.HandleFunc("GET /api/tanks", s.handleTanks)
	mux.HandleFunc("GET /api/customers", s.handleCustomers)
	mux.HandleFunc("GET /api/rentals", s.handleRentals)
	mux.HandleFunc("POST /api/rentals", s.handleCreateRental)
	mux.HandleFunc("POST /api/rentals/{id}/return", s.handleReturnRental)
	mux.HandleFunc("GET /api/deliveries", s.handleDeliveries)
	mux.HandleFunc("POST /api/deliveries/{id}/status", s.handleDeliveryStatus)
	mux.HandleFunc("GET /api/deliveries/{id}/manifest", s.handleManifest)
	mux.HandleFunc("GET /api/tickets", s.handleTickets)
	mux.HandleFunc("POST /api/tickets", s.handleCreateTicket)
	mux.HandleFunc("GET /api/tickets/{id}/interactions", s.handleInteractions)
	mux.HandleFunc("POST /api/tickets/{id}/interactions", s.handleAddInteraction)
	mux.HandleFunc("POST /api/admin/fault", s.handleFault)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, errorBody{"no such path: " + r.URL.Path})
	})
	observe := func(r *http.Request, status int, duration time.Duration) {
		s.stats.Record(status, duration, r.URL.Path != "/api/inventory/stream")
	}
	return httplog.Middleware(s.logger, observe, s.faultMiddleware(mux))
}

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError maps an error to a status code. The client sees the message of a domain
// error; any other error is logged in full and the client sees a short message.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	httplog.SetError(r, err)
	switch {
	case errors.Is(err, domain.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, errorBody{err.Error()})
	case errors.Is(err, domain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{err.Error()})
	case errors.Is(err, domain.ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody{"internal error"})
	}
}

// decodeBody reads a JSON request body into v.
func decodeBody(r *http.Request, v any) error {
	body := http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	if err := json.NewDecoder(body).Decode(v); err != nil {
		return fmt.Errorf("%w: request body is not valid JSON: %v", domain.ErrInvalid, err)
	}
	return nil
}

// pathID reads the {id} path value.
func pathID(r *http.Request) (int64, error) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("%w: id must be a positive number, got %q", domain.ErrInvalid, raw)
	}
	return id, nil
}
