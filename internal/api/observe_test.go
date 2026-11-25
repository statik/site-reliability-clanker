package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/forge"
	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
	"github.com/statik/site-reliability-clanker/internal/store"
	"github.com/statik/site-reliability-clanker/internal/store/storetest"
)

// collected is a server where people sign in, with the environments local and prod. The
// clock of the core is years away from the wall clock, as the simulator can move it.
type collected struct {
	handler  http.Handler
	db       *sql.DB
	monitor  *monitor.Monitor
	wall     time.Time
	signedIn bool
	logs     *bytes.Buffer
	body     []byte
	tokens   map[string]string
}

var virtualNow = time.Date(2031, 3, 1, 8, 0, 0, 0, time.UTC)

func collect(t *testing.T) *collected {
	t.Helper()
	c := &collected{db: storetest.Open(t), wall: time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC),
		logs: &bytes.Buffer{}, tokens: map[string]string{}}
	before := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(c.logs, &slog.HandlerOptions{
		Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(before) })
	app := packtest.Second(t)
	app.Components = slices.Clone(app.Components)
	for i := range app.Components {
		app.Components[i].Workload = "paperboat-" + app.Components[i].Name
	}
	c.monitor = monitor.New(c.db, fixedClock{virtualNow})
	c.monitor.App = app
	c.monitor.Wall = func() time.Time { return c.wall }
	orch := &orchestrator.Orchestrator{DB: c.db, App: app, Envs: map[string]orchestrator.EnvConfig{
		"local": {Name: "local", Cloud: "local"}, "prod": {Name: "prod", Cloud: "aws"}}}
	server := &Server{App: app, Orch: orch, Monitor: c.monitor, Forge: forge.New(c.db),
		Identify: func(*http.Request) (orchestrator.User, error) {
			if !c.signedIn {
				return orchestrator.User{}, ErrNotSignedIn
			}
			return orchestrator.User{Name: "dave", Role: "viewer"}, nil
		},
		SignIn: http.NotFoundHandler()}
	var err error
	if c.handler, err = server.Handler(); err != nil {
		t.Fatal(err)
	}
	if c.body, err = os.ReadFile("../otlp/testdata/k8s-cluster.json"); err != nil {
		t.Fatal(err)
	}
	for _, env := range []string{"local", "prod"} {
		if c.tokens[env], err = monitor.CollectorToken(c.db, env); err != nil {
			t.Fatal(err)
		}
		c.setTargets(t, env)
	}
	return c
}

func (c *collected) setTargets(t *testing.T, env string) {
	t.Helper()
	err := store.SetLayerOutputs(c.db, env, pack.TargetsLayer, map[string]any{
		pack.TargetsOutput: map[string]any{"application": "paperboat"}}, "2026-11-20")
	if err != nil {
		t.Fatal(err)
	}
}

// post sends a body to the path of the collectors with the header Authorization.
func (c *collected) post(authorization string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, CollectorPath, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	answer := httptest.NewRecorder()
	c.handler.ServeHTTP(answer, request)
	return answer
}

// read returns the observation of env as a signed-in person gets it.
func (c *collected) read(t *testing.T, env string) (monitor.CurrentObservation, string) {
	t.Helper()
	c.signedIn = true
	defer func() { c.signedIn = false }()
	answer := ask(c.handler, "GET", "/api/v1/observation?env="+env)
	if answer.Code != http.StatusOK {
		t.Fatalf("GET the observation of %s: %d %s", env, answer.Code, answer.Body)
	}
	var current monitor.CurrentObservation
	if err := json.Unmarshal(answer.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	return current, strings.TrimSpace(answer.Body.String())
}

func states(current monitor.CurrentObservation) string {
	parts := []string{}
	for _, component := range current.Components {
		part := component.Name + "=" + string(component.State)
		for _, instance := range component.Instances {
			part += "/" + string(instance.State)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}

const (
	nothingKnown = "api=unknown web=unknown worker=unknown"
	noToken      = `{"error":"the request has no valid collector token"}`
)

func TestTheCollectorPathRefusesARequestWithNoValidToken(t *testing.T) {
	c := collect(t)
	if err := store.SetKV(c.db, "collector.token.prod", ""); err != nil {
		t.Fatal(err)
	}
	local := c.tokens["local"]
	tests := []struct{ name, authorization string }{
		{"no header", ""},
		{"a wrong token", "Bearer " + strings.Repeat("0", 64)},
		{"a token that is one character short", "Bearer " + local[:len(local)-1]},
		{"a token with one character more", "Bearer " + local + "0"},
		{"the token with another scheme", "Basic " + local},
		{"the token with no scheme", local},
		{"an empty token, with an environment whose token is empty", "Bearer "},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			answer := c.post(test.authorization, c.body)
			got := strings.TrimSpace(answer.Body.String())
			if answer.Code != 401 || got != noToken {
				t.Errorf("answer %d %s, want 401 %s", answer.Code, got, noToken)
			}
		})
	}
	for _, env := range []string{"local", "prod"} {
		if current, _ := c.read(t, env); current.AgeSeconds != -1 {
			t.Errorf("a refused request stored an observation of %s: %+v", env, current)
		}
	}
	// The gate opens this one path only.
	for _, path := range []string{CollectorPath + "/more", "/otlp/v1/traces", "/otlp/"} {
		answer := ask(c.handler, "POST", path)
		if answer.Code != 401 || !strings.Contains(answer.Body.String(), "not signed in") {
			t.Errorf("POST %s: %d %s, want the gate to refuse it", path, answer.Code, answer.Body)
		}
	}
	if answer := ask(c.handler, "GET", "/api/v1/observation?env=local"); answer.Code != 401 {
		t.Errorf("GET the observation with no sign-in: %d %s", answer.Code, answer.Body)
	}
}

func TestTheCollectorPathRefusesABodyItCannotUse(t *testing.T) {
	c := collect(t)
	bearer := "Bearer " + c.tokens["local"]
	large := append(bytes.TrimSuffix(c.body, []byte("}")), ',', '"', 'x', '"', ':', '"')
	large = append(large, bytes.Repeat([]byte("a"), 2<<20)...)
	large = append(large, '"', '}')
	atLimit := append([]byte{}, c.body...)
	atLimit = append(atLimit, bytes.Repeat([]byte(" "), 2<<20-len(c.body))...)
	tests := []struct {
		name   string
		body   []byte
		status int
		want   string
	}{
		{"a body over the limit", large, 413, `{"error":"the body is larger than 2097152 bytes"}`},
		{"text that is not JSON", []byte("metrics"), 400, `{"error":"the body is not an ` +
			`OTLP request for metrics in JSON: a syntax error at byte 1"}`},
		{"JSON of another request", []byte(`{"resourceLogs":[]}`), 400,
			`{"error":"the body has no list resourceMetrics"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			answer := c.post(bearer, test.body)
			if got := strings.TrimSpace(answer.Body.String()); answer.Code != test.status ||
				got != test.want {
				t.Errorf("answer %d %s, want %d %s", answer.Code, got, test.status, test.want)
			}
			if current, _ := c.read(t, "local"); current.AgeSeconds != -1 {
				t.Errorf("the request stored an observation: %+v", current)
			}
		})
	}
	if answer := c.post(bearer, atLimit); answer.Code != 200 {
		t.Errorf("a body of exactly the limit: %d %s", answer.Code, answer.Body)
	}
}

func TestTheCollectorPathWaitsForTheTargets(t *testing.T) {
	c := collect(t)
	if _, err := c.db.Exec(`DELETE FROM layer_outputs WHERE env = 'local'`); err != nil {
		t.Fatal(err)
	}
	answer := c.post("Bearer "+c.tokens["local"], c.body)
	want := `{"error":"the targets of the environment local are not known yet: environment ` +
		`local: the layer 40-platform has no output targets: the layer is not applied, or ` +
		`the environment is from before this output and must start again"}`
	if got := strings.TrimSpace(answer.Body.String()); answer.Code != 503 || got != want {
		t.Errorf("answer %d %s\nwant 503 %s", answer.Code, got, want)
	}
	if current, _ := c.read(t, "local"); current.AgeSeconds != -1 {
		t.Errorf("the request stored an observation: %+v", current)
	}
	c.setTargets(t, "local")
	if answer := c.post("Bearer "+c.tokens["local"], c.body); answer.Code != 200 {
		t.Errorf("with the targets: %d %s", answer.Code, answer.Body)
	}
}

func TestTheCollectorPathStoresTheObservationOfTheEnvironmentOfTheToken(t *testing.T) {
	c := collect(t)
	err := store.SetLayerOutputs(c.db, "prod", c.monitor.App.Layer, map[string]any{
		c.monitor.App.VersionVariable: "1.0.0"}, "2026-11-20")
	if err != nil {
		t.Fatal(err)
	}
	answer := c.post("Bearer "+c.tokens["prod"], c.body)
	if got := strings.TrimSpace(answer.Body.String()); answer.Code != 200 || got != "{}" ||
		answer.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("answer %d %s %v, want 200 {}", answer.Code, got, answer.Header())
	}
	c.wall = c.wall.Add(7 * time.Second)
	prod, raw := c.read(t, "prod")
	t.Logf("GET /api/v1/observation?env=prod answers %s", raw)
	if prod.Env != "prod" || prod.Stale || prod.AgeSeconds != 7 ||
		states(prod) != "api=ok/ok web=ok/ok worker=ok/ok" || !prod.At.Equal(virtualNow) ||
		prod.Version.Wanted != "1.0.0" || prod.Version.Source != "image" {
		t.Errorf("observation of prod = %s", raw)
	}
	if got := prod.Components[1]; got.Wanted != 1 || got.Ready != 1 || len(got.Instances) != 1 ||
		got.Instances[0].ID != "paperboat-web-88d77c669-s8bx8" ||
		got.Instances[0].Version != "1.0.0" {
		t.Errorf("component web = %+v", got)
	}
	local, raw := c.read(t, "local")
	wantLocal := `{"schema":1,"env":"local","at":"2031-03-01T08:00:00Z","application":"harbor",` +
		`"version":{"wanted":"","source":"image"},"components":[` +
		`{"name":"api","runtime":"kubernetes","state":"unknown","wanted":0,"ready":0,` +
		`"instances":[]},{"name":"web","runtime":"kubernetes","state":"unknown","wanted":0,` +
		`"ready":0,"instances":[]},{"name":"worker","runtime":"kubernetes","state":"unknown",` +
		`"wanted":0,"ready":0,"instances":[]}],"checks":[],"volumes":[],` +
		`"age_seconds":-1,"stale":true}`
	if raw != wantLocal || states(local) != nothingKnown {
		t.Errorf("observation of local:\n got %s\nwant %s", raw, wantLocal)
	}
	// A wanted version that is not a version stays out of the record.
	err = store.SetLayerOutputs(c.db, "prod", c.monitor.App.Layer, map[string]any{
		c.monitor.App.VersionVariable: "latest"}, "2026-11-20")
	if err != nil {
		t.Fatal(err)
	}
	if answer := c.post("Bearer "+c.tokens["prod"], c.body); answer.Code != 200 {
		t.Errorf("with a wanted version that is free text: %d %s", answer.Code, answer.Body)
	}
	if prod, raw := c.read(t, "prod"); prod.Version.Wanted != "" || prod.Stale {
		t.Errorf("observation of prod with a wanted version that is free text = %s", raw)
	}
	c.signedIn = true
	if answer := ask(c.handler, "GET", "/api/v1/observation?env=moon"); answer.Code != 400 ||
		!strings.Contains(answer.Body.String(), `environment \"moon\" is not configured`) {
		t.Errorf("an environment that does not exist: %d %s", answer.Code, answer.Body)
	}
}

func TestAnObservationIsStaleAfterSixtySecondsOfTheWallClock(t *testing.T) {
	c := collect(t)
	if answer := c.post("Bearer "+c.tokens["local"], c.body); answer.Code != 200 {
		t.Fatalf("answer %d %s", answer.Code, answer.Body)
	}
	received := c.wall
	tests := []struct {
		after  time.Duration
		age    int
		stale  bool
		states string
	}{
		{0, 0, false, "api=ok/ok web=ok/ok worker=ok/ok"},
		{60 * time.Second, 60, false, "api=ok/ok web=ok/ok worker=ok/ok"},
		{61 * time.Second, 61, true, "api=unknown/unknown web=unknown/unknown " +
			"worker=unknown/unknown"},
		{26 * time.Hour, 93600, true, "api=unknown/unknown web=unknown/unknown " +
			"worker=unknown/unknown"},
	}
	for _, test := range tests {
		c.wall = received.Add(test.after)
		current, raw := c.read(t, "local")
		if current.AgeSeconds != test.age || current.Stale != test.stale ||
			states(current) != test.states {
			t.Errorf("after %s: %s\nwant the age %d, stale %v, states %s", test.after, raw,
				test.age, test.stale, test.states)
		}
		if current.Components[0].Wanted != 1 || !current.At.Equal(virtualNow) {
			t.Errorf("after %s the record lost data: %s", test.after, raw)
		}
	}
	if answer := c.post("Bearer "+c.tokens["local"], c.body); answer.Code != 200 {
		t.Fatalf("answer %d %s", answer.Code, answer.Body)
	}
	if current, raw := c.read(t, "local"); current.Stale || current.AgeSeconds != 0 {
		t.Errorf("after new data: %s", raw)
	}
}

func TestARestartDegradesAComponentUntilTheNextObservation(t *testing.T) {
	c := collect(t)
	bearer := "Bearer " + c.tokens["local"]
	old := []byte(`"asInt":"0"`)
	at := bytes.Index(c.body, []byte(`"name":"k8s.container.restarts"`))
	value := at + bytes.Index(c.body[at:], old)
	restarted := slices.Concat(c.body[:value], []byte(`"asInt":"3"`), c.body[value+len(old):])
	want := []string{"api=ok/ok web=ok/ok worker=ok/ok", "api=ok/ok web=degraded/ok worker=ok/ok",
		"api=ok/ok web=ok/ok worker=ok/ok"}
	for i, body := range [][]byte{c.body, restarted, restarted} {
		if answer := c.post(bearer, body); answer.Code != 200 {
			t.Fatalf("request %d: %d %s", i, answer.Code, answer.Body)
		}
		current, raw := c.read(t, "local")
		if states(current) != want[i] {
			t.Errorf("after request %d: %s, want %s\n%s", i, states(current), want[i], raw)
		}
		if i == 1 && current.Components[1].Instances[0].Restarts != 3 {
			t.Errorf("restarts of the instance: %s", raw)
		}
	}
}

func TestNoAnswerAndNoLogLineHasTheCollectorToken(t *testing.T) {
	c := collect(t)
	token := c.tokens["local"]
	answers := &bytes.Buffer{}
	for _, answer := range []*httptest.ResponseRecorder{
		c.post("Bearer "+token, c.body), c.post("Bearer "+token, []byte("{")),
		c.post("Bearer "+token+"0", c.body), c.post("Basic "+token, c.body),
		c.post("Bearer "+token, []byte(`{"resourceMetrics":"`+token+`"}`))} {
		answers.WriteString(answer.Body.String())
		for name, values := range answer.Header() {
			answers.WriteString(name + strings.Join(values, ""))
		}
	}
	_, raw := c.read(t, "local")
	answers.WriteString(raw)
	for _, secret := range c.tokens {
		if strings.Contains(answers.String(), secret) {
			t.Errorf("an answer has a collector token: %s", answers)
		}
		if strings.Contains(c.logs.String(), secret) {
			t.Errorf("the log has a collector token: %s", c.logs)
		}
	}
	if strings.Contains(c.logs.String(), "k8s.") {
		t.Errorf("the log has text of a body: %s", c.logs)
	}
	if !strings.Contains(answers.String(), "the field resourceMetrics has the JSON type string") {
		t.Errorf("the answers have no error of the body with the token: %s", answers)
	}
}

func TestCollectorTokenIsMadeOnceForEachEnvironment(t *testing.T) {
	db := storetest.Open(t)
	if env, found, err := monitor.CollectorEnv(db, []string{"local"}, "x"); err != nil ||
		found || env != "" {
		t.Errorf("before a token exists: %q %v %v", env, found, err)
	}
	if stored, _ := store.GetKV(db, "collector.token.local"); stored != "" {
		t.Errorf("the check of a token made the token %q", stored)
	}
	first, err := monitor.CollectorToken(db, "local")
	if err != nil {
		t.Fatal(err)
	}
	second, err := monitor.CollectorToken(db, "local")
	if err != nil {
		t.Fatal(err)
	}
	other, err := monitor.CollectorToken(db, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 || strings.Trim(first, "0123456789abcdef") != "" || first != second ||
		first == other {
		t.Errorf("tokens %q, %q and %q", first, second, other)
	}
	if stored, _ := store.GetKV(db, "collector.token.local"); stored != first {
		t.Errorf("kv has %q, want %q", stored, first)
	}
	for presented, want := range map[string]string{first: "local", other: "prod", "": "",
		first + other: "", first[:63]: ""} {
		env, found, err := monitor.CollectorEnv(db, []string{"local", "prod"}, presented)
		if err != nil || env != want || found != (want != "") {
			t.Errorf("CollectorEnv(%q) = %q %v %v, want %q", presented, env, found, err, want)
		}
	}
	if env, found, _ := monitor.CollectorEnv(db, []string{"local"}, other); found {
		t.Errorf("the token of an environment that is not configured gives %q", env)
	}
}

func TestTheTopologyTakesTheStatusOfAComponentFromTheObservation(t *testing.T) {
	c := collect(t)
	app := c.monitor.App
	layers := map[string]map[string]any{"30-data": {}, app.Layer: {}}
	at := bytes.Index(c.body, []byte(`"name":"k8s.deployment.desired"`))
	old := []byte(`"asInt":"1"`)
	value := at + bytes.Index(c.body[at:], old)
	degraded := slices.Concat(c.body[:value], []byte(`"asInt":"4"`), c.body[value+len(old):])
	if answer := c.post("Bearer "+c.tokens["local"], degraded); answer.Code != 200 {
		t.Fatalf("answer %d %s", answer.Code, answer.Body)
	}
	show := func(asOf string, unhealthy, incident bool) string {
		nodes := topologyNodes(monitor.Snapshot{Layers: layers}, app)
		server := &Server{App: app, Monitor: c.monitor}
		if err := server.markHealth(nodes, "local", asOf, unhealthy, incident); err != nil {
			t.Fatal(err)
		}
		parts := []string{}
		for _, node := range nodes {
			if _, ok := app.Component(node.ID); ok {
				parts = append(parts, node.ID+"="+node.Status+" ("+node.Detail+")"+
					strings.Join(node.Badges, ","))
				continue
			}
			if node.Status != "ok" || len(node.Badges) != 0 ||
				strings.HasSuffix(node.Detail, "ready") {
				t.Errorf("a node that is not a component changed: %+v", node)
			}
		}
		return strings.Join(parts, " ")
	}
	const (
		observed = "api=ok (1 of 1 ready) web=ok (1 of 1 ready) worker=degraded (1 of 4 ready)"
		badged   = "api=ok (1 of 1 ready)incident web=ok (1 of 1 ready)incident " +
			"worker=degraded (1 of 4 ready)incident"
		before = "api=ok (Harbor api) web=ok (Harbor web) worker=ok (Harbor worker)"
		marked = "api=degraded (Harbor api) web=degraded (Harbor web) " +
			"worker=degraded (Harbor worker)"
		alarmed = "api=degraded (Harbor api)incident web=degraded (Harbor web)incident " +
			"worker=degraded (Harbor worker)incident"
	)
	tests := []struct {
		name                string
		age                 time.Duration
		asOf                string
		unhealthy, incident bool
		want                string
	}{
		{"a new observation", 0, "", false, false, observed},
		{"a new observation with a low health number", 0, "", true, false, observed},
		{"a new observation with an incident", 0, "", false, true, badged},
		{"a view of the past", 0, "2026-11-01T00:00:00.000Z", true, false, marked},
		{"a stale observation", 2 * time.Minute, "", false, false, before},
		{"a stale observation with a low health number", 2 * time.Minute, "", true, false, marked},
		{"a stale observation with an incident", 2 * time.Minute, "", false, true, alarmed},
	}
	received := c.wall
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c.wall = received.Add(test.age)
			if got := show(test.asOf, test.unhealthy, test.incident); got != test.want {
				t.Errorf("nodes:\n got %s\nwant %s", got, test.want)
			}
		})
	}
	t.Run("no observation", func(t *testing.T) {
		if _, err := c.db.Exec(`DELETE FROM observations`); err != nil {
			t.Fatal(err)
		}
		c.wall = received
		if got := show("", true, false); got != marked {
			t.Errorf("nodes:\n got %s\nwant %s", got, marked)
		}
	})
}
