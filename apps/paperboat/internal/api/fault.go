package api

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/domain"
)

// Fault modes for POST /api/admin/fault.
const (
	FaultNone   = "none"
	FaultSlow   = "slow"
	FaultErrors = "errors"
)

// SlowDelay is the delay that the slow mode adds to each request.
const SlowDelay = 1500 * time.Millisecond

// Fault is the fault injection switch. It lives in memory, so a restart clears it.
type Fault struct {
	mu   sync.RWMutex
	mode string
}

// Mode returns the active mode.
func (f *Fault) Mode() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.mode == "" {
		return FaultNone
	}
	return f.mode
}

// Set changes the mode.
func (f *Fault) Set(mode string) error {
	switch mode {
	case FaultNone, FaultSlow, FaultErrors:
	default:
		return fmt.Errorf("%w: mode must be none, slow or errors, got %q", domain.ErrInvalid, mode)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mode = mode
	return nil
}

// faultExempt reports whether a path keeps working during an injected fault. Health checks
// stay honest about the database, and the switch itself must stay reachable.
func faultExempt(path string) bool {
	return path == "/api/healthz" || path == "/api/admin/fault"
}

// faultMiddleware applies the injected fault to the requests behind it.
func (s *Server) faultMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if faultExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		switch s.fault.Mode() {
		case FaultSlow:
			s.sleep(SlowDelay)
		case FaultErrors:
			if s.random() < 0.5 {
				s.writeError(w, r, errors.New("request handler failed"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// handleFault serves POST /api/admin/fault.
func (s *Server) handleFault(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AdminToken == "" {
		writeJSON(w, http.StatusForbidden,
			errorBody{"fault injection is off: ADMIN_TOKEN is not set on this service"})
		return
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.AdminToken)) != 1 {
		writeJSON(w, http.StatusUnauthorized,
			errorBody{"send Authorization: Bearer with the admin token"})
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := decodeBody(r, &body); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.fault.Set(body.Mode); err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"mode": s.fault.Mode()})
}
