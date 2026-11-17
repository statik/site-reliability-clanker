// Package web serves the browser UI and proxies /api/ to the api service.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/buildinfo"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/httplog"
)

// Addr is the listen address of the web service.
const Addr = ":8080"

const apiTimeout = 5 * time.Second

//go:embed templates/*.html static/*
var assets embed.FS

// Server renders the pages from the answers of the api service.
type Server struct {
	build  buildinfo.Info
	apiURL *url.URL
	client *http.Client
	logger *slog.Logger
	pages  map[string]*template.Template
	now    func() time.Time
}

// New returns a web server that reads from the api service at apiURL.
func New(build buildinfo.Info, apiURL string, logger *slog.Logger) (*Server, error) {
	if apiURL == "" {
		return nil, errors.New("API_URL is not set; set it to the URL of the api service")
	}
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("API_URL %q is not a URL such as http://paperboat-api:8081", apiURL)
	}
	s := &Server{
		build: build, apiURL: parsed, logger: logger, now: time.Now,
		client: &http.Client{Timeout: apiTimeout},
		pages:  map[string]*template.Template{},
	}
	for _, page := range []string{"board", "rentals", "deliveries", "support", "ticket"} {
		parsedPage, err := template.New("layout.html").Funcs(s.funcs()).
			ParseFS(assets, "templates/layout.html", "templates/"+page+".html")
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", page, err)
		}
		s.pages[page] = parsedPage
	}
	return s, nil
}

// Handler returns the routes of the web service with request logging.
func (s *Server) Handler() http.Handler {
	static, err := fs.Sub(assets, "static")
	if err != nil {
		panic("embedded static directory is missing: " + err.Error())
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", s.proxy())
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /{$}", s.handleBoard)
	mux.HandleFunc("GET /rentals", s.handleRentals)
	mux.HandleFunc("GET /deliveries", s.handleDeliveries)
	mux.HandleFunc("GET /support", s.handleSupport)
	mux.HandleFunc("GET /support/{id}", s.handleTicket)
	return httplog.Middleware(s.logger, nil, mux)
}

// proxy forwards /api/ to the api service. FlushInterval -1 sends each event of the
// inventory stream to the browser at once.
func (s *Server) proxy() http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(s.apiURL)
			r.SetXForwarded()
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if r.Context().Err() != nil {
				return
			}
			httplog.SetError(r, err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "the api service does not answer",
			})
		},
	}
}

// apiGet reads one JSON answer from the api service.
func (s *Server) apiGet(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiURL.JoinPath(path).String(),
		nil)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", path, err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("the api service does not answer on %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		var body struct {
			Error string `json:"error"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		_ = json.Unmarshal(raw, &body)
		return fmt.Errorf("GET %s answered %d %s", path, resp.StatusCode,
			strings.TrimSpace(body.Error))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode answer of %s: %w", path, err)
	}
	return nil
}

// apiVersion is the answer of /api/version.
type apiVersion struct {
	Version       string `json:"version"`
	SchemaVersion int    `json:"schema_version"`
	BaseImage     string `json:"base_image"`
}

// page is the data that every template gets.
type page struct {
	Title     string
	Active    string
	Version   string
	ShowAging bool
	API       *apiVersion
	LoadError string
	Data      any
}

// render writes one page. loadErr is the reason the data is missing, if it is.
func (s *Server) render(w http.ResponseWriter, r *http.Request, name, title string, data any,
	loadErr error,
) {
	p := page{
		Title: title, Active: name, Version: s.build.Version,
		ShowAging: s.build.Level >= 2, Data: data,
	}
	if name == "ticket" {
		p.Active = "support"
	}
	var version apiVersion
	if err := s.apiGet(r.Context(), "/api/version", &version); err == nil {
		p.API = &version
	}
	status := http.StatusOK
	if loadErr != nil {
		httplog.SetError(r, loadErr)
		p.LoadError = loadErr.Error()
		status = http.StatusBadGateway
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := s.pages[name].Execute(w, p); err != nil {
		httplog.SetError(r, err)
	}
}

func (s *Server) funcs() template.FuncMap {
	return template.FuncMap{
		"money":       money,
		"ageText":     ageText,
		"statusLabel": statusLabel,
		"band":        conditionBand,
		"deref":       func(v *int) int { return *v },
		"clock":       func(t time.Time) string { return t.UTC().Format("15:04") },
		"day":         func(t time.Time) string { return t.UTC().Format("2 Jan") },
		"ago":         func(t time.Time) string { return ago(s.now().Sub(t)) },
	}
}

// money formats cents as dollars.
func money(cents int) string {
	return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
}

// ageText formats an age in days the way a tank keeper says it.
func ageText(days int) string {
	switch {
	case days < 60:
		return fmt.Sprintf("%d days", days)
	case days < 730:
		return fmt.Sprintf("%d months", days/30)
	default:
		return fmt.Sprintf("%d.%d years", days/365, (days%365)*10/365)
	}
}

// statusLabel turns a status value such as in_transit into words.
func statusLabel(status string) string {
	words := strings.ReplaceAll(status, "_", " ")
	if words == "" {
		return ""
	}
	return strings.ToUpper(words[:1]) + words[1:]
}

// conditionBand names the colour band of a condition score.
func conditionBand(condition int) string {
	switch {
	case condition >= 60:
		return "good"
	case condition >= 30:
		return "fair"
	default:
		return "poor"
	}
}

// ago formats a duration as a short phrase.
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d d ago", int(d.Hours()/24))
	}
}
