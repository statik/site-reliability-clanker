// Package sink is a local stand-in for Slack, Microsoft Teams and Discord. It accepts the
// same webhook payloads and shows what arrived, so the notifier adapters run end to end
// without real channels.
package sink

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"sync"
	"time"
)

// Received is one webhook delivery.
type Received struct {
	Channel string          `json:"channel"`
	At      string          `json:"at"`
	Payload json.RawMessage `json:"payload"`
}

// Sink stores webhook deliveries in memory.
type Sink struct {
	mu       sync.Mutex
	received []Received
}

var page = template.Must(template.New("sink").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>S.R.C. webhook sink</title>
<meta http-equiv="refresh" content="5">
<style>
body{font-family:'IBM Plex Sans',Helvetica,Arial,sans-serif;background:#f4f4f4;color:#161616;margin:0;padding:32px}
h1{font-weight:300;margin:0 0 4px} p{color:#525252;margin:0 0 24px}
.msg{background:#fff;border-left:4px solid #0f62fe;padding:16px;margin-bottom:16px}
.slack{border-color:#4a154b}.teams{border-color:#4b53bc}.discord{border-color:#5865f2}
.meta{font-size:12px;color:#525252;text-transform:uppercase;letter-spacing:.32px;margin-bottom:8px}
pre{font-family:'IBM Plex Mono',monospace;font-size:12px;white-space:pre-wrap;word-break:break-word;margin:0;max-height:320px;overflow:auto}
</style></head><body>
<h1>Webhook sink</h1>
<p>Local stand-in for Slack, Microsoft Teams and Discord. {{len .}} payloads received, newest first.</p>
{{range .}}<div class="msg {{.Channel}}"><div class="meta">{{.Channel}} · {{.At}}</div><pre>{{printf "%s" .Payload}}</pre></div>{{end}}
</body></html>`))

func (s *Sink) list() []Received {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Received, len(s.received))
	for i, r := range s.received {
		out[len(out)-1-i] = r
	}
	return out
}

// Handler returns the routes of the sink: POST /{channel}, GET / and GET /received.
func (s *Sink) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /{channel}", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil || !json.Valid(body) {
			http.Error(w, "the body must be JSON", http.StatusBadRequest)
			return
		}
		var pretty json.RawMessage
		if indented, err := json.MarshalIndent(json.RawMessage(body), "", "  "); err == nil {
			pretty = indented
		}
		s.mu.Lock()
		s.received = append(s.received, Received{Channel: r.PathValue("channel"),
			At: time.Now().UTC().Format(time.RFC3339), Payload: pretty})
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("GET /received", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(s.list()); err != nil {
			return
		}
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := page.Execute(w, s.list()); err != nil {
			return
		}
	})
	return mux
}
