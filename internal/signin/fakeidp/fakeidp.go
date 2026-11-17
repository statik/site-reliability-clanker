// Package fakeidp is a small OpenID Connect provider for tests and for the checks in
// scripts/verify. It signs in whoever it is told to, with no password. Never run it where
// a real person signs in.
package fakeidp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
)

const keyID = "fake-key-1"

// Person is the account that the provider signs in.
type Person struct {
	Email    string
	Verified bool
}

type grant struct {
	clientID  string
	nonce     string
	challenge string
	person    Person
}

// Provider is the fake identity provider.
type Provider struct {
	// Issuer is the address at which the control plane and the browser reach the provider.
	Issuer string

	mu     sync.Mutex
	person Person
	codes  map[string]grant
	key    *rsa.PrivateKey
	signer jose.Signer
}

// New makes a provider with a new key. It signs in nobody until SignInAs is called.
func New(issuer string) (*Provider, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("make the key of the fake provider: %w", err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithHeader("kid", keyID))
	if err != nil {
		return nil, fmt.Errorf("make the signer of the fake provider: %w", err)
	}
	return &Provider{Issuer: issuer, codes: map[string]grant{}, key: key, signer: signer}, nil
}

// SignInAs sets the account that the next sign-in gets.
func (p *Provider) SignInAs(person Person) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.person = person
}

// Handler serves the provider.
func (p *Provider) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /keys", p.keys)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	mux.HandleFunc("POST /person", p.setPerson)
	return mux
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer": p.Issuer, "authorization_endpoint": p.Issuer + "/authorize",
		"token_endpoint": p.Issuer + "/token", "jwks_uri": p.Issuer + "/keys",
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
	})
}

func (p *Provider) keys(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: keyID,
		Algorithm: "RS256", Use: "sig"}}})
}

// setPerson lets a check choose the account: POST /person?email=...&verified=true.
func (p *Provider) setPerson(w http.ResponseWriter, r *http.Request) {
	p.SignInAs(Person{Email: r.URL.Query().Get("email"),
		Verified: r.URL.Query().Get("verified") != "false"})
	w.WriteHeader(http.StatusNoContent)
}

// authorize signs the current account in at once and sends the browser back.
func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	back, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || back.Host == "" || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "the request needs redirect_uri and a S256 code challenge",
			http.StatusBadRequest)
		return
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	p.mu.Lock()
	p.codes[code] = grant{clientID: q.Get("client_id"), nonce: q.Get("nonce"),
		challenge: q.Get("code_challenge"), person: p.person}
	p.mu.Unlock()
	answer := back.Query()
	answer.Set("code", code)
	answer.Set("state", q.Get("state"))
	back.RawQuery = answer.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

// token exchanges a code for an ID token. It checks the PKCE verifier as a real provider
// does, and a code works once.
func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	g, found := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code"))
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !found || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	}
	now := time.Now()
	claims, err := json.Marshal(map[string]any{"iss": p.Issuer, "aud": g.clientID,
		"sub": "fake|" + g.person.Email, "email": g.person.Email,
		"email_verified": g.person.Verified, "nonce": g.nonce, "iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix()})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	signed, err := p.signer.Sign(claims)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	idToken, err := signed.CompactSerialize()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"access_token": "fake-access-token", "token_type": "Bearer",
		"expires_in": 300, "id_token": idToken})
}
