package pack_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
)

// setting returns the deploy field "settings" with one setting that has these fields.
func setting(fields map[string]any) func(map[string]any) {
	return in("deploy", "settings", []any{fields})
}

// number returns a setting of the type number with these bounds and this default.
func number(low, high, value any) map[string]any {
	return map[string]any{"variable": "gateway_replicas", "type": "number", "min": low,
		"max": high, "default": value, "value": "gateway.replicaCount"}
}

// route returns the deploy field "route" with these fields beside a service and a port.
func route(fields map[string]any) func(map[string]any) {
	value := map[string]any{"service": "harbor-ui", "port": 8080}
	for key, field := range fields {
		value[key] = field
	}
	return in("deploy", "route", value)
}

func TestLoadRefusesADeploymentOutsideItsBounds(t *testing.T) {
	cases := map[string]struct {
		change func(map[string]any)
		want   string
	}{
		"a Helm timeout under 60 s": {in("deploy", "timeout_seconds", 59),
			"deploy.timeout_seconds is 59"},
		"a Helm timeout over 1200 s": {in("deploy", "timeout_seconds", 1201),
			"deploy.timeout_seconds is 1201"},
		"a Helm timeout of 0": {in("deploy", "timeout_seconds", 0),
			"deploy.timeout_seconds is 0"},
		"a Helm timeout that is not whole": {in("deploy", "timeout_seconds", 90.5),
			"timeout_seconds"},
		"no route": {func(m map[string]any) {
			delete(m["deploy"].(map[string]any), "route")
		}, "deploy.route is required"},
		"a route with no service": {in("deploy", "route", map[string]any{"port": 80}),
			`deploy.route.service is ""`},
		"a route to a service that is not a name": {route(map[string]any{
			"service": "ui.other-namespace"}), "deploy.route.service"},
		"a route to port 0": {route(map[string]any{"port": 0}), "deploy.route.port is 0"},
		"a route to port 65536": {route(map[string]any{"port": 65536}),
			"deploy.route.port is 65536"},
		"a route with a field that does not exist": {route(map[string]any{"filters": "x"}),
			"unknown field"},
		"a route timeout under 0": {route(map[string]any{"timeout_seconds": -1}),
			"deploy.route.timeout_seconds is -1"},
		"a route timeout over an hour": {route(map[string]any{"timeout_seconds": 3601}),
			"deploy.route.timeout_seconds is 3601"},
		"a wildcard with no host name": {route(map[string]any{"wildcard": true}),
			"wildcard needs deploy.route.hostname"},
		"an empty list of paths": {route(map[string]any{"paths": []any{}}),
			"deploy.route.paths must have 1 to 8"},
		"nine paths": {route(map[string]any{"paths": []any{"/a", "/b", "/c", "/d", "/e", "/f",
			"/g", "/h", "/i"}}), "deploy.route.paths must have 1 to 8"},
		"a path with a step up": {route(map[string]any{"paths": []any{"/a/../admin"}}),
			`the prefix "/a/../admin"`},
		"a path with a query": {route(map[string]any{"paths": []any{"/a?b=1"}}),
			`the prefix "/a?b=1"`},
		"a path that is not a path": {route(map[string]any{"paths": []any{"realms"}}),
			`the prefix "realms"`},
		"a path two times": {route(map[string]any{"paths": []any{"/a", "/a"}}),
			`the prefix "/a"`},
		"an empty list of secrets": {in("deploy", "secrets", []any{}),
			"deploy.secrets must have 1 to 8"},
		"nine secrets": {in("deploy", "secrets", []any{"a", "b", "c", "d", "e", "f", "g", "h",
			"i"}), "deploy.secrets must have 1 to 8"},
		"a secret key that is not a name": {in("deploy", "secrets", []any{"../token"}),
			`the key "../token"`},
		"a secret key two times": {in("deploy", "secrets", []any{"a", "a"}), `the key "a"`},
		"a workloads namespace that is not a bool": {in("deploy", "workloads_namespace", "yes"),
			"workloads_namespace"},
	}
	for name, tc := range cases {
		_, err := pack.Load(packFiles(manifest(t, tc.change)), "p")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want one with %q", name, err, tc.want)
		}
	}
}

func TestLoadRefusesASettingThatIsNotSafe(t *testing.T) {
	boolean := func(fields map[string]any) map[string]any {
		value := map[string]any{"variable": "jobs_enabled", "type": "bool", "default": false,
			"value": "jobservice.enabled"}
		for key, field := range fields {
			value[key] = field
		}
		return value
	}
	named := func(variable, value string) map[string]any {
		fields := number(1, 4, 2)
		fields["variable"], fields["value"] = variable, value
		return fields
	}
	cases := map[string]struct {
		change func(map[string]any)
		want   string
	}{
		"an empty list": {in("deploy", "settings", []any{}), "deploy.settings must have 1 to 16"},
		"a variable that is not a name": {setting(named("Gateway-Replicas", "a")),
			`the setting "Gateway-Replicas" must match`},
		"the version variable": {setting(named("harbor_release", "a")),
			"the setting harbor_release has the name of the version variable"},
		"a variable of the core": {setting(named("env_name", "a")),
			"the setting env_name has the name"},
		"the admin token": {setting(named("admin_token", "adminToken")),
			"the setting admin_token has the name"},
		"a variable two times": {in("deploy", "settings", []any{named("a_count", "a"),
			named("a_count", "b")}), "the setting a_count has the name"},
		"a Helm path with an index": {setting(named("a_count", "pods[0].replicas")),
			`the value "pods[0].replicas"`},
		"a Helm path with a second value": {setting(named("a_count", "a,b=1")),
			`the value "a,b=1"`},
		"a Helm path with an escape": {setting(named("a_count", `a\.b`)), `the value "a\\.b"`},
		"a value of the platform": {setting(named("a_count", "platform.databaseSecret")),
			`the value "platform.databaseSecret"`},
		"the root of the platform values": {setting(named("a_count", "platform")),
			`the value "platform"`},
		"a Helm path inside another": {in("deploy", "settings", []any{named("a_count", "a"),
			named("b_count", "a.replicas")}), "overlaps the value a of another setting"},
		"a Helm path two times": {in("deploy", "settings", []any{named("a_count", "a.b"),
			named("b_count", "a.b")}), "overlaps the value a.b"},
		"a type that is not read": {setting(map[string]any{"variable": "a_name",
			"type": "string", "default": "x", "value": "a"}), `the type "string"`},
		"a number with no max": {setting(number(1, nil, 2)), "needs min and max"},
		"a number with no min": {setting(number(nil, 4, 2)), "needs min and max"},
		"a min over the max":   {setting(number(5, 4, 4)), "min 5 and max 4"},
		"a negative min":       {setting(number(-1, 4, 2)), "min -1 and max 4"},
		"a max over a million": {setting(number(0, 1000001, 2)), "max 1000001"},
		"a min that is not whole": {setting(number(1.5, 4, 2)),
			"not a correct manifest"},
		"a default under the min": {setting(number(1, 4, 0)),
			"must be a whole number from 1 to 4"},
		"a default over the max": {setting(number(1, 4, 5)),
			"must be a whole number from 1 to 4"},
		"a default that is not whole": {setting(number(1, 4, 2.5)),
			"must be a whole number from 1 to 4"},
		"a default that is a text": {setting(number(1, 4, "2")),
			"must be a whole number from 1 to 4"},
		"a number with no default": {setting(map[string]any{"variable": "a_count",
			"type": "number", "min": 1, "max": 4, "value": "a"}), "must be a whole number"},
		"a bool with a min": {setting(boolean(map[string]any{"min": 0})),
			"is a bool and has no min or max"},
		"a bool with a text default": {setting(boolean(map[string]any{"default": "true"})),
			"must be true or false"},
		"a bool with no default": {setting(map[string]any{"variable": "a_on", "type": "bool",
			"value": "a"}), "must be true or false"},
		"a setting with a field that does not exist": {setting(boolean(map[string]any{
			"description": "x"})), "unknown field"},
	}
	for name, tc := range cases {
		_, err := pack.Load(packFiles(manifest(t, tc.change)), "p")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want one with %q", name, err, tc.want)
		}
	}
}

func TestADeploymentLoadsAtItsBounds(t *testing.T) {
	cases := map[string]func(map[string]any){
		"a Helm timeout of 60 s":   in("deploy", "timeout_seconds", 60),
		"a Helm timeout of 1200 s": in("deploy", "timeout_seconds", 1200),
		"a route with no timeout":  route(map[string]any{"timeout_seconds": 0}),
		"a route timeout of an hour": route(map[string]any{"timeout_seconds": 3600,
			"paths": []any{"/realms", "/resources", "/.well-known", "/admin"}}),
		"a route with a wildcard":       route(map[string]any{"hostname": true, "wildcard": true}),
		"a setting with one value":      setting(number(3, 3, 3)),
		"a setting from 0 to a million": setting(number(0, 1000000, 1000000)),
		"no settings, secrets, values or extras": func(m map[string]any) {
			for _, field := range []string{"settings", "secrets", "values", "extras"} {
				delete(m["deploy"].(map[string]any), field)
			}
		},
	}
	for name, change := range cases {
		if _, err := pack.Load(packFiles(manifest(t, change)), "p"); err != nil {
			t.Errorf("%s: the pack does not load: %v", name, err)
		}
	}
}

func TestTheHelmTimeoutIs300SecondsWhenThePackGivesNone(t *testing.T) {
	if got := packtest.Second(t).Deploy.HelmTimeout(); got != 600 {
		t.Errorf("the timeout of the second pack is %d", got)
	}
	raw := manifest(t, func(m map[string]any) {
		delete(m["deploy"].(map[string]any), "timeout_seconds")
	})
	app, err := pack.Load(packFiles(raw), "p")
	if err != nil {
		t.Fatal(err)
	}
	if got := app.Deploy.HelmTimeout(); got != 300 {
		t.Errorf("HelmTimeout() = %d", got)
	}
}

func TestLoadChecksTheDirectoriesOfTheDeployment(t *testing.T) {
	raw := manifest(t, func(map[string]any) {})
	cases := map[string]struct {
		change func(fstest.MapFS)
		want   string
	}{
		"values with no values.yaml": {func(f fstest.MapFS) {
			delete(f, "p/values/values.yaml")
		}, "has no values.yaml"},
		"values for a cloud that does not exist": {func(f fstest.MapFS) {
			f["p/values/values-mars.yaml"] = &fstest.MapFile{Data: []byte("a: 1\n")}
		}, "values/values-mars.yaml is not values.yaml"},
		"values in another format": {func(f fstest.MapFS) {
			f["p/values/values.json"] = &fstest.MapFile{Data: []byte("{}")}
		}, "values/values.json is not values.yaml"},
		"values in a directory below": {func(f fstest.MapFS) {
			f["p/values/aws/values.yaml"] = &fstest.MapFile{Data: []byte("a: 1\n")}
		}, "files only"},
		"a values file over the limit": {func(f fstest.MapFS) {
			f["p/values/values.yaml"] = &fstest.MapFile{Data: make([]byte, 256<<10+1)}
		}, "the limit is"},
		"extras with no Chart.yaml": {func(f fstest.MapFS) {
			delete(f, "p/extras/Chart.yaml")
			f["p/extras/templates/job.yaml"] = &fstest.MapFile{Data: []byte("kind: Job\n")}
		}, `deploy.extras is "extras", which has no Chart.yaml`},
		"a chart with no Chart.yaml": {func(f fstest.MapFS) {
			delete(f, "p/chart/Chart.yaml")
			f["p/chart/values.yaml"] = &fstest.MapFile{Data: []byte("a: 1\n")}
		}, `deploy.chart is "chart", which has no Chart.yaml`},
	}
	for name, tc := range cases {
		files := packFiles(raw)
		tc.change(files)
		_, err := pack.Load(files, "p")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want one with %q", name, err, tc.want)
		}
	}
	files := packFiles(raw)
	for _, cloud := range pack.Clouds {
		files["p/values/values-"+cloud+".yaml"] = &fstest.MapFile{Data: []byte("a: 1\n")}
	}
	if _, err := pack.Load(files, "p"); err != nil {
		t.Errorf("a values file for each cloud: %v", err)
	}
}
