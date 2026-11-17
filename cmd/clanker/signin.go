package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/api"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/signin"
)

// The settings of the sign-in. The client secret is a secret: on a cloud account it must
// come from a secret store and not from the environment of the image.
const (
	publicURLEnv    = "CLANKER_PUBLIC_URL"
	issuerEnv       = "CLANKER_OIDC_ISSUER"
	clientIDEnv     = "CLANKER_OIDC_CLIENT_ID"
	clientSecretEnv = "CLANKER_OIDC_CLIENT_SECRET"
	adminEmailEnv   = "CLANKER_ADMIN_EMAIL"
)

// signInSettings holds what the control plane needs so that people sign in.
type signInSettings struct {
	provider   signin.Config
	adminEmail string
}

// loadSignIn reads the settings of the sign-in. On a cloud people must sign in, so a
// missing setting stops the start. On the local target the sign-in is on when the issuer
// is set; without it the demo names its user in a header.
func loadSignIn(o options) (*signInSettings, error) {
	settings := signInSettings{provider: signin.Config{Issuer: os.Getenv(issuerEnv),
		ClientID: os.Getenv(clientIDEnv), ClientSecret: os.Getenv(clientSecretEnv),
		PublicURL: os.Getenv(publicURLEnv)}, adminEmail: os.Getenv(adminEmailEnv)}
	if o.cloud == "local" && settings.provider.Issuer == "" {
		return nil, nil
	}
	missing := []string{}
	for name, value := range map[string]string{issuerEnv: settings.provider.Issuer,
		clientIDEnv: settings.provider.ClientID, clientSecretEnv: settings.provider.ClientSecret,
		publicURLEnv: settings.provider.PublicURL, adminEmailEnv: settings.adminEmail} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("people must sign in to this control plane, and these settings "+
			"are not set: %s", strings.Join(missing, ", "))
	}
	return &settings, nil
}

// ensureAdmin makes the first admin, so that one person can sign in to a new control
// plane. It changes nothing when a user with this email address exists.
func ensureAdmin(db *sql.DB, email string) error {
	email = strings.ToLower(email)
	_, err := db.Exec(`INSERT INTO users (name, display, role, email) VALUES ($1, $1, 'admin', $1)
		ON CONFLICT DO NOTHING`, email)
	if err != nil {
		return fmt.Errorf("make the first admin %s: %w", email, err)
	}
	return nil
}

// identify returns the function that names the user behind a request: the person of the
// session, when the users table has that email address.
func identify(sessions *signin.Service, orch *orchestrator.Orchestrator) func(*http.Request) (
	orchestrator.User, error) {
	return func(r *http.Request) (orchestrator.User, error) {
		email, err := sessions.Email(r)
		if errors.Is(err, signin.ErrNoSession) {
			return orchestrator.User{}, api.ErrNotSignedIn
		}
		if err != nil {
			return orchestrator.User{}, err
		}
		user, err := orch.GetUserByEmail(email)
		if err != nil {
			return orchestrator.User{}, api.Forbidden(fmt.Errorf("%s is signed in and has no "+
				"role in this control plane: an admin must add this email address", email))
		}
		return user, nil
	}
}

// withSignIn switches the sign-in on in the API when the settings ask for it.
func withSignIn(ctx context.Context, server *api.Server, c *components, o options) error {
	settings, err := loadSignIn(o)
	if err != nil || settings == nil {
		return err
	}
	if err := ensureAdmin(c.db, settings.adminEmail); err != nil {
		return err
	}
	sessions, err := signin.New(ctx, c.db, settings.provider)
	if err != nil {
		return err
	}
	server.Identify, server.SignIn = identify(sessions, c.orch), sessions.Handler()
	return nil
}
