package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/statik/site-reliability-clanker/internal/agent"
	"github.com/statik/site-reliability-clanker/internal/audit"
	"github.com/statik/site-reliability-clanker/internal/ollama"
)

// modelSettings is what a person sees of the language model. It never holds a key or a
// part of a key.
type modelSettings struct {
	Mode     string `json:"mode"`
	Provider string `json:"provider"`
	// Models is the model of each role. Empty when no model is called.
	Models map[string]string `json:"models"`
	// BaseURL is the endpoint of the mode local.
	BaseURL string       `json:"base_url,omitempty"`
	KeySet  bool         `json:"key_set"`
	Local   ollama.State `json:"local"`
	// Done says what a start or a stop did.
	Done string `json:"done,omitempty"`
}

// publicURL returns an address without the parts that can hold a secret: the user, the
// password and the query.
func publicURL(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	parsed.User, parsed.RawQuery, parsed.Fragment = nil, "", ""
	return parsed.String()
}

func (s *Server) modelSettings(ctx context.Context) (modelSettings, error) {
	local, err := s.LocalModel.State(ctx)
	if err != nil {
		return modelSettings{}, err
	}
	settings := modelSettings{Mode: s.Agent.LLM.Mode(), Models: map[string]string{},
		Local: local}
	chat, isChat := s.Agent.LLM.(*agent.Chat)
	if isChat {
		settings.Models, settings.KeySet = s.Agent.Models, chat.APIKey != ""
	}
	switch settings.Mode {
	case "openrouter":
		settings.Provider = "OpenRouter"
	case "replay":
		settings.Provider = "Recorded answers"
	case "local":
		settings.Provider = "A model endpoint of the operator"
		if local.Available {
			settings.Provider = "Ollama on this machine"
		}
		if isChat {
			settings.BaseURL = publicURL(chat.BaseURL)
		}
	default:
		settings.Provider = settings.Mode
	}
	return settings, nil
}

func (s *Server) getModelSettings(r *http.Request) (any, error) {
	return s.modelSettings(r.Context())
}

// changeLocalModel starts or stops the local inference for an admin, and records it.
func (s *Server) changeLocalModel(r *http.Request, event, verb string,
	act func(context.Context) (string, error)) (any, error) {
	user, err := s.requireRole(r, "admin")
	if err != nil {
		return nil, err
	}
	done, err := act(r.Context())
	var refusal ollama.Refusal
	if errors.As(err, &refusal) {
		return nil, statusError{http.StatusConflict, err}
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.Audit.Append(audit.Event{Type: event, Actor: user.Name,
		Summary: user.Display + " " + verb + " the local inference: " + done,
		Data: map[string]any{"model": s.LocalModel.Model, "url": s.LocalModel.Client.Base,
			"done": done}}); err != nil {
		return nil, err
	}
	settings, err := s.modelSettings(r.Context())
	if err != nil {
		return nil, err
	}
	settings.Done = done
	return settings, nil
}

func (s *Server) postLocalModelStart(r *http.Request) (any, error) {
	return s.changeLocalModel(r, "model.local_started", "started", s.LocalModel.Start)
}

func (s *Server) postLocalModelStop(r *http.Request) (any, error) {
	return s.changeLocalModel(r, "model.local_stopped", "stopped", s.LocalModel.Stop)
}
