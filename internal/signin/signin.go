// Package signin signs a person in with OpenID Connect. The control plane runs the
// authorization code flow with PKCE itself, against any provider that publishes a discovery
// document, so that the same sign-in works on every cloud and on a laptop.
//
// A session is a row in the database of the control plane. The browser holds a random
// token in a cookie, and the database holds only the hash of the token.
package signin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/statik/site-reliability-clanker/internal/store"
)

const (
	sessionCookie = "clanker_session"
	loginCookie   = "clanker_login"
	// sessionLife is how long a person stays signed in.
	sessionLife = 12 * time.Hour
	// loginLife is how long a person has to finish the sign-in at the provider.
	loginLife = 10 * time.Minute
)

// ErrNoSession means that the request has no session that is valid now.
var ErrNoSession = errors.New("not signed in")

// Config is the registration of the control plane at the identity provider.
type Config struct {
	// Issuer is the address of the provider, as its discovery document gives it.
	Issuer string
	// ClientID and ClientSecret are the credentials of the control plane at the provider.
	ClientID     string
	ClientSecret string
	// PublicURL is the address of the dashboard as a browser sees it. The provider sends
	// the person back to PublicURL + "/auth/callback".
	PublicURL string
}

// Service signs people in and knows their sessions.
type Service struct {
	db       *sql.DB
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
	// secure is set when the dashboard is served over HTTPS. The cookies then go over
	// HTTPS only.
	secure bool
	now    func() time.Time
}

// New reads the discovery document of the provider. It fails when the provider does not
// answer, so that a wrong address stops the start and not the first sign-in.
func New(ctx context.Context, db *sql.DB, cfg Config) (*Service, error) {
	public, err := url.Parse(cfg.PublicURL)
	if err != nil || public.Host == "" {
		return nil, fmt.Errorf("the public address %q of the dashboard is not a URL", cfg.PublicURL)
	}
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("the sign-in needs the issuer, the client ID and the client secret")
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("read the discovery document of the identity provider %s: %w",
			cfg.Issuer, err)
	}
	return &Service{
		db:       db,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth: oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
			Endpoint:    provider.Endpoint(),
			RedirectURL: strings.TrimSuffix(cfg.PublicURL, "/") + "/auth/callback",
			Scopes:      []string{oidc.ScopeOpenID, "email", "profile"}},
		secure: public.Scheme == "https",
		now:    time.Now,
	}, nil
}

func random() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("make a random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Service) cookie(name, value, path string, life time.Duration) *http.Cookie {
	c := &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true, Secure: s.secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(life / time.Second)}
	if life <= 0 {
		c.MaxAge = -1
	}
	return c
}

// localPath returns target when it is a path on this site, and "/" for anything else, so
// that the sign-in cannot send a person to another site.
func localPath(target string) string {
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") ||
		strings.Contains(target, "\\") {
		return "/"
	}
	return target
}

// Handler serves the sign-in below /auth/.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/login", s.login)
	mux.HandleFunc("GET /auth/callback", s.callback)
	mux.HandleFunc("POST /auth/logout", s.logout)
	return mux
}

// login sends the person to the provider. The state, the nonce and the PKCE verifier stay
// in a cookie of this browser until the provider sends the person back.
func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	state, err := random()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	nonce, err := random()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	verifier := oauth2.GenerateVerifier()
	back := url.QueryEscape(localPath(r.URL.Query().Get("return")))
	http.SetCookie(w, s.cookie(loginCookie, strings.Join([]string{state, nonce, verifier, back},
		"."), "/auth/", loginLife))
	http.Redirect(w, r, s.oauth.AuthCodeURL(state, oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

// identity checks the answer of the provider and returns the verified email.
func (s *Service) identity(r *http.Request) (email, back string, err error) {
	saved, err := r.Cookie(loginCookie)
	if err != nil {
		return "", "", errors.New("this browser did not start a sign-in, or the sign-in took " +
			"longer than ten minutes")
	}
	parts := strings.Split(saved.Value, ".")
	if len(parts) != 4 {
		return "", "", errors.New("the sign-in cookie is not valid")
	}
	state, nonce, verifier := parts[0], parts[1], parts[2]
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(state)) != 1 {
		return "", "", errors.New("the answer of the identity provider does not belong to this sign-in")
	}
	if problem := r.URL.Query().Get("error"); problem != "" {
		return "", "", fmt.Errorf("the identity provider refused the sign-in: %s", problem)
	}
	token, err := s.oauth.Exchange(r.Context(), r.URL.Query().Get("code"),
		oauth2.VerifierOption(verifier))
	if err != nil {
		return "", "", fmt.Errorf("exchange the code at the identity provider: %w", err)
	}
	raw, _ := token.Extra("id_token").(string)
	if raw == "" {
		return "", "", errors.New("the identity provider sent no ID token")
	}
	idToken, err := s.verifier.Verify(r.Context(), raw)
	if err != nil {
		return "", "", fmt.Errorf("verify the ID token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		return "", "", errors.New("the ID token does not belong to this sign-in")
	}
	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return "", "", fmt.Errorf("read the claims of the ID token: %w", err)
	}
	if claims.Email == "" || !claims.Verified {
		return "", "", errors.New("the identity provider did not confirm an email address for " +
			"this account: the ID token needs the claims email and email_verified")
	}
	back, err = url.QueryUnescape(parts[3])
	if err != nil {
		back = "/"
	}
	return strings.ToLower(claims.Email), localPath(back), nil
}

func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, s.cookie(loginCookie, "", "/auth/", 0))
	email, back, err := s.identity(r)
	if err != nil {
		http.Error(w, "Sign-in failed: "+err.Error(), http.StatusForbidden)
		return
	}
	token, err := random()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := s.now()
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE expires <= $1`,
		store.FormatTime(now)); err != nil {
		http.Error(w, "remove old sessions: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO sessions (id, email, expires,
		created_at) VALUES ($1, $2, $3, $4)`, hash(token), email,
		store.FormatTime(now.Add(sessionLife)), store.FormatTime(now)); err != nil {
		http.Error(w, "record the session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, s.cookie(sessionCookie, token, "/", sessionLife))
	http.Redirect(w, r, back, http.StatusFound)
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	if saved, err := r.Cookie(sessionCookie); err == nil {
		if _, err := s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = $1`,
			hash(saved.Value)); err != nil {
			http.Error(w, "end the session: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	http.SetCookie(w, s.cookie(sessionCookie, "", "/", 0))
	w.WriteHeader(http.StatusNoContent)
}

// Email returns the email of the person whose session the request carries. It returns
// ErrNoSession when there is no session or the session ended.
func (s *Service) Email(r *http.Request) (string, error) {
	saved, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", ErrNoSession
	}
	var email, expires string
	err = s.db.QueryRowContext(r.Context(), `SELECT email, expires FROM sessions WHERE id = $1`,
		hash(saved.Value)).Scan(&email, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoSession
	}
	if err != nil {
		return "", fmt.Errorf("read the session: %w", err)
	}
	if !s.now().Before(store.ParseTime(expires)) {
		return "", ErrNoSession
	}
	return email, nil
}
