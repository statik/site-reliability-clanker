// Package broker is the access broker. It is the only component that holds trust in a
// target. It signs grants, checks them, and exchanges a valid grant for a credential that
// expires with the grant.
package broker

import (
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/policy"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// Grant kinds.
const (
	KindPlan    = "plan"
	KindApply   = "apply"
	KindRunbook = "runbook"
	KindDebug   = "debug"
)

// Grant is a signed record that the broker exchanges for a credential.
type Grant struct {
	ID         string   `json:"id"`
	WorkItem   string   `json:"work_item"`
	Env        string   `json:"env"`
	Layer      string   `json:"layer"`
	Identity   string   `json:"identity"`
	Kind       string   `json:"kind"`
	Commit     string   `json:"commit"`
	PlanSHA256 string   `json:"plan_sha256"`
	Tier       string   `json:"tier"`
	Approvers  []string `json:"approvers"`
	NotBefore  string   `json:"not_before"`
	Expires    string   `json:"expires"`
	Signature  string   `json:"signature"`
	Status     string   `json:"status"`
	IssuedAt   string   `json:"issued_at"`
	EndedAt    string   `json:"ended_at"`
}

// Clock gives the virtual time.
type Clock interface{ Now() time.Time }

// Broker issues and checks grants.
type Broker struct {
	db    *sql.DB
	clock Clock
	key   ed25519.PrivateKey
	// issuer makes the credentials of the target. It is nil when the control plane has no
	// issuer for its target; the broker then signs and checks grants but exchanges none.
	issuer Issuer
}

// New loads or creates the signing key in dataDir. issuer can be nil.
func New(db *sql.DB, clock Clock, dataDir string, issuer Issuer) (*Broker, error) {
	keyPath := filepath.Join(dataDir, "broker.key")
	raw, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		_, key, genErr := ed25519.GenerateKey(rand.Reader)
		if genErr != nil {
			return nil, fmt.Errorf("generate broker signing key: %w", genErr)
		}
		raw = key
		if err := os.WriteFile(keyPath, raw, 0o600); err != nil {
			return nil, fmt.Errorf("write broker signing key %s: %w", keyPath, err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("read broker signing key %s: %w", keyPath, err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("broker signing key %s has %d bytes, want %d: delete it to "+
			"create a new key", keyPath, len(raw), ed25519.PrivateKeySize)
	}
	return &Broker{db: db, clock: clock, key: raw, issuer: issuer}, nil
}

func (g Grant) signedText() string {
	return strings.Join([]string{g.ID, g.WorkItem, g.Env, g.Layer, g.Identity, g.Kind, g.Commit,
		g.PlanSHA256, g.Tier, strings.Join(g.Approvers, ","), g.NotBefore, g.Expires}, "\n")
}

// IdentityFor returns the identity that a grant of this kind and layer uses.
func IdentityFor(kind, layer string) string {
	switch kind {
	case KindPlan:
		return "planner"
	case KindRunbook:
		return "runbook"
	case KindDebug:
		return "debugger"
	default:
		return "applier-" + layer
	}
}

// Request asks for a grant.
type Request struct {
	WorkItem   string
	Env        string
	Layer      string
	Kind       string
	Commit     string
	PlanSHA256 string
	Tier       string
	Approvers  []string
	NotBefore  time.Time
	Duration   time.Duration
}

func distinct(names []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, n := range names {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// Issue signs and records a grant. It refuses a request that does not have the approvals
// that the tier requires.
func (b *Broker) Issue(req Request) (Grant, error) {
	approvers := distinct(req.Approvers)
	need := policy.ApproversRequired(req.Tier)
	if req.Kind == KindApply && len(approvers) < need {
		return Grant{}, fmt.Errorf("issue grant for %s %s/%s: tier %s needs %d approvers, got %d",
			req.WorkItem, req.Env, req.Layer, req.Tier, need, len(approvers))
	}
	if req.Kind == KindApply && (req.Commit == "" || req.PlanSHA256 == "") {
		return Grant{}, fmt.Errorf("issue grant for %s %s/%s: an apply grant needs a commit "+
			"and a plan hash", req.WorkItem, req.Env, req.Layer)
	}
	now := b.clock.Now()
	notBefore := req.NotBefore
	if notBefore.IsZero() {
		notBefore = now
	}
	idBytes := make([]byte, 6)
	if _, err := rand.Read(idBytes); err != nil {
		return Grant{}, fmt.Errorf("create grant ID: %w", err)
	}
	g := Grant{
		ID:       fmt.Sprintf("G-%x", idBytes),
		WorkItem: req.WorkItem, Env: req.Env, Layer: req.Layer, Kind: req.Kind,
		Identity: IdentityFor(req.Kind, req.Layer),
		Commit:   req.Commit, PlanSHA256: req.PlanSHA256, Tier: req.Tier, Approvers: approvers,
		NotBefore: store.FormatTime(notBefore),
		Expires:   store.FormatTime(notBefore.Add(req.Duration)),
		Status:    "issued", IssuedAt: store.FormatTime(now),
	}
	g.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(b.key, []byte(g.signedText())))
	approversJSON, _ := json.Marshal(g.Approvers)
	_, err := b.db.Exec(`INSERT INTO grants (id, work_item, env, layer, identity, kind,
		commit_sha, plan_sha256, tier, approvers, not_before, expires, signature, status,
		issued_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		g.ID, g.WorkItem, g.Env, g.Layer, g.Identity, g.Kind, g.Commit, g.PlanSHA256, g.Tier,
		string(approversJSON), g.NotBefore, g.Expires, g.Signature, g.Status, g.IssuedAt)
	if err != nil {
		return Grant{}, fmt.Errorf("record grant %s: %w", g.ID, err)
	}
	return g, nil
}

// Presentation is what a runner shows the broker together with the grant.
type Presentation struct {
	Commit     string
	PlanSHA256 string
}

// Check verifies the signature, the time window, the status, and that the commit and plan
// hash that the runner holds are the ones in the grant.
func (b *Broker) Check(g Grant, p Presentation) error {
	sig, err := base64.StdEncoding.DecodeString(g.Signature)
	if err != nil {
		return fmt.Errorf("grant %s: signature is not base64: %w", g.ID, err)
	}
	public, _ := b.key.Public().(ed25519.PublicKey)
	if !ed25519.Verify(public, []byte(g.signedText()), sig) {
		return fmt.Errorf("grant %s: signature is not valid", g.ID)
	}
	stored, err := b.Get(g.ID)
	if err != nil {
		return err
	}
	if stored.Status != "issued" && stored.Status != "active" {
		return fmt.Errorf("grant %s: status is %s", g.ID, stored.Status)
	}
	now := b.clock.Now()
	if now.Before(store.ParseTime(g.NotBefore)) {
		return fmt.Errorf("grant %s: not valid before %s", g.ID, g.NotBefore)
	}
	if !now.Before(store.ParseTime(g.Expires)) {
		return fmt.Errorf("grant %s: expired at %s", g.ID, g.Expires)
	}
	if g.Kind != KindApply {
		return nil
	}
	if p.Commit != g.Commit {
		return fmt.Errorf("grant %s: runner holds commit %s, grant is for commit %s", g.ID,
			p.Commit, g.Commit)
	}
	if p.PlanSHA256 != g.PlanSHA256 {
		return fmt.Errorf("grant %s: runner holds plan %s, grant is for plan %s", g.ID,
			p.PlanSHA256, g.PlanSHA256)
	}
	return nil
}

const grantColumns = `id, work_item, env, layer, identity, kind, commit_sha, plan_sha256, tier,
	approvers, not_before, expires, signature, status, issued_at, ended_at`

func scanGrant(row interface{ Scan(...any) error }) (Grant, error) {
	var g Grant
	var approvers string
	err := row.Scan(&g.ID, &g.WorkItem, &g.Env, &g.Layer, &g.Identity, &g.Kind, &g.Commit,
		&g.PlanSHA256, &g.Tier, &approvers, &g.NotBefore, &g.Expires, &g.Signature, &g.Status,
		&g.IssuedAt, &g.EndedAt)
	if err != nil {
		return Grant{}, err
	}
	if err := json.Unmarshal([]byte(approvers), &g.Approvers); err != nil {
		return Grant{}, fmt.Errorf("decode approvers of grant %s: %w", g.ID, err)
	}
	return g, nil
}

// Get returns one grant.
func (b *Broker) Get(id string) (Grant, error) {
	g, err := scanGrant(b.db.QueryRow(`SELECT `+grantColumns+` FROM grants WHERE id = $1`, id))
	if err == sql.ErrNoRows {
		return Grant{}, fmt.Errorf("grant %s does not exist", id)
	}
	return g, err
}

// List returns grants, newest first. Plan grants are left out unless includePlan is set,
// because every plan job has one.
func (b *Broker) List(includePlan bool) ([]Grant, error) {
	query := `SELECT ` + grantColumns + ` FROM grants`
	if !includePlan {
		query += ` WHERE kind != 'plan'`
	}
	rows, err := b.db.Query(query + ` ORDER BY issued_at DESC LIMIT 500`)
	if err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	defer rows.Close()
	grants := []Grant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

func (b *Broker) setStatus(id, status string, ended bool) error {
	endedAt := ""
	if ended {
		endedAt = store.FormatTime(b.clock.Now())
	}
	_, err := b.db.Exec(`UPDATE grants SET status = $1, ended_at = $2 WHERE id = $3`, status,
		endedAt, id)
	if err != nil {
		return fmt.Errorf("set grant %s to %s: %w", id, status, err)
	}
	return nil
}
