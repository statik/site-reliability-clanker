package chatops

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/signin"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// A chat account is linked to a person once. The first click of an account that has no
// link gets a code in a private answer. The person opens the link page with the code in a
// browser, signs in, sees which chat account goes to which email address, and confirms.
// The page follows the rules of the device page in package signin: only a browser session
// counts, only a POST from a page of this site links, and a code works once.

const (
	// linkLife is how long a person has to open the link page and confirm.
	linkLife = 10 * time.Minute
	linkPath = "/auth/chat-link"
)

func hash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// askToLink makes a link code for a chat account that has no link, and returns the private
// answer with the address of the link page. An account has one code at a time. This leaves
// no audit event: anybody in the channel can click.
func (s *Service) askToLink(ctx context.Context, a Account) Answer {
	failed := Answer{Text: "The control plane could not make a link code. Try again."}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		slog.Error("chat approval: make a link code", "error", err)
		return failed
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM chat_link_codes WHERE expires <= $1 OR
		(platform = $2 AND workspace = $3 AND account = $4)`, store.FormatTime(now), a.Platform,
		a.Workspace, a.ID); err != nil {
		slog.Error("chat approval: remove old link codes", "error", err)
		return failed
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO chat_link_codes (id, platform, workspace,
		account, account_name, expires, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		hash(code), a.Platform, a.Workspace, a.ID, a.Name, store.FormatTime(now.Add(linkLife)),
		store.FormatTime(now)); err != nil {
		slog.Error("chat approval: record a link code", "error", err)
		return failed
	}
	return Answer{Text: "Nothing was decided. Link this " + labels[a.Platform] + " account to " +
		"your sign-in first. The link works once and for ten minutes. Then click again.",
		URL: s.public + linkPath + "?code=" + url.QueryEscape(code), URLTitle: "Link the account"}
}

// LinkHandler serves the link page. Mount it at /auth/chat-link, below the open path of
// the sign-in: the page asks for a browser session itself.
func (s *Service) LinkHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+linkPath, s.linkPage)
	mux.HandleFunc("POST "+linkPath, s.linkChange)
	return mux
}

var linkTemplate = template.Must(template.New("link").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Link a chat account</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>body{font-family:system-ui,sans-serif;max-width:38rem;margin:4rem auto;padding:0 1rem;
line-height:1.5}button{font-size:1rem;padding:.5rem .75rem}td{padding:.25rem 1rem .25rem 0}
.note{color:#525252}</style>
</head><body>
<h1>Link a chat account</h1>
{{if .Problem}}<p><strong>{{.Problem}}</strong></p>{{end}}
{{if .Done}}<p>{{.Done}}</p>{{end}}
{{if .Code}}
<p>This {{.Pending.Platform}} account will decide on changes as <strong>{{.Email}}</strong>:</p>
<table><tr><td>Account</td><td><strong>{{.Pending.Name}}</strong> ({{.Pending.ID}})</td></tr>
<tr><td>{{.Pending.Place}}</td><td>{{.Pending.Workspace}}</td></tr></table>
{{if .NoRole}}<p class="note">{{.Email}} has no role in this control plane. You can link the
account, and you cannot approve until an admin adds this email address.</p>{{end}}
<p>Confirm only when this is your own account and you clicked the button in the chat
yourself. If somebody sent you this address, close the page.</p>
<form method="post" action="/auth/chat-link"><input type="hidden" name="code" value="{{.Code}}">
<button>Link this account</button></form>
{{end}}
<h2>Accounts linked to {{.Email}}</h2>
{{if .Links}}<table>{{range .Links}}<tr><td>{{.Platform}}</td>
<td><strong>{{.Name}}</strong> ({{.ID}}) in {{.Workspace}}</td><td>
<form method="post" action="/auth/chat-link"><input type="hidden" name="action" value="unlink">
<input type="hidden" name="platform" value="{{.Key}}">
<input type="hidden" name="workspace" value="{{.Workspace}}">
<input type="hidden" name="account" value="{{.ID}}"><button>Remove</button></form></td></tr>
{{end}}</table>{{else}}<p class="note">No chat account is linked.</p>{{end}}
</body></html>`))

// linkRow is a chat account as the page shows it.
type linkRow struct {
	// Key is the name of the platform in the database, and Platform is its label.
	Key, Platform, Workspace, ID, Name string
	// Place is the word for the workspace on that platform.
	Place string
}

func row(a Account) linkRow {
	place := map[string]string{"slack": "Workspace", "teams": "Tenant"}[a.Platform]
	return linkRow{Key: a.Platform, Platform: labels[a.Platform], Workspace: a.Workspace, ID: a.ID,
		Name: a.Name, Place: place}
}

type linkView struct {
	Email, Code, Problem, Done string
	Pending                    linkRow
	NoRole                     bool
	Links                      []linkRow
}

func (s *Service) links(ctx context.Context, email string) ([]linkRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT platform, workspace, account, account_name
		FROM chat_links WHERE email = $1 ORDER BY platform, workspace, account`, email)
	if err != nil {
		return nil, fmt.Errorf("list the linked chat accounts: %w", err)
	}
	defer rows.Close()
	links := []linkRow{}
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.Platform, &a.Workspace, &a.ID, &a.Name); err != nil {
			return nil, fmt.Errorf("read a linked chat account: %w", err)
		}
		links = append(links, row(a))
	}
	return links, rows.Err()
}

// show writes the link page. The page is never kept, never framed, and sends no referrer,
// because its address can hold a link code.
func (s *Service) show(w http.ResponseWriter, r *http.Request, status int, view linkView) {
	links, err := s.links(r.Context(), view.Email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	view.Links = links
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	if err := linkTemplate.Execute(w, view); err != nil {
		return
	}
}

const unknownCode = "This link code is not known, was used, or is older than ten minutes. " +
	"Click the button in the chat again to get a new one."

// pendingAccount returns the chat account of a link code that is valid now.
func (s *Service) pendingAccount(ctx context.Context, code string) (Account, error) {
	var a Account
	var expires string
	err := s.db.QueryRowContext(ctx, `SELECT platform, workspace, account, account_name, expires
		FROM chat_link_codes WHERE id = $1`, hash(code)).Scan(&a.Platform, &a.Workspace, &a.ID,
		&a.Name, &expires)
	if err == nil && !s.now().Before(store.ParseTime(expires)) {
		return Account{}, sql.ErrNoRows
	}
	return a, err
}

// linkPage shows a signed-in person the chat account of a code and asks for a
// confirmation. It changes nothing.
func (s *Service) linkPage(w http.ResponseWriter, r *http.Request) {
	email, err := s.Sessions.BrowserEmail(r)
	if errors.Is(err, signin.ErrNoSession) {
		http.Redirect(w, r, "/auth/login?return="+url.QueryEscape(r.URL.RequestURI()),
			http.StatusFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		s.show(w, r, http.StatusOK, linkView{Email: email})
		return
	}
	account, err := s.pendingAccount(r.Context(), code)
	if errors.Is(err, sql.ErrNoRows) {
		s.show(w, r, http.StatusNotFound, linkView{Email: email, Problem: unknownCode})
		return
	}
	if err != nil {
		http.Error(w, "read the link code: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_, userErr := s.orch.GetUserByEmail(email)
	s.show(w, r, http.StatusOK, linkView{Email: email, Code: code, Pending: row(account),
		NoRole: userErr != nil})
}

// confirmer returns the person who may change links with this request. Only a browser
// session counts, and only a request that comes from a page of this site. When nobody may,
// confirmer writes the answer and returns false.
func (s *Service) confirmer(w http.ResponseWriter, r *http.Request) (string, bool) {
	email, err := s.Sessions.BrowserEmail(r)
	if errors.Is(err, signin.ErrNoSession) {
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

func (s *Service) linkChange(w http.ResponseWriter, r *http.Request) {
	email, ok := s.confirmer(w, r)
	if !ok {
		return
	}
	var view linkView
	var err error
	if r.FormValue("action") == "unlink" {
		view, err = s.unlink(r.Context(), email, Account{Platform: r.FormValue("platform"),
			Workspace: r.FormValue("workspace"), ID: r.FormValue("account")})
	} else {
		view, err = s.link(r.Context(), email, r.FormValue("code"))
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	status := http.StatusOK
	if view.Problem != "" {
		status = http.StatusNotFound
	}
	s.show(w, r, status, view)
}

// link uses a code up and links its chat account to the person. The removal of the code
// says whether this request is the one that links, so that a code works once.
func (s *Service) link(ctx context.Context, email, code string) (linkView, error) {
	var a Account
	err := s.db.QueryRowContext(ctx, `DELETE FROM chat_link_codes WHERE id = $1 AND expires > $2
		RETURNING platform, workspace, account, account_name`, hash(code),
		store.FormatTime(s.now())).Scan(&a.Platform, &a.Workspace, &a.ID, &a.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return linkView{Email: email, Problem: unknownCode}, nil
	}
	if err != nil {
		return linkView{}, fmt.Errorf("use the link code: %w", err)
	}
	// An account has one link. A new link of the same account replaces the old one.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO chat_links (platform, workspace, account,
		account_name, email, created_at) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (platform, workspace, account) DO UPDATE SET email = excluded.email,
		account_name = excluded.account_name, created_at = excluded.created_at`, a.Platform,
		a.Workspace, a.ID, a.Name, email, store.FormatTime(s.now())); err != nil {
		return linkView{}, fmt.Errorf("record the link: %w", err)
	}
	if err := s.emit(audit.Event{Type: "chat.linked", Actor: email,
		Summary: fmt.Sprintf("%s linked to %s", a, email),
		Data:    map[string]any{"email": email, "origin": a.origin()}}); err != nil {
		return linkView{}, err
	}
	return linkView{Email: email, Done: "The account is linked. Go back to the chat and click " +
		"again."}, nil
}

// unlink removes a link of the person. A person removes only links to the own email.
func (s *Service) unlink(ctx context.Context, email string, a Account) (linkView, error) {
	err := s.db.QueryRowContext(ctx, `DELETE FROM chat_links WHERE platform = $1
		AND workspace = $2 AND account = $3 AND email = $4 RETURNING account_name`, a.Platform,
		a.Workspace, a.ID, email).Scan(&a.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return linkView{Email: email, Problem: "This account is not linked to " + email + "."}, nil
	}
	if err != nil {
		return linkView{}, fmt.Errorf("remove the link: %w", err)
	}
	if err := s.emit(audit.Event{Type: "chat.unlinked", Actor: email,
		Summary: fmt.Sprintf("%s unlinked from %s", a, email),
		Data:    map[string]any{"email": email, "origin": a.origin()}}); err != nil {
		return linkView{}, err
	}
	return linkView{Email: email, Done: "The link is removed."}, nil
}
