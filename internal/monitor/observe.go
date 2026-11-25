package monitor

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/statik/site-reliability-clanker/internal/observation"
	"github.com/statik/site-reliability-clanker/internal/otlp"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/store"
)

// StaleAfter is the age at which the data of a collector says nothing about now.
const StaleAfter = 60 * time.Second

// storedObservation is the content of the column data of the table observations.
type storedObservation struct {
	// Received is the wall-clock time at which the data arrived. The age comes from it,
	// because the simulator can move the clock that stamps the record.
	Received    time.Time               `json:"received"`
	Observation observation.Observation `json:"observation"`
	// Restarts has, for each instance, the sum of the restart counts that the collector
	// sent. The next observation takes the difference.
	Restarts map[string]int `json:"restarts"`
}

// CurrentObservation is the newest record of an environment with its age.
type CurrentObservation struct {
	observation.Observation
	// AgeSeconds is the number of wall-clock seconds since the data arrived, or -1 when
	// no data arrived yet.
	AgeSeconds int `json:"age_seconds"`
	// Stale is true when no data arrived or the data is older than StaleAfter. Each state
	// in the record is then StateUnknown.
	Stale bool `json:"stale"`
}

func (m *Monitor) wallNow() time.Time {
	if m.Wall != nil {
		return m.Wall()
	}
	return time.Now()
}

func (m *Monitor) storedObservation(env string) (storedObservation, bool, error) {
	var data string
	err := m.DB.QueryRow(`SELECT data FROM observations WHERE env = $1`, env).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return storedObservation{}, false, nil
	}
	if err != nil {
		return storedObservation{}, false, fmt.Errorf("read the observation of %s: %w", env, err)
	}
	var stored storedObservation
	if err := json.Unmarshal([]byte(data), &stored); err != nil {
		return storedObservation{}, false, fmt.Errorf("decode the observation of %s: %w", env,
			err)
	}
	return stored, true, nil
}

// wantedVersion returns the version that the layer of the application declares in env,
// or "" when the layer is not applied or the value is not a version.
func (m *Monitor) wantedVersion(env string) (string, error) {
	outputs, err := store.LayerOutputs(m.DB, env, m.App.Layer)
	if err != nil {
		return "", err
	}
	version, _ := outputs[m.App.VersionVariable].(string)
	if !observation.IsVersion(version) {
		return "", nil
	}
	return version, nil
}

// RecordObservation reduces the points of one request of a collector to the record of
// env and keeps it in the place of the record before it. The record has the time of the
// clock of the core, not a time of the data.
//
// Args:
//
//	env: the environment that the token of the collector belongs to.
//	targets: where the components run on env.
//	points: the points of the request.
//
// Returns:
//
//	The record that is now the newest of env.
func (m *Monitor) RecordObservation(env string, targets pack.Targets,
	points []otlp.Point) (observation.Observation, error) {
	previous, _, err := m.storedObservation(env)
	if err != nil {
		return observation.Observation{}, err
	}
	wanted, err := m.wantedVersion(env)
	if err != nil {
		return observation.Observation{}, err
	}
	record, restarts := observation.FromKubernetes(observation.KubernetesData{App: m.App,
		Targets: targets, Env: env, At: m.Clock.Now().UTC(), WantedVersion: wanted,
		Points: points, PreviousRestarts: previous.Restarts})
	if err := record.Validate(m.App); err != nil {
		return observation.Observation{}, fmt.Errorf("the observation of %s: %w", env, err)
	}
	data, err := json.Marshal(storedObservation{Received: m.wallNow().UTC(),
		Observation: record, Restarts: restarts})
	if err != nil {
		return observation.Observation{}, fmt.Errorf("encode the observation of %s: %w", env,
			err)
	}
	_, err = m.DB.Exec(`INSERT INTO observations (env, at, data) VALUES ($1,$2,$3)
		ON CONFLICT (env) DO UPDATE SET at = excluded.at, data = excluded.data`, env,
		store.FormatTime(record.At), string(data))
	if err != nil {
		return observation.Observation{}, fmt.Errorf("store the observation of %s: %w", env,
			err)
	}
	return record, nil
}

// Observation returns the newest record of env with its age. Silence is not StateOK: when
// the record is older than StaleAfter, each state in the answer is StateUnknown. With no
// record, the answer has each component of the pack in the state StateUnknown.
func (m *Monitor) Observation(env string) (CurrentObservation, error) {
	stored, found, err := m.storedObservation(env)
	if err != nil {
		return CurrentObservation{}, err
	}
	if !found {
		record, _ := observation.FromKubernetes(observation.KubernetesData{App: m.App,
			Env: env, At: m.Clock.Now().UTC()})
		return CurrentObservation{Observation: record, AgeSeconds: -1, Stale: true}, nil
	}
	age := m.wallNow().Sub(stored.Received)
	current := CurrentObservation{Observation: stored.Observation,
		AgeSeconds: max(int(age/time.Second), 0), Stale: age > StaleAfter}
	if current.Stale {
		current.Observation = stored.Observation.AllUnknown()
	}
	return current, nil
}

func collectorTokenKey(env string) string { return "collector.token." + env }

// CollectorToken returns the secret with which the collector of env sends its data. The
// first call for an environment makes the secret.
func CollectorToken(db *sql.DB, env string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("make the collector token of %s: %w", env, err)
	}
	// Two callers can ask at the same moment. The first row wins, and both read it.
	_, err := db.Exec(`INSERT INTO kv(key, value) VALUES($1, $2) ON CONFLICT(key) DO NOTHING`,
		collectorTokenKey(env), hex.EncodeToString(raw))
	if err != nil {
		return "", fmt.Errorf("store the collector token of %s: %w", env, err)
	}
	token, err := store.GetKV(db, collectorTokenKey(env))
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", fmt.Errorf("the collector token of %s is empty in the database", env)
	}
	return token, nil
}

// CollectorEnv returns the environment whose collector token is presented. It makes no
// token, and it compares the token of each environment in constant time, also after a
// match.
//
// Args:
//
//	db: the database of the control plane.
//	envs: the environments of the control plane.
//	presented: the bearer token of a request.
//
// Returns:
//
//	The environment and true, or "" and false when no environment has this token.
func CollectorEnv(db *sql.DB, envs []string, presented string) (string, bool, error) {
	if presented == "" {
		return "", false, nil
	}
	match := ""
	for _, env := range envs {
		token, err := store.GetKV(db, collectorTokenKey(env))
		if err != nil {
			return "", false, err
		}
		if token == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(presented)) == 1 {
			match = env
		}
	}
	return match, match != "", nil
}
