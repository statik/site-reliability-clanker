package broker

import (
	"context"
	"fmt"
	"time"

	"github.com/statik/site-reliability-clanker/internal/store"
)

// Credential is what the broker gives a runner for one grant.
type Credential struct {
	GrantID string
	// KubeConfig is the text of a kubeconfig with a short-lived token. It is empty for a
	// layer that needs no cluster credential. The broker keeps no copy.
	KubeConfig string
	// Mechanism says in plain words what the credential is.
	Mechanism string
}

// Issuer makes and withdraws the credentials of grants on one kind of target. The broker
// calls it only for a grant that it checked. Each target has its own issuer, because the
// credential comes from the API of the target.
type Issuer interface {
	// Issue returns a credential that is valid for no longer than the target allows after
	// remaining, the time until the grant expires.
	Issue(ctx context.Context, g Grant, remaining time.Duration) (Credential, error)
	// Withdraw makes the credential of an ended grant invalid.
	Withdraw(ctx context.Context, g Grant) error
}

// Exchange checks the grant and returns a credential that expires with it.
func (b *Broker) Exchange(ctx context.Context, g Grant, p Presentation) (Credential, error) {
	if err := b.Check(g, p); err != nil {
		return Credential{}, err
	}
	if b.issuer == nil {
		return Credential{}, fmt.Errorf("grant %s: this control plane has no credential issuer "+
			"for the target of %s", g.ID, g.Env)
	}
	remaining := store.ParseTime(g.Expires).Sub(b.clock.Now())
	cred, err := b.issuer.Issue(ctx, g, remaining)
	if err != nil {
		return Credential{}, err
	}
	cred.GrantID = g.ID
	return cred, b.setStatus(g.ID, "active", false)
}

// Revoke ends a grant and then makes its credential invalid. The grant is ended also when
// the issuer fails.
func (b *Broker) Revoke(ctx context.Context, g Grant, outcome string) error {
	if err := b.setStatus(g.ID, outcome, true); err != nil {
		return err
	}
	if b.issuer == nil {
		return nil
	}
	return b.issuer.Withdraw(ctx, g)
}

// ExpireOverdue marks grants whose window ended without a revoke. It returns them.
func (b *Broker) ExpireOverdue() ([]Grant, error) {
	now := store.FormatTime(b.clock.Now())
	rows, err := b.db.Query(`SELECT `+grantColumns+` FROM grants
		WHERE status IN ('issued', 'active') AND expires <= $1`, now)
	if err != nil {
		return nil, fmt.Errorf("find overdue grants: %w", err)
	}
	overdue := []Grant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		overdue = append(overdue, g)
	}
	rows.Close()
	for _, g := range overdue {
		if err := b.setStatus(g.ID, "expired", true); err != nil {
			return nil, err
		}
	}
	return overdue, nil
}
