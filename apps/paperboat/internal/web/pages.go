package web

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/store"
)

// statusOrder is the order of fish status values on the board.
var statusOrder = []string{
	domain.FishAvailable, domain.FishReserved, domain.FishInTransit, domain.FishRented,
	domain.FishQuarantined, domain.FishRetired,
}

type statusCount struct {
	Key   string
	Count int
}

type tankView struct {
	store.Tank
	Fish []store.Fish
}

type boardData struct {
	Total    int
	Statuses []statusCount
	Tanks    []tankView
}

// buildBoard groups the fish by tank and counts them by status.
func buildBoard(tanks []store.Tank, fish []store.Fish) boardData {
	board := boardData{Total: len(fish)}
	counts := map[string]int{}
	byTank := map[int64][]store.Fish{}
	for _, f := range fish {
		counts[f.Status]++
		byTank[f.TankID] = append(byTank[f.TankID], f)
	}
	for _, key := range statusOrder {
		board.Statuses = append(board.Statuses, statusCount{Key: key, Count: counts[key]})
	}
	for _, t := range tanks {
		board.Tanks = append(board.Tanks, tankView{Tank: t, Fish: byTank[t.ID]})
	}
	return board
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	var tanks []store.Tank
	var fish []store.Fish
	err := s.apiGet(r.Context(), "/api/tanks", &tanks)
	if err == nil {
		err = s.apiGet(r.Context(), "/api/inventory", &fish)
	}
	s.render(w, r, "board", "Inventory", buildBoard(tanks, fish), err)
}

type rentalsData struct {
	Rentals   []store.Rental
	Customers []store.Customer
	Available []store.Fish
	// FishError is set when only the list of fish for the form is missing.
	FishError string
}

func (s *Server) handleRentals(w http.ResponseWriter, r *http.Request) {
	var data rentalsData
	err := s.apiGet(r.Context(), "/api/rentals", &data.Rentals)
	if err == nil {
		err = s.apiGet(r.Context(), "/api/customers", &data.Customers)
	}
	var fish []store.Fish
	if fishErr := s.apiGet(r.Context(), "/api/inventory", &fish); fishErr != nil {
		data.FishError = fishErr.Error()
	}
	for _, f := range fish {
		if f.Status == domain.FishAvailable {
			data.Available = append(data.Available, f)
		}
	}
	s.render(w, r, "rentals", "Rentals", data, err)
}

func (s *Server) handleDeliveries(w http.ResponseWriter, r *http.Request) {
	var deliveries []store.Delivery
	err := s.apiGet(r.Context(), "/api/deliveries", &deliveries)
	s.render(w, r, "deliveries", "Deliveries", deliveries, err)
}

type supportData struct {
	Tickets   []store.Ticket
	Customers []store.Customer
	Open      int
}

func (s *Server) handleSupport(w http.ResponseWriter, r *http.Request) {
	var data supportData
	err := s.apiGet(r.Context(), "/api/tickets", &data.Tickets)
	if err == nil {
		err = s.apiGet(r.Context(), "/api/customers", &data.Customers)
	}
	for _, t := range data.Tickets {
		if t.Status == domain.TicketOpen {
			data.Open++
		}
	}
	s.render(w, r, "support", "Support desk", data, err)
}

type ticketData struct {
	Ticket       store.Ticket
	Interactions []store.Interaction
}

func (s *Server) handleTicket(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var data ticketData
	var tickets []store.Ticket
	err = s.apiGet(r.Context(), "/api/tickets", &tickets)
	found := false
	for _, t := range tickets {
		if t.ID == id {
			data.Ticket, found = t, true
		}
	}
	if err == nil && !found {
		http.NotFound(w, r)
		return
	}
	if err == nil {
		path := fmt.Sprintf("/api/tickets/%d/interactions", id)
		err = s.apiGet(r.Context(), path, &data.Interactions)
	}
	s.render(w, r, "ticket", fmt.Sprintf("Ticket %d", id), data, err)
}
