package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"slices"
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
	// approverEmailsEnv names more people who can approve, separated by commas. A change
	// of tier T3 needs two approvers, and the admin is only one.
	approverEmailsEnv = "CLANKER_APPROVER_EMAILS"
)

// signInSettings holds what the control plane needs so that people sign in.
type signInSettings struct {
	provider       signin.Config
	adminEmail     string
	approverEmails []string
}

// loadSignIn reads the settings of the sign-in. On a cloud people must sign in, so a
// missing setting stops the start. On the local target the sign-in is on when the issuer
// is set; without it the demo names its user in a header.
func loadSignIn(o options) (*signInSettings, error) {
	settings := signInSettings{provider: signin.Config{Issuer: os.Getenv(issuerEnv),
		ClientID: os.Getenv(clientIDEnv), ClientSecret: os.Getenv(clientSecretEnv),
		PublicURL: os.Getenv(publicURLEnv)}, adminEmail: os.Getenv(adminEmailEnv)}
	approvers := os.Getenv(approverEmailsEnv)
	if o.cloud == "local" && settings.provider.Issuer == "" {
		if approvers != "" {
			return nil, fmt.Errorf("%s is set and the sign-in is off, so nobody in it could "+
				"sign in: set %s and the other settings of the sign-in, or remove %s",
				approverEmailsEnv, issuerEnv, approverEmailsEnv)
		}
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
	var err error
	if settings.approverEmails, err = parseApproverEmails(approvers,
		settings.adminEmail); err != nil {
		return nil, err
	}
	return &settings, nil
}

// parseApproverEmails reads the setting CLANKER_APPROVER_EMAILS: email addresses separated
// by commas, in lower case and each one once. The admin is not one of them, because one
// person counts as one approver. An empty setting names nobody.
func parseApproverEmails(value, adminEmail string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	admin := strings.ToLower(strings.TrimSpace(adminEmail))
	emails := []string{}
	for i, entry := range strings.Split(value, ",") {
		email := strings.ToLower(strings.TrimSpace(entry))
		var problem string
		switch {
		case email == "":
			problem = fmt.Sprintf("entry %d is empty", i+1)
		case !isEmailAddress(email):
			problem = fmt.Sprintf("%q is not an email address", strings.TrimSpace(entry))
		case email == admin:
			problem = fmt.Sprintf("%s is the admin of %s, who approves already", email,
				adminEmailEnv)
		case slices.Contains(emails, email):
			problem = fmt.Sprintf("%s comes twice", email)
		}
		if problem != "" {
			return nil, fmt.Errorf("%s is not valid: %s. Give email addresses separated by "+
				"commas, such as approver@example.com,second@example.com", approverEmailsEnv,
				problem)
		}
		emails = append(emails, email)
	}
	return emails, nil
}

// isEmailAddress reports whether the text is one bare email address, with no display name
// and no angle brackets.
func isEmailAddress(text string) bool {
	address, err := mail.ParseAddress(text)
	return err == nil && address.Name == "" && address.Address == text
}

// ensureAdmin makes the first admin, so that one person can sign in to a new control
// plane. It changes nothing when a user with this email address exists.
func ensureAdmin(db *sql.DB, email string) error {
	if err := ensureUser(db, strings.ToLower(email), "admin"); err != nil {
		return fmt.Errorf("make the first admin: %w", err)
	}
	return nil
}

// ensureApprovers makes the approvers of CLANKER_APPROVER_EMAILS, so that a change of tier
// T3 can get its second approver on a new control plane. Like ensureAdmin, it changes
// nothing for an email address that a user has: that user keeps the role, and an address
// that leaves the setting removes nobody.
func ensureApprovers(db *sql.DB, emails []string) error {
	for _, email := range emails {
		if err := ensureUser(db, email, "approver"); err != nil {
			return fmt.Errorf("make an approver of %s: %w", approverEmailsEnv, err)
		}
	}
	return nil
}

// ensureUser adds a user with the email address as the name, unless a user has that name
// or that email address.
func ensureUser(db *sql.DB, email, role string) error {
	_, err := db.Exec(`INSERT INTO users (name, display, role, email) VALUES ($1, $1, $2, $1)
		ON CONFLICT DO NOTHING`, email, role)
	if err != nil {
		return fmt.Errorf("add the user %s with the role %s: %w", email, role, err)
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
	if err := ensureApprovers(c.db, settings.approverEmails); err != nil {
		return err
	}
	sessions, err := signin.New(ctx, c.db, settings.provider)
	if err != nil {
		return err
	}
	server.Identify, server.SignIn = identify(sessions, c.orch), sessions.Handler()
	if c.chat != nil {
		c.chat.serve(server, sessions)
	}
	return nil
}
