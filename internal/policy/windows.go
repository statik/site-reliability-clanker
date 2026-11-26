package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	// The control plane also runs where the system has no time zone files.
	_ "time/tzdata"
)

// WindowsPath is the path of the maintenance windows in the runbooks repository.
const WindowsPath = "policy/maintenance-windows.json"

// WindowReserve is the time that must be left in a maintenance window when an apply
// starts. On the local target it is also the lifetime of the apply grant of one layer. On
// AWS that grant lasts one hour, so the apply of one layer can end up to 30 minutes after
// the window. An apply of several layers can end after the window.
const WindowReserve = 30 * time.Minute

// A window is longer than the reserve: the scheduler starts a change some seconds after
// the start of the window, and the reserve must still be left then.
const (
	minWindowMinutes = 60
	maxWindowMinutes = 24 * 60
)

const windowsFormat = `the file must be {"windows": [{"days": ["sat"], "start": "02:00", ` +
	`"minutes": 120, "time_zone": "Europe/Dublin"}]}; days are mon to sun, start is HH:MM ` +
	`on a 24-hour clock, minutes is 60 to 1440, and time_zone is an IANA name`

var weekdays = map[string]time.Weekday{"sun": time.Sunday, "mon": time.Monday,
	"tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday,
	"sat": time.Saturday}

// window is one weekly maintenance window.
type window struct {
	days     []time.Weekday
	hour     int
	minute   int
	length   time.Duration
	location *time.Location
}

// Windows are the maintenance windows of the runbooks repository. The zero value is the
// setting of a repository that has no windows file: a change does not wait for a window.
type Windows struct {
	list []window
}

// ParseWindows reads the content of the windows file. It accepts exactly one JSON object
// with the one key windows. Each window has exactly the keys days, start, minutes and
// time_zone. Everything else is an error.
func ParseWindows(source []byte) (Windows, error) {
	windows, err := parseWindows(source)
	if err != nil {
		return Windows{}, fmt.Errorf("%s is not valid (%w): %s", WindowsPath, err,
			windowsFormat)
	}
	return windows, nil
}

func parseWindows(source []byte) (Windows, error) {
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()
	value, err := strictValue(decoder)
	if err != nil {
		return Windows{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Windows{}, errors.New("there is data after the JSON object")
	}
	object, ok := value.(map[string]any)
	if !ok || !hasKeys(object, "windows") {
		return Windows{}, errors.New("it is not one JSON object with the one key windows")
	}
	entries, ok := object["windows"].([]any)
	if !ok {
		return Windows{}, errors.New("the value of windows is not a list")
	}
	windows := Windows{}
	for i, entry := range entries {
		w, err := parseWindow(entry)
		if err != nil {
			return Windows{}, fmt.Errorf("window %d: %w", i+1, err)
		}
		windows.list = append(windows.list, w)
	}
	return windows, nil
}

// strictValue reads one JSON value token by token, because the decoder of a struct
// ignores the case of a key and accepts a key that comes twice.
func strictValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		return strictObject(decoder)
	case json.Delim('['):
		list := []any{}
		for decoder.More() {
			item, err := strictValue(decoder)
			if err != nil {
				return nil, err
			}
			list = append(list, item)
		}
		_, err := decoder.Token()
		return list, err
	default:
		return token, nil
	}
}

func strictObject(decoder *json.Decoder) (map[string]any, error) {
	object := map[string]any{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, _ := token.(string)
		if _, twice := object[key]; twice {
			return nil, fmt.Errorf("the key %q comes twice", key)
		}
		if object[key], err = strictValue(decoder); err != nil {
			return nil, err
		}
	}
	_, err := decoder.Token()
	return object, err
}

// hasKeys reports whether the object has exactly these keys.
func hasKeys(object map[string]any, keys ...string) bool {
	if len(object) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, found := object[key]; !found {
			return false
		}
	}
	return true
}

func parseWindow(entry any) (window, error) {
	object, ok := entry.(map[string]any)
	if !ok || !hasKeys(object, "days", "start", "minutes", "time_zone") {
		return window{}, errors.New("it is not an object with exactly the keys days, start, " +
			"minutes and time_zone")
	}
	days, err := parseDays(object["days"])
	if err != nil {
		return window{}, err
	}
	text, _ := object["start"].(string)
	start, err := time.Parse("15:04", text)
	if err != nil || len(text) != len("15:04") {
		return window{}, fmt.Errorf("start %v is not a time HH:MM", object["start"])
	}
	length, err := parseLength(object["minutes"])
	if err != nil {
		return window{}, err
	}
	location, err := parseZone(object["time_zone"])
	if err != nil {
		return window{}, err
	}
	return window{days: days, hour: start.Hour(), minute: start.Minute(), length: length,
		location: location}, nil
}

func parseLength(value any) (time.Duration, error) {
	number, _ := value.(json.Number)
	minutes, err := strconv.Atoi(number.String())
	if err != nil || minutes < minWindowMinutes || minutes > maxWindowMinutes {
		return 0, fmt.Errorf("minutes %v is not a whole number from %d to %d", value,
			minWindowMinutes, maxWindowMinutes)
	}
	return time.Duration(minutes) * time.Minute, nil
}

func parseDays(value any) ([]time.Weekday, error) {
	names, ok := value.([]any)
	if !ok || len(names) == 0 {
		return nil, errors.New("days is not a list with one day or more")
	}
	days := []time.Weekday{}
	for _, name := range names {
		text, _ := name.(string)
		day, known := weekdays[text]
		if !known {
			return nil, fmt.Errorf("day %v is not one of mon, tue, wed, thu, fri, sat, sun", name)
		}
		if slices.Contains(days, day) {
			return nil, fmt.Errorf("day %s comes twice", text)
		}
		days = append(days, day)
	}
	return days, nil
}

// parseZone refuses the names that mean the zone of the computer: the windows must be
// the same on each computer that reads the file.
func parseZone(value any) (*time.Location, error) {
	name, _ := value.(string)
	if name == "" || name == "Local" {
		return nil, fmt.Errorf("time_zone %v is not an IANA name", value)
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("time_zone %q is not an IANA name", name)
	}
	return location, nil
}

// Empty reports whether no maintenance window is set.
func (w Windows) Empty() bool { return len(w.list) == 0 }

// Next returns the first moment at or after from when an apply can start, and the start
// of the window that this moment is in. An apply can start inside a window while
// WindowReserve or more of the window is left. Without windows, the moment is from and
// the start is the zero time.
func (w Windows) Next(from time.Time) (at, windowStart time.Time) {
	if w.Empty() {
		return from, time.Time{}
	}
	for _, one := range w.list {
		candidate, start := one.next(from)
		if at.IsZero() || candidate.Before(at) {
			at, windowStart = candidate, start
		}
	}
	return at, windowStart
}

// next looks at the window of each calendar day from the day before from to the same day
// of the next week. The day before is there for a window that crosses midnight. The
// start is built from the calendar day in the zone of the window, because a day with a
// change of the clock does not have 24 hours.
func (w window) next(from time.Time) (at, windowStart time.Time) {
	local := from.In(w.location)
	for offset := -1; offset <= 7; offset++ {
		start := time.Date(local.Year(), local.Month(), local.Day()+offset, w.hour, w.minute,
			0, 0, w.location)
		if !slices.Contains(w.days, start.Weekday()) {
			continue
		}
		if start.After(from) {
			return start, start
		}
		if start.Add(w.length).Sub(from) >= WindowReserve {
			return from, start
		}
	}
	// Each weekday is in the nine days above, and a window is longer than the reserve, so
	// this line is not reached with a parsed window.
	return time.Time{}, time.Time{}
}
