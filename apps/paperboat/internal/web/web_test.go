package web

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/buildinfo"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/store"
)

// fakeAPI is the api service as the web service sees it: the network boundary.
func fakeAPI(t *testing.T, inventoryStatus int) *httptest.Server {
	t.Helper()
	condition, retire := 65, 400
	answers := map[string]any{
		"/api/version": apiVersion{Version: "1.1.0", SchemaVersion: 2, BaseImage: "base:1"},
		"/api/tanks":   []store.Tank{{ID: 1, Name: "Kelp Room", Capacity: 12, Occupancy: 1}},
		"/api/inventory": []store.Fish{
			{ID: 1, Name: "Dory", Species: "Betta", TankID: 1, Status: "available", AgeDays: 200,
				Condition: &condition, RetireAfterDays: &retire},
			{ID: 2, Name: "Gill", Species: "Koi", TankID: 1, Status: "rented", AgeDays: 900,
				Condition: &condition, RetireAfterDays: &retire},
		},
		"/api/rentals":    []store.Rental{{ID: 5, Customer: "Ada Brine", Status: "active"}},
		"/api/customers":  []store.Customer{{ID: 1, Name: "Ada Brine"}},
		"/api/deliveries": []store.Delivery{{ID: 8, Kind: "delivery", Status: "scheduled"}},
		"/api/tickets": []store.Ticket{
			{ID: 3, Subject: "Pale fish", Status: "open", Priority: "high", CreatedAt: time.Now()},
		},
		"/api/tickets/3/interactions": []store.Interaction{{ID: 1, Author: "Ada Brine",
			Channel: "chat", Body: "Dory looks pale"}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer, ok := answers[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/inventory" && inventoryStatus != http.StatusOK {
			w.WriteHeader(inventoryStatus)
			_, _ = io.WriteString(w, `{"error":"internal error"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(server.Close)
	return server
}

func get(t *testing.T, level int, apiURL, path string) (int, string) {
	t.Helper()
	build := buildinfo.Info{Version: "9.9.9", Level: level}
	server, err := New(build, apiURL, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

func wantAll(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestBoardGroupsFishByTankAndShowsTheVersion(t *testing.T) {
	api := fakeAPI(t, http.StatusOK)
	status, body := get(t, 2, api.URL, "/")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	wantAll(t, body, "Kelp Room", "Dory", "Gill", `data-status="rented"`, "6 months",
		`id="app-version">9.9.9<`, "API 1.1.0", "Schema 2", "Base image base:1",
		`data-band="good"`, "Condition 65 of 100")
}

func TestBoardBefore110ShowsAgeButNoCondition(t *testing.T) {
	api := fakeAPI(t, http.StatusOK)
	_, body := get(t, 1, api.URL, "/")
	wantAll(t, body, "Dory", "6 months")
	if strings.Contains(body, "Condition 65") || strings.Contains(body, `class="condition"`) {
		t.Error("the 1.0.0 board shows a condition")
	}
}

func TestBoardSaysWhyTheInventoryIsMissing(t *testing.T) {
	api := fakeAPI(t, http.StatusInternalServerError)
	status, body := get(t, 2, api.URL, "/")
	if status != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", status)
	}
	wantAll(t, body, "data-load-error", "GET /api/inventory answered 500 internal error")
}

func TestOtherPagesRender(t *testing.T) {
	api := fakeAPI(t, http.StatusOK)
	cases := map[string][]string{
		"/rentals":    {"Ada Brine", `data-post="/api/rentals/5/return"`, "Dory, Betta"},
		"/deliveries": {`data-post="/api/deliveries/8/status"`, "Send out", "Not written yet"},
		"/support":    {"Pale fish", `href="/support/3"`, "1 open"},
		"/support/3":  {"Pale fish", "Dory looks pale", `/api/tickets/3/interactions`},
	}
	for path, wants := range cases {
		status, body := get(t, 2, api.URL, path)
		if status != http.StatusOK {
			t.Errorf("%s: status = %d", path, status)
		}
		wantAll(t, body, wants...)
	}
	if status, _ := get(t, 2, api.URL, "/support/999"); status != http.StatusNotFound {
		t.Errorf("unknown ticket: status = %d, want 404", status)
	}
}

func TestRentalsPageStillListsRentalsWhenInventoryFails(t *testing.T) {
	api := fakeAPI(t, http.StatusInternalServerError)
	status, body := get(t, 2, api.URL, "/rentals")
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	wantAll(t, body, "Ada Brine", "The list of fish did not load")
}

func TestProxyForwardsAPIPathsAndReportsADeadAPI(t *testing.T) {
	api := fakeAPI(t, http.StatusOK)
	status, body := get(t, 2, api.URL, "/api/customers")
	if status != http.StatusOK || !strings.Contains(body, "Ada Brine") {
		t.Errorf("proxy: status = %d, body = %s", status, body)
	}
	api.Close()
	status, body = get(t, 2, api.URL, "/api/customers")
	if status != http.StatusBadGateway || !strings.Contains(body, `"error"`) {
		t.Errorf("dead api: status = %d, body = %s", status, body)
	}
	status, body = get(t, 2, api.URL, "/")
	if status != http.StatusBadGateway || !strings.Contains(body, "The API does not answer") {
		t.Errorf("dead api page: status = %d", status)
	}
}

func TestNewRejectsABadAPIURL(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	for _, bad := range []string{"", "paperboat-api:8081", "/just/a/path"} {
		if _, err := New(buildinfo.Info{Level: 2}, bad, logger); err == nil {
			t.Errorf("API_URL %q was accepted", bad)
		}
	}
}

func TestFormatters(t *testing.T) {
	cases := []struct{ name, got, want string }{
		{"ageText(0)", ageText(0), "0 days"},
		{"ageText(59)", ageText(59), "59 days"},
		{"ageText(60)", ageText(60), "2 months"},
		{"ageText(729)", ageText(729), "24 months"},
		{"ageText(730)", ageText(730), "2.0 years"},
		{"ageText(1000)", ageText(1000), "2.7 years"},
		{"money(1350)", money(1350), "$13.50"},
		{"money(5)", money(5), "$0.05"},
		{"statusLabel", statusLabel("in_transit"), "In transit"},
		{"statusLabel empty", statusLabel(""), ""},
		{"band 100", conditionBand(100), "good"},
		{"band 60", conditionBand(60), "good"},
		{"band 59", conditionBand(59), "fair"},
		{"band 30", conditionBand(30), "fair"},
		{"band 29", conditionBand(29), "poor"},
		{"band 0", conditionBand(0), "poor"},
		{"ago 30s", ago(30 * time.Second), "just now"},
		{"ago 5m", ago(5 * time.Minute), "5 min ago"},
		{"ago 3h", ago(3 * time.Hour), "3 h ago"},
		{"ago 50h", ago(50 * time.Hour), "2 d ago"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestBuildBoardCountsEachStatus(t *testing.T) {
	board := buildBoard(
		[]store.Tank{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}},
		[]store.Fish{{ID: 1, TankID: 1, Status: "available"}, {ID: 2, TankID: 1, Status: "rented"},
			{ID: 3, TankID: 1, Status: "available"}},
	)
	got := []int{board.Total, len(board.Tanks), len(board.Tanks[0].Fish),
		len(board.Tanks[1].Fish), len(board.Statuses)}
	if want := []int{3, 2, 3, 0, 6}; !slices.Equal(got, want) {
		t.Fatalf("board sizes = %v, want %v", got, want)
	}
	want := []statusCount{{"available", 2}, {"reserved", 0}, {"in_transit", 0}, {"rented", 1},
		{"quarantined", 0}, {"retired", 0}}
	if !slices.Equal(board.Statuses, want) {
		t.Errorf("statuses = %+v, want %+v", board.Statuses, want)
	}
}
