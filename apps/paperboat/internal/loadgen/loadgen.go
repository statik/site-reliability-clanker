// Package loadgen plays synthetic customers against the web service, so inventory moves
// without a person.
package loadgen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/store"
)

const (
	tickEvery = 3 * time.Second
	// minAvailable keeps some fish in the tanks, so the board never empties.
	minAvailable = 18
	// minRentalAge is how long a synthetic customer keeps a fish.
	minRentalAge = 45 * time.Second
	// answerAfter leaves tickets open long enough for the escalation job to raise them.
	answerAfter    = 5 * time.Minute
	summaryEvery   = 20
	requestTimeout = 10 * time.Second
)

var subjects = []string{
	"Fish looks pale since delivery", "Driver missed the delivery window",
	"Can I extend my rental?", "Wrong species delivered", "Question about feeding",
	"Tank bag leaked in transit", "Invoice shows the wrong price",
}

var replies = []string{
	"Thanks for getting in touch. We checked with the tank team and all is well.",
	"We rebooked the driver and added a day to your rental at no charge.",
	"A fresh health certificate is on its way to you.",
}

// Generator sends the synthetic traffic.
type Generator struct {
	BaseURL string
	Client  *http.Client
	Logger  *slog.Logger
	ok      int
	failed  int
	// returned holds the rentals this generator already asked to have collected. A rental
	// stays active until the van is back, so the list of rentals alone does not show it.
	returned map[int64]bool
}

// New returns a generator for the service at baseURL.
func New(baseURL string, logger *slog.Logger) (*Generator, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("API_URL is not set; set it to the URL of the web service")
	}
	return &Generator{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		Client:   &http.Client{Timeout: requestTimeout},
		Logger:   logger,
		returned: map[int64]bool{},
	}, nil
}

// Run sends traffic until ctx ends. A failed request is counted and logged; the generator
// keeps going, because a failing application is when the traffic matters most.
func (g *Generator) Run(ctx context.Context) {
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for tick := 1; ; tick++ {
		g.step(ctx, tick)
		if tick%summaryEvery == 0 {
			g.Logger.Info("loadgen summary", "requests_ok", g.ok, "requests_failed", g.failed)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// step is one visit: look at the board, then maybe rent, return or contact support.
func (g *Generator) step(ctx context.Context, tick int) {
	var fish []store.Fish
	inventoryErr := g.call(ctx, http.MethodGet, "/api/inventory", nil, &fish)
	var customers []store.Customer
	if g.call(ctx, http.MethodGet, "/api/customers", nil, &customers) != nil ||
		len(customers) == 0 {
		return
	}
	customer := customers[rand.IntN(len(customers))]
	if inventoryErr == nil && tick%2 == 0 {
		g.maybeRent(ctx, fish, customer)
	}
	g.maybeReturn(ctx)
	if tick%7 == 0 {
		g.openTicket(ctx, customer)
	}
	if tick%5 == 0 {
		g.answerTicket(ctx)
	}
}

func (g *Generator) maybeRent(ctx context.Context, fish []store.Fish, c store.Customer) {
	var available []store.Fish
	for _, f := range fish {
		if f.Status == domain.FishAvailable {
			available = append(available, f)
		}
	}
	if len(available) <= minAvailable {
		return
	}
	pick := available[rand.IntN(len(available))]
	body := store.NewRental{CustomerID: c.ID, FishID: pick.ID, Days: 1 + rand.IntN(7)}
	_ = g.call(ctx, http.MethodPost, "/api/rentals", body, nil)
}

func (g *Generator) maybeReturn(ctx context.Context) {
	var rentals []store.Rental
	if g.call(ctx, http.MethodGet, "/api/rentals", nil, &rentals) != nil {
		return
	}
	for _, r := range rentals {
		old := time.Since(r.CreatedAt) > minRentalAge
		if r.Status != domain.RentalActive || !old || g.returned[r.ID] || rand.IntN(4) != 0 {
			continue
		}
		path := fmt.Sprintf("/api/rentals/%d/return", r.ID)
		if g.call(ctx, http.MethodPost, path, nil, nil) == nil {
			g.returned[r.ID] = true
		}
		return
	}
}

func (g *Generator) openTicket(ctx context.Context, c store.Customer) {
	body := store.NewTicket{
		CustomerID: c.ID,
		Subject:    subjects[rand.IntN(len(subjects))],
		Priority:   domain.Priorities[rand.IntN(2)],
		Channel:    domain.Channels[rand.IntN(len(domain.Channels))],
		Body:       "Hello, could someone take a look at this for me? Thank you.",
	}
	_ = g.call(ctx, http.MethodPost, "/api/tickets", body, nil)
}

// answerTicket has the support desk answer the oldest open ticket and resolve it.
func (g *Generator) answerTicket(ctx context.Context) {
	var tickets []store.Ticket
	if g.call(ctx, http.MethodGet, "/api/tickets", nil, &tickets) != nil {
		return
	}
	var oldest *store.Ticket
	for i := range tickets {
		if tickets[i].Status != domain.TicketResolved {
			oldest = &tickets[i]
		}
	}
	if oldest == nil || time.Since(oldest.CreatedAt) < answerAfter {
		return
	}
	body := store.NewInteraction{
		Author:  "Support desk",
		Channel: domain.Channels[rand.IntN(len(domain.Channels))],
		Body:    replies[rand.IntN(len(replies))],
		Status:  domain.TicketResolved,
	}
	path := fmt.Sprintf("/api/tickets/%d/interactions", oldest.ID)
	_ = g.call(ctx, http.MethodPost, path, body, nil)
}

// call sends one request and decodes a JSON answer into out, when out is not nil.
func (g *Generator) call(ctx context.Context, method, path string, body, out any) error {
	err := g.do(ctx, method, path, body, out)
	if err != nil {
		g.failed++
		g.Logger.Warn("request failed", "method", method, "path", path, "error", err.Error())
		return err
	}
	g.ok++
	return nil
}

func (g *Generator) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.BaseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "paperboat-loadgen")
	resp, err := g.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		text, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(text)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode answer: %w", err)
	}
	return nil
}
