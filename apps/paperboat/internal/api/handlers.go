package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/files"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/httplog"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/store"
)

const healthTimeout = 2 * time.Second

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		httplog.SetError(r, err)
		writeJSON(w, http.StatusServiceUnavailable, errorBody{"database does not answer"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	schema, err := s.db.SchemaVersion(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, versionBody{
		Version: s.cfg.Build.Version, SchemaVersion: schema, BaseImage: s.cfg.Build.BaseImage,
	})
}

type versionBody struct {
	Version       string `json:"version"`
	SchemaVersion int    `json:"schema_version"`
	BaseImage     string `json:"base_image"`
}

type statsBody struct {
	Requests      int64   `json:"requests"`
	Errors        int64   `json:"errors"`
	P95Ms         float64 `json:"p95_ms"`
	FishAvailable int     `json:"fish_available"`
	RentalsActive int     `json:"rentals_active"`
	DeliveriesOut int     `json:"deliveries_out"`
	TicketsOpen   int     `json:"tickets_open"`
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	counts, err := s.db.Counts(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	requests, failed, p95 := s.stats.Snapshot()
	writeJSON(w, http.StatusOK, statsBody{
		Requests: requests, Errors: failed, P95Ms: p95,
		FishAvailable: counts.FishAvailable, RentalsActive: counts.RentalsActive,
		DeliveriesOut: counts.DeliveriesOut, TicketsOpen: counts.TicketsOpen,
	})
}

// list writes the result of a list query as a JSON array, never null.
func list[T any](s *Server, w http.ResponseWriter, r *http.Request, items []T, err error) {
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if items == nil {
		items = []T{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleInventory(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Build.InventoryFault && s.random() < 0.5 {
		s.writeError(w, r, errors.New("inventory: build tank snapshot: index out of range"))
		return
	}
	fish, err := s.db.Inventory(r.Context(), s.now())
	list(s, w, r, fish, err)
}

func (s *Server) handleTanks(w http.ResponseWriter, r *http.Request) {
	tanks, err := s.db.Tanks(r.Context())
	list(s, w, r, tanks, err)
}

func (s *Server) handleCustomers(w http.ResponseWriter, r *http.Request) {
	customers, err := s.db.Customers(r.Context())
	list(s, w, r, customers, err)
}

func (s *Server) handleRentals(w http.ResponseWriter, r *http.Request) {
	rentals, err := s.db.Rentals(r.Context())
	list(s, w, r, rentals, err)
}

func (s *Server) handleCreateRental(w http.ResponseWriter, r *http.Request) {
	var in store.NewRental
	if err := decodeBody(r, &in); err != nil {
		s.writeError(w, r, err)
		return
	}
	if in.CustomerID < 1 || in.FishID < 1 {
		s.writeError(w, r, fmt.Errorf("%w: customer_id and fish_id are required", domain.ErrInvalid))
		return
	}
	rental, err := s.db.CreateRental(r.Context(), in, s.now())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, rental)
}

func (s *Server) handleReturnRental(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	rental, err := s.db.ReturnRental(r.Context(), id, s.now())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rental)
}

func (s *Server) handleDeliveries(w http.ResponseWriter, r *http.Request) {
	deliveries, err := s.db.Deliveries(r.Context())
	list(s, w, r, deliveries, err)
}

func (s *Server) handleDeliveryStatus(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := decodeBody(r, &body); err != nil {
		s.writeError(w, r, err)
		return
	}
	now := s.now()
	delivery, err := s.db.TransitionDelivery(r.Context(), id, body.Status, now)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if delivery.Status == domain.DeliveryOut {
		err := files.IssueManifest(r.Context(), s.cfg.DataDir, s.db, delivery, now)
		if err != nil {
			s.writeError(w, r, fmt.Errorf("delivery %d is out but has no manifest: %w", id, err))
			return
		}
		delivery.ManifestPath = files.ManifestDir + "/" + domain.ManifestName(id)
	}
	writeJSON(w, http.StatusOK, delivery)
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	delivery, err := s.db.Delivery(r.Context(), id)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	content, err := files.ReadManifest(s.cfg.DataDir, delivery.ManifestPath)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(content)
}

func (s *Server) handleTickets(w http.ResponseWriter, r *http.Request) {
	tickets, err := s.db.Tickets(r.Context())
	list(s, w, r, tickets, err)
}

// validateTicket checks a new ticket and fills the defaults.
func validateTicket(in *store.NewTicket) error {
	in.Subject = strings.TrimSpace(in.Subject)
	in.Body = strings.TrimSpace(in.Body)
	if in.CustomerID < 1 || in.Subject == "" {
		return fmt.Errorf("%w: customer_id and subject are required", domain.ErrInvalid)
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if in.Channel == "" {
		in.Channel = "email"
	}
	if err := domain.ValidatePriority(in.Priority); err != nil {
		return err
	}
	return domain.ValidateChannel(in.Channel)
}

func (s *Server) handleCreateTicket(w http.ResponseWriter, r *http.Request) {
	var in store.NewTicket
	if err := decodeBody(r, &in); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := validateTicket(&in); err != nil {
		s.writeError(w, r, err)
		return
	}
	ticket, err := s.db.CreateTicket(r.Context(), in)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, ticket)
}

func (s *Server) handleInteractions(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	interactions, err := s.db.Interactions(r.Context(), id)
	list(s, w, r, interactions, err)
}

// validateInteraction checks a new interaction.
func validateInteraction(in *store.NewInteraction) error {
	in.Author = strings.TrimSpace(in.Author)
	in.Body = strings.TrimSpace(in.Body)
	if in.Author == "" || in.Body == "" {
		return fmt.Errorf("%w: author and body are required", domain.ErrInvalid)
	}
	if err := domain.ValidateChannel(in.Channel); err != nil {
		return err
	}
	if in.Status == "" {
		return nil
	}
	return domain.ValidateTicketStatus(in.Status)
}

func (s *Server) handleAddInteraction(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var in store.NewInteraction
	if err := decodeBody(r, &in); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := validateInteraction(&in); err != nil {
		s.writeError(w, r, err)
		return
	}
	interaction, err := s.db.AddInteraction(r.Context(), id, in)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, interaction)
}
