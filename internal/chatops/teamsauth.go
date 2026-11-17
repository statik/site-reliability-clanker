package chatops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// The Bot Connector service of Microsoft signs each request to a bot with a JSON Web
// Token. This file verifies that token as the Microsoft documentation lists it in "Step 4:
// Verify the JWT token" of the page on authentication with the Bot Connector API. The
// address of that page is at the top of teams.go.

const (
	// connectorMetadata is the OpenID metadata of the Bot Connector. The documentation says
	// that it is a static address.
	connectorMetadata = "https://login.botframework.com/v1/.well-known/openidconfiguration"
	connectorIssuer   = "https://api.botframework.com"
	// connectorSkew is the clock difference that the documentation allows.
	connectorSkew = 5 * time.Minute
	// keyLife is how long the keys are kept. The documentation asks for a refresh at least
	// every 24 hours.
	keyLife = 24 * time.Hour
	// keyPause is the shortest time between two reads of the keys, so that tokens with an
	// unknown key do not make the control plane ask Microsoft at each request.
	keyPause = time.Minute
)

// connectorKey is one signing key of the Bot Connector. The endorsements are the channels
// for which Microsoft uses the key.
type connectorKey struct {
	jose.JSONWebKey
	Endorsements []string
}

// connectorKeys reads and keeps the signing keys of the Bot Connector.
type connectorKeys struct {
	metadataURL string
	client      *http.Client
	now         func() time.Time

	mu      sync.Mutex
	keys    map[string]connectorKey
	fetched time.Time
}

func getJSON(ctx context.Context, client *http.Client, address string, into any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("build the request to %s: %w", address, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("read %s: %w", address, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", address, response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(into); err != nil {
		return fmt.Errorf("decode %s: %w", address, err)
	}
	return nil
}

// fetch reads the metadata and then the keys that it names.
func (k *connectorKeys) fetch(ctx context.Context) (map[string]connectorKey, error) {
	var metadata struct {
		KeysURL string `json:"jwks_uri"`
	}
	if err := getJSON(ctx, k.client, k.metadataURL, &metadata); err != nil {
		return nil, err
	}
	var set struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := getJSON(ctx, k.client, metadata.KeysURL, &set); err != nil {
		return nil, err
	}
	keys := map[string]connectorKey{}
	for _, raw := range set.Keys {
		var key connectorKey
		var extra struct {
			Endorsements []string `json:"endorsements"`
		}
		// A key of a kind that this program does not know is left out.
		if json.Unmarshal(raw, &key.JSONWebKey) != nil || json.Unmarshal(raw, &extra) != nil {
			continue
		}
		key.Endorsements = extra.Endorsements
		keys[key.KeyID] = key
	}
	return keys, nil
}

// find returns the key with this ID. It reads the keys again when they are old, or when
// the ID is not known and the last read is not too new.
func (k *connectorKeys) find(ctx context.Context, id string) (connectorKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	age := k.now().Sub(k.fetched)
	_, known := k.keys[id]
	if age > keyLife || (!known && age > keyPause) {
		keys, err := k.fetch(ctx)
		if err != nil {
			return connectorKey{}, err
		}
		k.keys, k.fetched = keys, k.now()
	}
	key, known := k.keys[id]
	if !known {
		return connectorKey{}, errors.New("the token is signed with a key that the Bot " +
			"Connector does not publish")
	}
	return key, nil
}

// claims returns the claims of a token after it checked the signature with a published
// key, and the key that signed it.
func (k *connectorKeys) claims(ctx context.Context, header string) (jwt.Claims, map[string]any,
	connectorKey, error) {
	extra := map[string]any{}
	raw, found := strings.CutPrefix(header, "Bearer ")
	if !found {
		return jwt.Claims{}, nil, connectorKey{}, errors.New("the request has no bearer token")
	}
	token, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil || len(token.Headers) != 1 {
		return jwt.Claims{}, nil, connectorKey{}, errors.New("the token is not a JSON Web " +
			"Token that is signed with RS256")
	}
	key, err := k.find(ctx, token.Headers[0].KeyID)
	if err != nil {
		return jwt.Claims{}, nil, connectorKey{}, err
	}
	var claims jwt.Claims
	if err := token.Claims(key.Key, &claims, &extra); err != nil {
		return jwt.Claims{}, nil, connectorKey{}, errors.New("the signature of the token is " +
			"not valid")
	}
	return claims, extra, key, nil
}

// verify checks the token of a request from the Bot Connector: the signature with a
// published key, the issuer, the audience (the app ID of the bot), the validity period,
// that the key is endorsed for the channel of the activity, and that the serviceUrl claim
// is the serviceUrl of the activity.
func (k *connectorKeys) verify(ctx context.Context, header, appID string, a teamsActivity) error {
	claims, extra, key, err := k.claims(ctx, header)
	if err != nil {
		return err
	}
	if err := claims.ValidateWithLeeway(jwt.Expected{Issuer: connectorIssuer,
		AnyAudience: jwt.Audience{appID}, Time: k.now()}, connectorSkew); err != nil {
		return fmt.Errorf("the token is not valid for this bot: %w", err)
	}
	if claims.Expiry == nil {
		return errors.New("the token has no expiry time")
	}
	if !slices.Contains(key.Endorsements, a.ChannelID) {
		return errors.New("the key of the token is not endorsed for the channel of the activity")
	}
	// The documentation writes the claim as serviceUrl. This program also takes the name in
	// lower case, because the decoder of the token library does not ignore the case.
	serviceURL, _ := extra["serviceUrl"].(string)
	if serviceURL == "" {
		serviceURL, _ = extra["serviceurl"].(string)
	}
	if serviceURL == "" || serviceURL != a.ServiceURL {
		return errors.New("the serviceUrl of the activity is not the serviceUrl of the token")
	}
	return nil
}
