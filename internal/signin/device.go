package signin

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/store"
)

// The device flow signs in a program that has no browser: git, a script, a phone app. It
// follows the OAuth 2.0 device authorization grant (RFC 8628), and the control plane is
// its own authorization server. The program shows a short code. A person who is signed in
// in a browser approves the code. The program then gets a token of its own.

const (
	// deviceLife is how long a person has to approve a code.
	deviceLife = 10 * time.Minute
	// tokenLife is how long the token of a program is valid.
	tokenLife = 12 * time.Hour
	// pollSeconds is how long a program must wait between two questions for its token.
	pollSeconds = 5
	// codeLetters has no vowel and no digit, so that a code reads clearly and spells no word.
	codeLetters = "BCDFGHJKLMNPQRSTVWXZ"
)

// userCode makes the code that the person types or confirms, in the form XXXX-XXXX.
func userCode() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("make a code: %w", err)
	}
	code := make([]byte, 0, 9)
	for i, b := range raw {
		if i == 4 {
			code = append(code, '-')
		}
		code = append(code, codeLetters[int(b)%len(codeLetters)])
	}
	return string(code), nil
}

// normalCode brings a code that a person typed to the stored form.
func normalCode(typed string) string {
	letters := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(typed))
	if len(letters) != 8 {
		return ""
	}
	return letters[:4] + "-" + letters[4:]
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		// The client went away. Nothing more can be sent.
		return
	}
}

// deviceCode starts a sign-in for a program. Anybody can ask: the answer is of no use
// until a signed-in person approves the code.
func (s *Service) deviceCode(w http.ResponseWriter, r *http.Request) {
	device, err := random()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	code, err := userCode()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := s.now()
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM device_codes WHERE expires <= $1`,
		store.FormatTime(now)); err != nil {
		http.Error(w, "remove old codes: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO device_codes (id, user_code, status,
		expires, created_at) VALUES ($1, $2, 'pending', $3, $4)`, hash(device), code,
		store.FormatTime(now.Add(deviceLife)), store.FormatTime(now)); err != nil {
		http.Error(w, "record the code: "+err.Error(), http.StatusInternalServerError)
		return
	}
	page := s.public + "/auth/device"
	writeJSON(w, http.StatusOK, map[string]any{"device_code": device, "user_code": code,
		"verification_uri":          page,
		"verification_uri_complete": page + "?user_code=" + url.QueryEscape(code),
		"expires_in":                int(deviceLife / time.Second), "interval": pollSeconds})
}

// claim uses a device code up. It returns the email of the person who approved the code,
// or the reason why the program gets no token. A device code gives one answer: the row is
// removed, and the removal says whether this request is the one that gets the answer, so
// that two requests at the same moment do not both get a token.
func (s *Service) claim(r *http.Request, id string) (email, refusal string, err error) {
	var status, expires string
	err = s.db.QueryRowContext(r.Context(), `SELECT status, email, expires FROM device_codes
		WHERE id = $1`, id).Scan(&status, &email, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "expired_token", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("read the code: %w", err)
	}
	if refusal := notDecided(status, s.now(), store.ParseTime(expires)); refusal != "" {
		return "", refusal, nil
	}
	removed, err := s.db.ExecContext(r.Context(), `DELETE FROM device_codes WHERE id = $1`, id)
	if err != nil {
		return "", "", fmt.Errorf("use the code: %w", err)
	}
	if rows, err := removed.RowsAffected(); err != nil || rows != 1 {
		return "", "expired_token", nil
	}
	if status != "approved" {
		return "", "access_denied", nil
	}
	return email, "", nil
}

// notDecided returns why a code cannot be used yet or any more, or nothing when a person
// decided on it in time.
func notDecided(status string, now, expires time.Time) string {
	if !now.Before(expires) {
		return "expired_token"
	}
	if status == "pending" {
		return "authorization_pending"
	}
	return ""
}

// issue makes a token for a program that works as the person with this email.
func (s *Service) issue(r *http.Request, email string) (string, error) {
	token, err := random()
	if err != nil {
		return "", err
	}
	now := s.now()
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM tokens WHERE expires <= $1`,
		store.FormatTime(now)); err != nil {
		return "", fmt.Errorf("remove old tokens: %w", err)
	}
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO tokens (id, email, expires, created_at)
		VALUES ($1, $2, $3, $4)`, hash(token), email, store.FormatTime(now.Add(tokenLife)),
		store.FormatTime(now)); err != nil {
		return "", fmt.Errorf("record the token: %w", err)
	}
	return token, nil
}

// deviceToken gives a program its token when a person approved its code.
func (s *Service) deviceToken(w http.ResponseWriter, r *http.Request) {
	email, refusal, err := s.claim(r, hash(r.FormValue("device_code")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if refusal != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": refusal})
		return
	}
	token, err := s.issue(r, email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"access_token": token, "token_type": "Bearer",
		"expires_in": int(tokenLife / time.Second)})
}

// browserEmail returns the person of the browser session of a request. A program with a
// token is not a person in a browser: it must not approve a code and get more tokens.
func (s *Service) browserEmail(r *http.Request) (string, error) {
	if _, err := r.Cookie(sessionCookie); err != nil {
		return "", ErrNoSession
	}
	return s.Email(r)
}

var devicePage = template.Must(template.New("device").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Sign a program in</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>body{font-family:system-ui,sans-serif;max-width:34rem;margin:4rem auto;padding:0 1rem;line-height:1.5}
code{font-size:1.5rem;letter-spacing:.1em}input,button{font-size:1rem;padding:.5rem .75rem}
button{margin-right:.5rem}.note{color:#525252}</style></head><body>
<h1>Sign a program in</h1>
{{if .Done}}<p>{{.Done}}</p><p class="note">You can close this page.</p>
{{else if .Code}}
<p>A program asks to work as <strong>{{.Email}}</strong> for twelve hours. It shows this code:</p>
<p><code>{{.Code}}</code></p>
<p>Approve only when you started that program yourself and it shows the same code.</p>
<form method="post" action="/auth/device"><input type="hidden" name="user_code" value="{{.Code}}">
<button name="decision" value="approve">Approve</button>
<button name="decision" value="deny">Deny</button></form>
{{else}}
{{if .Problem}}<p><strong>{{.Problem}}</strong></p>{{end}}
<p>Type the code that the program shows.</p>
<form method="get" action="/auth/device"><input name="user_code" autocomplete="off" autofocus
 aria-label="Code"> <button>Continue</button></form>
{{end}}</body></html>`))

type deviceView struct {
	Email, Code, Problem, Done string
}

func (s *Service) showDevice(w http.ResponseWriter, status int, view deviceView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.WriteHeader(status)
	if err := devicePage.Execute(w, view); err != nil {
		return
	}
}

// pending reports whether a code waits for a decision now.
func (s *Service) pending(r *http.Request, code string) (bool, error) {
	var expires string
	err := s.db.QueryRowContext(r.Context(), `SELECT expires FROM device_codes
		WHERE user_code = $1 AND status = 'pending'`, code).Scan(&expires)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read the code: %w", err)
	}
	return s.now().Before(store.ParseTime(expires)), nil
}

// devicePage shows a signed-in person the code of a program and asks for a decision.
func (s *Service) devicePage(w http.ResponseWriter, r *http.Request) {
	email, err := s.browserEmail(r)
	if errors.Is(err, ErrNoSession) {
		http.Redirect(w, r, "/auth/login?return="+url.QueryEscape(r.URL.RequestURI()),
			http.StatusFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	typed := r.URL.Query().Get("user_code")
	if typed == "" {
		s.showDevice(w, http.StatusOK, deviceView{Email: email})
		return
	}
	code := normalCode(typed)
	waits, err := s.pending(r, code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !waits {
		s.showDevice(w, http.StatusNotFound, deviceView{Email: email,
			Problem: "This code is not known, or it is older than ten minutes."})
		return
	}
	s.showDevice(w, http.StatusOK, deviceView{Email: email, Code: code})
}

// decider returns the person who may decide on a code with this request. Only a browser
// session counts, and only a request that comes from a page of this site. When nobody may
// decide, decider writes the answer and returns false.
func (s *Service) decider(w http.ResponseWriter, r *http.Request) (string, bool) {
	email, err := s.browserEmail(r)
	if errors.Is(err, ErrNoSession) {
		http.Error(w, "Sign in to the dashboard first.", http.StatusUnauthorized)
		return "", false
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return "", false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.public {
		http.Error(w, "This request did not come from the dashboard.", http.StatusForbidden)
		return "", false
	}
	return email, true
}

// deviceDecision records the decision of a signed-in person on a code.
func (s *Service) deviceDecision(w http.ResponseWriter, r *http.Request) {
	email, ok := s.decider(w, r)
	if !ok {
		return
	}
	status := map[string]string{"approve": "approved", "deny": "denied"}[r.FormValue("decision")]
	if status == "" {
		http.Error(w, "The decision must be approve or deny.", http.StatusBadRequest)
		return
	}
	changed, err := s.db.ExecContext(r.Context(), `UPDATE device_codes SET status = $1, email = $2
		WHERE user_code = $3 AND status = 'pending' AND expires > $4`, status, email,
		normalCode(r.FormValue("user_code")), store.FormatTime(s.now()))
	if err != nil {
		http.Error(w, "record the decision: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if rows, err := changed.RowsAffected(); err != nil || rows != 1 {
		s.showDevice(w, http.StatusNotFound, deviceView{Email: email,
			Problem: "This code is not known, or it is older than ten minutes."})
		return
	}
	done := "The program is signed in as " + email + "."
	if status == "denied" {
		done = "The program was refused."
	}
	s.showDevice(w, http.StatusOK, deviceView{Email: email, Done: done})
}
