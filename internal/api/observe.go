package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/otlp"
	"github.com/statik/site-reliability-clanker/internal/pack"
)

// CollectorPath is where a collector sends its metrics. No user is signed in there: each
// request carries the token of the collector of one environment.
const CollectorPath = "/otlp/v1/metrics"

// maxCollectorBody is the size limit of one request of a collector.
const maxCollectorBody = 2 << 20

// errNoCollectorToken is the one answer to a request with no token and to a request with
// a token that no environment has.
var errNoCollectorToken = statusError{http.StatusUnauthorized,
	errors.New("the request has no valid collector token")}

// collectorEnv returns the environment whose collector sent the request.
func (s *Server) collectorEnv(r *http.Request) (string, error) {
	presented, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found {
		return "", errNoCollectorToken
	}
	env, known, err := monitor.CollectorEnv(s.Orch.DB, s.envNames(), presented)
	if err != nil {
		return "", err
	}
	if !known {
		return "", errNoCollectorToken
	}
	return env, nil
}

// postMetrics receives the metrics of a collector and keeps the observation that they
// reduce to. It writes no log line: the token and the body stay out of the logs.
func (s *Server) postMetrics(r *http.Request) (any, error) {
	env, err := s.collectorEnv(r)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxCollectorBody))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return nil, statusError{http.StatusRequestEntityTooLarge,
			fmt.Errorf("the body is larger than %d bytes", maxCollectorBody)}
	}
	if err != nil {
		return nil, badRequest("read the body: %v", err)
	}
	points, err := otlp.ParseMetrics(body)
	if err != nil {
		return nil, badRequest("%v", err)
	}
	targets, err := s.Orch.Targets(env)
	if err != nil {
		return nil, statusError{http.StatusServiceUnavailable, fmt.Errorf(
			"the targets of the environment %s are not known yet: %w", env, err)}
	}
	if _, err := s.Monitor.RecordObservation(env, targets, points); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (s *Server) getObservation(r *http.Request) (any, error) {
	env := r.URL.Query().Get("env")
	if _, ok := s.Orch.Envs[env]; !ok {
		return nil, badRequest("environment %q is not configured", env)
	}
	return s.Monitor.Observation(env)
}

// showObservation gives each node that is a component the state and the instance counts
// of the observation, and the badge of an open incident.
func showObservation(nodes []topoNode, app pack.Pack, current monitor.CurrentObservation,
	incident bool) {
	for _, component := range current.Components {
		for i := range nodes {
			if _, ok := app.Component(nodes[i].ID); !ok || nodes[i].ID != component.Name {
				continue
			}
			nodes[i].Status = string(component.State)
			nodes[i].Detail = fmt.Sprintf("%d of %d ready", component.Ready, component.Wanted)
			if incident {
				nodes[i].Badges = append(nodes[i].Badges, "incident")
			}
		}
	}
}

// markHealth sets the status of the nodes that are components. The live view takes it
// from an observation that is not stale. A view of the past, and a live view with no
// such observation, have only the one health number of the application.
func (s *Server) markHealth(nodes []topoNode, env, asOf string, unhealthy, incident bool) error {
	if asOf == "" {
		current, err := s.Monitor.Observation(env)
		if err != nil {
			return err
		}
		if !current.Stale {
			showObservation(nodes, s.App, current, incident)
			return nil
		}
	}
	if unhealthy || incident {
		markComponents(nodes, s.App, incident)
	}
	return nil
}
