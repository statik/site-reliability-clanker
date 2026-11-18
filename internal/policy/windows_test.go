package policy_test

import (
	"strings"
	"testing"
	"time"

	"github.com/statik/site-reliability-clanker/internal/policy"
)

func parsedWindows(t *testing.T, source string) policy.Windows {
	t.Helper()
	windows, err := policy.ParseWindows([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return windows
}

func utc(t *testing.T, text string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestNoWindowsMeansNoWait(t *testing.T) {
	from := utc(t, "2026-11-18T10:00:00Z")
	for _, windows := range []policy.Windows{{}, parsedWindows(t, `{"windows": []}`)} {
		if !windows.Empty() {
			t.Error("windows without an entry must be empty")
		}
		at, start := windows.Next(from)
		if !at.Equal(from) || !start.IsZero() {
			t.Errorf("want the apply at %s and no window, got %s and %s", from, at, start)
		}
	}
}

func TestParseWindowsRefusesWhatIsNotExact(t *testing.T) {
	valid := `{"days": ["sat"], "start": "02:00", "minutes": 120, "time_zone": "UTC"}`
	changed := func(old, text string) string {
		return `{"windows": [` + strings.Replace(valid, old, text, 1) + `]}`
	}
	for name, source := range map[string]string{
		"not JSON":            `windows`,
		"empty file":          ``,
		"a list":              `[]`,
		"other key":           `{"window": []}`,
		"key in upper case":   `{"Windows": []}`,
		"second key":          `{"windows": [], "t3_approvers": 1}`,
		"windows twice":       `{"windows": [], "windows": []}`,
		"windows not a list":  `{"windows": {}}`,
		"data after":          `{"windows": []} {}`,
		"window not object":   `{"windows": ["sat"]}`,
		"missing key":         `{"windows": [{"days": ["sat"], "start": "02:00", "minutes": 60}]}`,
		"unknown key":         changed("}", `, "x": 1}`),
		"key twice":           changed("}", `, "minutes": 60}`),
		"no day":              changed(`"sat"`, ``),
		"long day name":       changed(`sat`, `saturday`),
		"day in upper case":   changed(`sat`, `Sat`),
		"day twice":           changed(`"sat"`, `"sat", "sat"`),
		"day is a number":     changed(`"sat"`, `6`),
		"start without zero":  changed(`02:00`, `2:00`),
		"start hour 24":       changed(`02:00`, `24:00`),
		"start with seconds":  changed(`02:00`, `02:00:00`),
		"minutes too small":   changed(`120`, `29`),
		"minutes too large":   changed(`120`, `1441`),
		"minutes a fraction":  changed(`120`, `60.5`),
		"minutes a text":      changed(`120`, `"60"`),
		"zone not known":      changed(`UTC`, `Mars/Olympus`),
		"zone of the machine": changed(`UTC`, `Local`),
		"zone empty":          changed(`UTC`, ``),
	} {
		_, err := policy.ParseWindows([]byte(source))
		if err == nil {
			t.Errorf("%s: %s was accepted", name, source)
			continue
		}
		if !strings.Contains(err.Error(), policy.WindowsPath) {
			t.Errorf("%s: the error does not name the file: %v", name, err)
		}
	}
}

func TestParseWindowsAcceptsSeveralWindows(t *testing.T) {
	windows := parsedWindows(t, `{"windows": [
		{"days": ["sat", "sun"], "start": "02:00", "minutes": 120, "time_zone": "Europe/Dublin"},
		{"days": ["wed"], "start": "23:30", "minutes": 30, "time_zone": "UTC"}]}`+"\n")
	if windows.Empty() {
		t.Fatal("two windows were parsed to no window")
	}
	// Monday. The window of Wednesday in UTC comes before the window of Saturday.
	at, start := windows.Next(utc(t, "2026-11-16T12:00:00Z"))
	want := utc(t, "2026-11-18T23:30:00Z")
	if !at.Equal(want) || !start.Equal(want) {
		t.Errorf("want %s, got the apply at %s in the window of %s", want, at, start)
	}
}

func TestNextWindow(t *testing.T) {
	saturdayNight := `{"windows": [{"days": ["sat"], "start": "02:00", "minutes": 120,
		"time_zone": "UTC"}]}`
	tokyo := `{"windows": [{"days": ["mon"], "start": "09:00", "minutes": 60,
		"time_zone": "Asia/Tokyo"}]}`
	overMidnight := `{"windows": [{"days": ["fri"], "start": "23:00", "minutes": 180,
		"time_zone": "UTC"}]}`
	newYork := `{"windows": [{"days": ["sat", "sun"], "start": "03:00", "minutes": 60,
		"time_zone": "America/New_York"}]}`
	for _, c := range []struct{ name, windows, from, at, start string }{
		{"before the window", saturdayNight, "2026-11-18T10:00:00Z",
			"2026-11-21T02:00:00Z", "2026-11-21T02:00:00Z"},
		{"at the start", saturdayNight, "2026-11-21T02:00:00Z",
			"2026-11-21T02:00:00Z", "2026-11-21T02:00:00Z"},
		{"inside with time left", saturdayNight, "2026-11-21T02:45:00Z",
			"2026-11-21T02:45:00Z", "2026-11-21T02:00:00Z"},
		{"exactly the reserve is left", saturdayNight, "2026-11-21T03:30:00Z",
			"2026-11-21T03:30:00Z", "2026-11-21T02:00:00Z"},
		{"one minute less than the reserve", saturdayNight, "2026-11-21T03:31:00Z",
			"2026-11-28T02:00:00Z", "2026-11-28T02:00:00Z"},
		{"one minute before the end", saturdayNight, "2026-11-21T03:59:00Z",
			"2026-11-28T02:00:00Z", "2026-11-28T02:00:00Z"},
		{"at the end", saturdayNight, "2026-11-21T04:00:00Z",
			"2026-11-28T02:00:00Z", "2026-11-28T02:00:00Z"},
		// Monday 09:00 in Tokyo is Monday 00:00 UTC, and it is still Sunday in UTC before it.
		{"the day of the zone, not of UTC", tokyo, "2026-11-22T20:00:00Z",
			"2026-11-23T00:00:00Z", "2026-11-23T00:00:00Z"},
		{"after the window of the zone", tokyo, "2026-11-23T01:00:00Z",
			"2026-11-30T00:00:00Z", "2026-11-30T00:00:00Z"},
		{"after midnight in a window of the day before", overMidnight, "2026-11-21T01:00:00Z",
			"2026-11-21T01:00:00Z", "2026-11-20T23:00:00Z"},
		{"after midnight with too little left", overMidnight, "2026-11-21T01:45:00Z",
			"2026-11-27T23:00:00Z", "2026-11-27T23:00:00Z"},
		// The clocks of New York go back on 2026-11-01. 03:00 is 07:00 UTC on the Saturday
		// and 08:00 UTC on the Sunday.
		{"the day before the clocks go back", newYork, "2026-10-31T06:00:00Z",
			"2026-10-31T07:00:00Z", "2026-10-31T07:00:00Z"},
		{"the day the clocks go back", newYork, "2026-10-31T09:00:00Z",
			"2026-11-01T08:00:00Z", "2026-11-01T08:00:00Z"},
		// The clocks go forward on 2027-03-14. 03:00 is 08:00 UTC on the Saturday and
		// 07:00 UTC on the Sunday.
		{"the day the clocks go forward", newYork, "2027-03-13T10:00:00Z",
			"2027-03-14T07:00:00Z", "2027-03-14T07:00:00Z"},
	} {
		at, start := parsedWindows(t, c.windows).Next(utc(t, c.from))
		if !at.Equal(utc(t, c.at)) || !start.Equal(utc(t, c.start)) {
			t.Errorf("%s: from %s want the apply at %s in the window of %s, got %s and %s",
				c.name, c.from, c.at, c.start, at.UTC().Format(time.RFC3339),
				start.UTC().Format(time.RFC3339))
		}
	}
}
