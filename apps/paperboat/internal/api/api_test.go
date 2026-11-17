package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/buildinfo"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/store"
)

// fakeStore stands in for the database. A method that a test does not set panics through
// the nil embedded interface, which shows a handler that reads more than it should.
type fakeStore struct {
	Store
	pingErr      error
	inventory    []store.Fish
	rentalErr    error
	delivery     store.Delivery
	deliveryErr  error
	manifestPath string
	lastTicket   store.NewTicket
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeStore) SchemaVersion(context.Context) (int, error) { return 2, nil }

func (f *fakeStore) Counts(context.Context) (store.Counts, error) {
	return store.Counts{FishAvailable: 41, RentalsActive: 7, DeliveriesOut: 2, TicketsOpen: 3}, nil
}

func (f *fakeStore) Inventory(context.Context, time.Time) ([]store.Fish, error) {
	return f.inventory, nil
}

func (f *fakeStore) CreateRental(_ context.Context, in store.NewRental, _ time.Time,
) (store.Rental, error) {
	return store.Rental{ID: 9, FishID: in.FishID, Status: domain.RentalReserved}, f.rentalErr
}

func (f *fakeStore) ReturnRental(_ context.Context, id int64, _ time.Time) (store.Rental, error) {
	return store.Rental{ID: id, Status: domain.RentalActive}, f.rentalErr
}

func (f *fakeStore) Delivery(context.Context, int64) (store.Delivery, error) {
	return f.delivery, f.deliveryErr
}

func (f *fakeStore) TransitionDelivery(_ context.Context, id int64, to string, _ time.Time,
) (store.Delivery, error) {
	if err := domain.ValidateDeliveryTransition(f.delivery.Status, to); err != nil {
		return store.Delivery{}, err
	}
	f.delivery.ID, f.delivery.Status = id, to
	return f.delivery, f.deliveryErr
}

func (f *fakeStore) SetManifestPath(_ context.Context, _ int64, path string) error {
	f.manifestPath = path
	return nil
}

func (f *fakeStore) CreateTicket(_ context.Context, in store.NewTicket) (store.Ticket, error) {
	f.lastTicket = in
	return store.Ticket{ID: 4, Subject: in.Subject, Priority: in.Priority}, nil
}

func (f *fakeStore) AddInteraction(_ context.Context, id int64, in store.NewInteraction,
) (store.Interaction, error) {
	return store.Interaction{ID: 1, TicketID: id, Author: in.Author, Body: in.Body}, nil
}

type testServer struct {
	*Server
	db   *fakeStore
	logs *strings.Builder
}

func newTestServer(t *testing.T, build buildinfo.Info, adminToken string) *testServer {
	t.Helper()
	db := &fakeStore{}
	logs := &strings.Builder{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	cfg := Config{Build: build, AdminToken: adminToken, DataDir: t.TempDir()}
	server := NewServer(cfg, db, NewHub(), logger, make(chan struct{}))
	server.sleep = func(time.Duration) {}
	return &testServer{Server: server, db: db, logs: logs}
}

func (s *testServer) do(method, path, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, want, rec.Body.String())
	}
}

func wantErrorBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
		t.Fatalf("body is not {\"error\":...}: %s", rec.Body.String())
	}
	return body["error"]
}

var release110 = buildinfo.Info{Version: "1.1.0", BaseImage: "base:1", Level: 2}

func TestHealthz(t *testing.T) {
	s := newTestServer(t, release110, "")
	rec := s.do(http.MethodGet, "/api/healthz", "")
	wantStatus(t, rec, http.StatusOK)
	if strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Errorf("body = %s", rec.Body.String())
	}
	s.db.pingErr = errors.New("connection refused")
	rec = s.do(http.MethodGet, "/api/healthz", "")
	wantStatus(t, rec, http.StatusServiceUnavailable)
	wantErrorBody(t, rec)
}

func TestVersionAndStats(t *testing.T) {
	s := newTestServer(t, release110, "")
	rec := s.do(http.MethodGet, "/api/version", "")
	wantStatus(t, rec, http.StatusOK)
	want := `{"version":"1.1.0","schema_version":2,"base_image":"base:1"}`
	if strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("version = %s, want %s", rec.Body.String(), want)
	}
	s.do(http.MethodGet, "/api/nothing-here", "")
	rec = s.do(http.MethodGet, "/api/stats", "")
	wantStatus(t, rec, http.StatusOK)
	var stats map[string]float64
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]float64{"requests": 2, "errors": 0, "fish_available": 41,
		"rentals_active": 7, "deliveries_out": 2, "tickets_open": 3} {
		if stats[key] != want {
			t.Errorf("stats[%s] = %v, want %v", key, stats[key], want)
		}
	}
	if _, ok := stats["p95_ms"]; !ok {
		t.Error("stats lacks p95_ms")
	}
}

func TestInventoryIsAnArrayWhenEmpty(t *testing.T) {
	s := newTestServer(t, release110, "")
	rec := s.do(http.MethodGet, "/api/inventory", "")
	wantStatus(t, rec, http.StatusOK)
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("body = %s, want []", rec.Body.String())
	}
}

func TestBadReleaseFailsHalfOfInventoryOnly(t *testing.T) {
	bad := buildinfo.Info{Version: "1.2.0-bad", Level: 2, InventoryFault: true}
	s := newTestServer(t, bad, "")
	draws := []float64{0.1, 0.9}
	calls := 0
	s.random = func() float64 { calls++; return draws[calls%2] }
	failed := 0
	for range 100 {
		if s.do(http.MethodGet, "/api/inventory", "").Code == http.StatusInternalServerError {
			failed++
		}
	}
	if failed != 50 {
		t.Errorf("%d of 100 inventory requests failed, want 50", failed)
	}
	s.random = func() float64 { return 0 }
	wantStatus(t, s.do(http.MethodGet, "/api/healthz", ""), http.StatusOK)
	wantStatus(t, s.do(http.MethodGet, "/api/version", ""), http.StatusOK)
	rec := s.do(http.MethodGet, "/api/inventory", "")
	wantStatus(t, rec, http.StatusInternalServerError)
	wantErrorBody(t, rec)

	good := newTestServer(t, release110, "")
	good.random = func() float64 { return 0 }
	wantStatus(t, good.do(http.MethodGet, "/api/inventory", ""), http.StatusOK)
}

func TestFaultRequiresTheAdminToken(t *testing.T) {
	off := newTestServer(t, release110, "")
	rec := off.do(http.MethodPost, "/api/admin/fault", `{"mode":"errors"}`,
		"Authorization", "Bearer ")
	wantStatus(t, rec, http.StatusForbidden)
	wantErrorBody(t, rec)

	s := newTestServer(t, release110, "s3cret")
	cases := []struct {
		name   string
		header string
		body   string
		want   int
	}{
		{"no header", "", `{"mode":"slow"}`, http.StatusUnauthorized},
		{"wrong token", "Bearer nope", `{"mode":"slow"}`, http.StatusUnauthorized},
		{"wrong scheme", "Basic s3cret", `{"mode":"slow"}`, http.StatusUnauthorized},
		{"unknown mode", "Bearer s3cret", `{"mode":"chaos"}`, http.StatusBadRequest},
		{"not JSON", "Bearer s3cret", `mode=slow`, http.StatusBadRequest},
		{"slow", "Bearer s3cret", `{"mode":"slow"}`, http.StatusOK},
	}
	for _, c := range cases {
		rec := s.do(http.MethodPost, "/api/admin/fault", c.body, "Authorization", c.header)
		if rec.Code != c.want {
			t.Errorf("%s: status = %d, want %d", c.name, rec.Code, c.want)
		}
	}
	if s.fault.Mode() != FaultSlow {
		t.Errorf("mode = %s, want slow", s.fault.Mode())
	}
}

func TestInjectedFaults(t *testing.T) {
	s := newTestServer(t, release110, "s3cret")
	var slept time.Duration
	s.sleep = func(d time.Duration) { slept += d }
	setMode := func(mode string) {
		rec := s.do(http.MethodPost, "/api/admin/fault", fmt.Sprintf(`{"mode":%q}`, mode),
			"Authorization", "Bearer s3cret")
		wantStatus(t, rec, http.StatusOK)
	}

	setMode(FaultSlow)
	wantStatus(t, s.do(http.MethodGet, "/api/inventory", ""), http.StatusOK)
	if slept != SlowDelay {
		t.Errorf("slow mode slept %s, want %s", slept, SlowDelay)
	}
	wantStatus(t, s.do(http.MethodGet, "/api/healthz", ""), http.StatusOK)
	if slept != SlowDelay {
		t.Error("slow mode delayed the health check")
	}

	setMode(FaultErrors)
	s.random = func() float64 { return 0.2 }
	rec := s.do(http.MethodGet, "/api/inventory", "")
	wantStatus(t, rec, http.StatusInternalServerError)
	wantErrorBody(t, rec)
	wantStatus(t, s.do(http.MethodGet, "/api/healthz", ""), http.StatusOK)
	s.random = func() float64 { return 0.8 }
	wantStatus(t, s.do(http.MethodGet, "/api/inventory", ""), http.StatusOK)

	s.random = func() float64 { return 0.2 }
	setMode(FaultNone)
	wantStatus(t, s.do(http.MethodGet, "/api/inventory", ""), http.StatusOK)
}

func TestRentalErrorsMapToStatusCodes(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		storeErr error
		want     int
	}{
		{"created", `{"customer_id":1,"fish_id":2,"days":3}`, nil, http.StatusCreated},
		{"not JSON", `{`, nil, http.StatusBadRequest},
		{"missing fish", `{"customer_id":1,"days":3}`, nil, http.StatusBadRequest},
		{"fish is rented", `{"customer_id":1,"fish_id":2,"days":3}`,
			domain.CanReserve(domain.FishRented), http.StatusConflict},
		{"no such fish", `{"customer_id":1,"fish_id":99,"days":3}`,
			fmt.Errorf("%w: fish 99", domain.ErrNotFound), http.StatusNotFound},
		{"database down", `{"customer_id":1,"fish_id":2,"days":3}`,
			errors.New("connection reset"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		s := newTestServer(t, release110, "")
		s.db.rentalErr = c.storeErr
		rec := s.do(http.MethodPost, "/api/rentals", c.body)
		if rec.Code != c.want {
			t.Errorf("%s: status = %d, want %d", c.name, rec.Code, c.want)
		}
		if c.want >= 400 {
			message := wantErrorBody(t, rec)
			if strings.Contains(message, "connection reset") {
				t.Errorf("%s: the client sees the internal cause: %s", c.name, message)
			}
		}
	}
}

func TestReturnRental(t *testing.T) {
	s := newTestServer(t, release110, "")
	wantStatus(t, s.do(http.MethodPost, "/api/rentals/5/return", ""), http.StatusOK)
	wantStatus(t, s.do(http.MethodPost, "/api/rentals/five/return", ""), http.StatusBadRequest)
	wantStatus(t, s.do(http.MethodPost, "/api/rentals/0/return", ""), http.StatusBadRequest)
	s.db.rentalErr = domain.CanReturn(domain.RentalReserved, false)
	wantStatus(t, s.do(http.MethodPost, "/api/rentals/5/return", ""), http.StatusConflict)
}

func TestDeliveryStatusWritesTheManifest(t *testing.T) {
	s := newTestServer(t, release110, "")
	s.db.delivery = store.Delivery{Status: domain.DeliveryScheduled, Kind: domain.KindDelivery,
		Fish: "Dory", Customer: "Ada Brine"}
	wantStatus(t, s.do(http.MethodGet, "/api/deliveries/7/manifest", ""), http.StatusNotFound)

	rec := s.do(http.MethodPost, "/api/deliveries/7/status", `{"status":"out"}`)
	wantStatus(t, rec, http.StatusOK)
	if s.db.manifestPath != "manifests/delivery-7.txt" {
		t.Fatalf("manifest path = %q", s.db.manifestPath)
	}
	s.db.delivery.ManifestPath = s.db.manifestPath
	rec = s.do(http.MethodGet, "/api/deliveries/7/manifest", "")
	wantStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "Dory") {
		t.Errorf("manifest lacks the fish: %s", rec.Body.String())
	}

	wantStatus(t, s.do(http.MethodPost, "/api/deliveries/7/status", `{"status":"out"}`),
		http.StatusConflict)
	wantStatus(t, s.do(http.MethodPost, "/api/deliveries/7/status", `{"status":"lost"}`),
		http.StatusBadRequest)
	wantStatus(t, s.do(http.MethodPost, "/api/deliveries/7/status", `{"status":"done"}`),
		http.StatusOK)
}

func TestManifestPathCannotLeaveTheDataDirectory(t *testing.T) {
	s := newTestServer(t, release110, "")
	for _, path := range []string{"../../etc/passwd", "/etc/passwd", "certificates/fish-1.txt",
		"manifests/../../secret", "manifests/delivery-404.txt"} {
		s.db.delivery = store.Delivery{ManifestPath: path}
		rec := s.do(http.MethodGet, "/api/deliveries/7/manifest", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("path %q: status = %d, want 404", path, rec.Code)
		}
	}
}

func TestTicketValidation(t *testing.T) {
	s := newTestServer(t, release110, "")
	rec := s.do(http.MethodPost, "/api/tickets", `{"customer_id":3,"subject":"  Pale fish "}`)
	wantStatus(t, rec, http.StatusCreated)
	got := s.db.lastTicket
	if got.Subject != "Pale fish" || got.Priority != "normal" || got.Channel != "email" {
		t.Errorf("defaults not applied: %+v", got)
	}
	bad := []string{
		`{"subject":"No customer"}`,
		`{"customer_id":3,"subject":"   "}`,
		`{"customer_id":3,"subject":"x","priority":"asap"}`,
		`{"customer_id":3,"subject":"x","channel":"fax"}`,
	}
	for _, body := range bad {
		rec := s.do(http.MethodPost, "/api/tickets", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestInteractionValidation(t *testing.T) {
	s := newTestServer(t, release110, "")
	good := `{"author":"Support desk","channel":"phone","body":"Called back","status":"resolved"}`
	wantStatus(t, s.do(http.MethodPost, "/api/tickets/4/interactions", good), http.StatusCreated)
	bad := []string{
		`{"channel":"phone","body":"No author"}`,
		`{"author":"A","channel":"phone","body":""}`,
		`{"author":"A","channel":"pigeon","body":"x"}`,
		`{"author":"A","channel":"phone","body":"x","status":"closed"}`,
	}
	for _, body := range bad {
		rec := s.do(http.MethodPost, "/api/tickets/4/interactions", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestEachRequestLogsOneJSONLine(t *testing.T) {
	s := newTestServer(t, release110, "")
	s.db.rentalErr = errors.New("connection reset")
	s.do(http.MethodGet, "/api/healthz", "")
	s.do(http.MethodPost, "/api/rentals", `{"customer_id":1,"fish_id":2,"days":3}`)
	lines := strings.Split(strings.TrimSpace(s.logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d log lines for 2 requests:\n%s", len(lines), s.logs.String())
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil {
		t.Fatalf("log line is not JSON: %s", lines[1])
	}
	if entry["path"] != "/api/rentals" || entry["status"] != float64(500) ||
		entry["error"] != "connection reset" || entry["level"] != "ERROR" {
		t.Errorf("log line = %s", lines[1])
	}
}

func TestStreamSendsFishStatusEvents(t *testing.T) {
	s := newTestServer(t, release110, "")
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/api/inventory/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %s", got)
	}
	read := func(want string) {
		t.Helper()
		buffer := make([]byte, 256)
		n, err := resp.Body.Read(buffer)
		if err != nil && err != io.EOF {
			t.Fatal(err)
		}
		if got := string(buffer[:n]); got != want {
			t.Fatalf("stream sent %q, want %q", got, want)
		}
	}
	read(": connected\n\n")
	s.hub.Publish(`{"fish_id":3,"status":"reserved"}`)
	read("event: fish_status\ndata: {\"fish_id\":3,\"status\":\"reserved\"}\n\n")
}

func TestPercentile(t *testing.T) {
	if got := Percentile(nil, 95); got != 0 {
		t.Errorf("no values: %v", got)
	}
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(100 - i)
	}
	if got := Percentile(values, 95); got != 95 {
		t.Errorf("p95 of 1..100 = %v, want 95", got)
	}
	if got := Percentile([]float64{7}, 95); got != 7 {
		t.Errorf("p95 of one value = %v, want 7", got)
	}
}

func TestStatsCountErrorsAndSkipStreamTiming(t *testing.T) {
	stats := &Stats{}
	stats.Record(http.StatusOK, 10*time.Millisecond, true)
	stats.Record(http.StatusInternalServerError, 20*time.Millisecond, true)
	stats.Record(http.StatusNotFound, 30*time.Millisecond, true)
	stats.Record(http.StatusOK, time.Hour, false)
	requests, failed, p95 := stats.Snapshot()
	if requests != 4 || failed != 1 || p95 != 30 {
		t.Errorf("got %d requests, %d errors, p95 %v; want 4, 1, 30", requests, failed, p95)
	}
	for range 2 * durationWindow {
		stats.Record(http.StatusOK, time.Millisecond, true)
	}
	if _, _, p95 := stats.Snapshot(); p95 != 1 {
		t.Errorf("old durations stay in the window: p95 = %v", p95)
	}
}
