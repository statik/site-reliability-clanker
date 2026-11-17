// Package api is the HTTP interface of the control plane. The dashboard uses nothing else.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/agent"
	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/broker"
	"github.com/statik/site-reliability-clanker/internal/detect"
	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/notify"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/sim"
)

// Server holds the components that the handlers call.
type Server struct {
	Orch     *orchestrator.Orchestrator
	Audit    *audit.Log
	Forge    *forge.Forge
	Broker   *broker.Broker
	Monitor  *monitor.Monitor
	Notifier *notify.Notifier
	Agent    *agent.Agent
	Detector *detect.Detector
	Sim      *sim.Simulator
	Clock    *sim.Clock
	// Web is the built dashboard. Nil serves the API only.
	Web fs.FS
	// Runners serves the runners of the jobs, below /runner/v1/. Each request carries the
	// token of one job. The user header means nothing there.
	Runners http.Handler
	// Identify returns the user behind a request, or ErrNotSignedIn. When it is set, every
	// route needs a user except the health path, the sign-in and the runner path. Nil is
	// the demo: the dashboard names its user in a header, which is not authentication.
	Identify func(r *http.Request) (orchestrator.User, error)
	// SignIn serves the sign-in below /auth/. It is set together with Identify.
	SignIn http.Handler
}

// ErrNotSignedIn means that a request carries no valid sign-in.
var ErrNotSignedIn = errors.New("not signed in")

// statusError carries an HTTP status with an error.
type statusError struct {
	status int
	err    error
}

func (e statusError) Error() string { return e.err.Error() }

func badRequest(format string, args ...any) error {
	return statusError{http.StatusBadRequest, fmt.Errorf(format, args...)}
}

func notFound(err error) error { return statusError{http.StatusNotFound, err} }

func forbidden(err error) error { return statusError{http.StatusForbidden, err} }

// Forbidden marks err as the answer to a person who is known and may not do this.
func Forbidden(err error) error { return forbidden(err) }

type handler func(r *http.Request) (any, error)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		// The client went away. Nothing more can be sent.
		return
	}
}

func wrap(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, err := h(r)
		if err != nil {
			status := http.StatusInternalServerError
			var se statusError
			switch {
			case errors.As(err, &se):
				status = se.status
			case errors.Is(err, ErrNotSignedIn):
				status = http.StatusUnauthorized
			case errors.Is(err, orchestrator.ErrForbidden):
				status = http.StatusForbidden
			case errors.Is(err, orchestrator.ErrConflict):
				status = http.StatusConflict
			case errors.Is(err, orchestrator.ErrInvalid):
				status = http.StatusBadRequest
			case strings.Contains(err.Error(), "does not exist"):
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, value)
	}
}

func decodeBody(r *http.Request, into any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return badRequest("decode request body: %v", err)
	}
	return nil
}

func intParam(r *http.Request, name string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return fallback
	}
	return value
}

// currentUser names the user behind a request. With a sign-in it is the signed-in person,
// and the X-Clanker-User header means nothing. In the demo it is the user that the
// dashboard names in that header from its user switcher, which is not authentication.
func (s *Server) currentUser(r *http.Request) (orchestrator.User, error) {
	if s.Identify != nil {
		return s.Identify(r)
	}
	name := r.Header.Get("X-Clanker-User")
	if name == "" {
		name = "dave"
	}
	user, err := s.Orch.GetUser(name)
	if err != nil {
		return orchestrator.User{}, forbidden(err)
	}
	return user, nil
}

// Handler returns the routes of the control plane.
func (s *Server) Handler() (http.Handler, error) {
	mux := http.NewServeMux()
	routes := map[string]handler{
		"GET /api/v1/auth":                            s.getAuth,
		"GET /api/v1/me":                              s.getMe,
		"POST /api/v1/me/seen":                        s.postSeen,
		"GET /api/v1/users":                           s.getUsers,
		"GET /api/v1/overview":                        s.getOverview,
		"GET /api/v1/work-items":                      s.getWorkItems,
		"GET /api/v1/work-items/{id}":                 s.getWorkItem,
		"POST /api/v1/work-items/{id}/decision":       s.postDecision,
		"GET /api/v1/work-items/{id}/plans/{layer}":   s.getPlanJSON,
		"GET /api/v1/jobs/{id}/log":                   s.getJobLog,
		"GET /api/v1/events":                          s.getEvents,
		"GET /api/v1/audit/verify":                    s.getAuditVerify,
		"GET /api/v1/audit/evidence":                  s.getEvidence,
		"GET /api/v1/timeline":                        s.getTimeline,
		"GET /api/v1/topology":                        s.getTopology,
		"GET /api/v1/posture":                         s.getPosture,
		"GET /api/v1/health":                          s.getHealth,
		"GET /api/v1/access":                          s.getAccess,
		"GET /api/v1/findings":                        s.getFindings,
		"GET /api/v1/notifications":                   s.getNotifications,
		"GET /api/v1/forge/repos":                     s.getRepos,
		"GET /api/v1/forge/repos/{repo}/tree":         s.getTree,
		"GET /api/v1/forge/repos/{repo}/blob":         s.getBlob,
		"GET /api/v1/forge/repos/{repo}/log":          s.getLog,
		"GET /api/v1/forge/pulls":                     s.getPulls,
		"GET /api/v1/forge/pulls/{id}":                s.getPull,
		"POST /api/v1/forge/pulls":                    s.postPull,
		"GET /api/v1/chat":                            s.getChat,
		"POST /api/v1/chat":                           s.postChat,
		"GET /api/v1/agent/calls":                     s.getAgentCalls,
		"GET /api/v1/sim":                             s.getSim,
		"POST /api/v1/sim/advance":                    s.postSimAdvance,
		"POST /api/v1/sim/scenes/{name}":              s.postSimScene,
		"POST /api/v1/debug-sessions":                 s.postDebugSession,
		"POST /api/v1/runbooks/{name}":                s.postRunbook,
		"POST /api/v1/notifications/digest":           s.postDigest,
		"POST /api/v1/environments/{env}/blank-slate": s.postBlankSlate,
		"POST /api/v1/environments/{env}/teardown":    s.postTeardown,
	}
	for pattern, h := range routes {
		mux.HandleFunc(pattern, wrap(h))
	}
	mux.HandleFunc("GET /api/v1/events/stream", s.streamEvents)
	gitHandler, err := s.Forge.HTTPHandler("/git/")
	if err != nil {
		return nil, err
	}
	mux.Handle("/git/", gitHandler)
	if s.Runners != nil {
		mux.Handle("/runner/v1/", s.Runners)
	}
	if s.Web != nil {
		mux.Handle("/", spaHandler(s.Web))
	}
	// A load balancer asks this path whether the control plane is up.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	if s.Identify == nil {
		return mux, nil
	}
	if s.SignIn == nil {
		return nil, errors.New("the API has a way to identify a user and no sign-in")
	}
	mux.Handle("/auth/", s.SignIn)
	return s.gate(mux), nil
}

// getAuth tells the dashboard whether people sign in or the demo names the user.
func (s *Server) getAuth(*http.Request) (any, error) {
	return map[string]bool{"sign_in": s.Identify != nil}, nil
}

// open reports whether a path needs no signed-in user: the health path, the sign-in, and
// the runner path, where each request carries the token of one job.
func open(path string) bool {
	return path == "/healthz" || strings.HasPrefix(path, "/auth/") ||
		strings.HasPrefix(path, "/runner/v1/")
}

// gitPush reports whether a request is a part of a push with the git client: the list of
// references for a push, or the objects.
func gitPush(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/git/") {
		return false
	}
	return strings.HasSuffix(r.URL.Path, "/git-receive-pack") ||
		r.URL.Query().Get("service") == "git-receive-pack"
}

// gate lets a request through only when a known user is signed in. A browser that opens a
// page goes to the sign-in. Every other request gets an error and no content.
func (s *Server) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if open(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		user, err := s.Identify(r)
		var se statusError
		switch {
		case err == nil && gitPush(r) && user.Role != "approver" && user.Role != "admin":
			writeJSON(w, http.StatusForbidden, map[string]string{"error": user.Name +
				" has role " + user.Role + "; a push needs one of: approver, admin"})
		case err == nil:
			next.ServeHTTP(w, r)
		case errors.Is(err, ErrNotSignedIn):
			page := r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/") &&
				!strings.HasPrefix(r.URL.Path, "/git/")
			if page {
				http.Redirect(w, r, "/auth/login?return="+url.QueryEscape(r.URL.RequestURI()),
					http.StatusFound)
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not signed in"})
		case errors.As(err, &se):
			writeJSON(w, se.status, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	})
}

// spaHandler serves the dashboard files and falls back to index.html for client routes.
func spaHandler(web fs.FS) http.Handler {
	files := http.FileServerFS(web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(web, path); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	events, stop := s.Audit.Subscribe()
	defer stop()
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case e := <-events:
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "id: %d\nevent: audit\ndata: %s\n\n", e.Seq, data)
			flusher.Flush()
		}
	}
}
