// Package observation holds the one record that the core reduces the data of a collector
// and of its own checks to, and the rules that map that data to a state. The detectors,
// the Investigator and the eval cases read the record. Section B.6 of docs/PACKS.md
// defines it.
//
// The package starts no program and reads no file.
package observation

import "time"

// Schema is the version of the record format that this code writes and reads.
const Schema = 1

// State is the state of a component, an instance or a check.
type State string

// The four states. StateUnknown is a real value: a reader must not take it for StateOK.
const (
	StateOK       State = "ok"
	StateDegraded State = "degraded"
	StateDown     State = "down"
	StateUnknown  State = "unknown"
)

// Observation is the newest data about one application in one environment.
type Observation struct {
	Schema      int         `json:"schema"`
	Env         string      `json:"env"`
	At          time.Time   `json:"at"`
	Application string      `json:"application"`
	Version     Version     `json:"version"`
	Components  []Component `json:"components"`
	Checks      []Check     `json:"checks"`
	Volumes     []Volume    `json:"volumes"`
	// Logs and Events are only in the observation of a debug session.
	Logs   []Log   `json:"logs,omitempty"`
	Events []Event `json:"events,omitempty"`
}

// Version is the version of the application.
type Version struct {
	Wanted string `json:"wanted"`
	// Running is set only when the source gives one version for the application. The
	// instances then have no version.
	Running string `json:"running,omitempty"`
	// Source is the version source of the pack.
	Source string `json:"source"`
}

// Component is the state of one component of the pack.
type Component struct {
	Name      string     `json:"name"`
	Runtime   string     `json:"runtime"`
	State     State      `json:"state"`
	Instances []Instance `json:"instances"`
}

// Instance is one pod or one host of a component.
type Instance struct {
	ID      string `json:"id"`
	State   State  `json:"state"`
	Version string `json:"version,omitempty"`
	// Restarts is the number of restarts since the last observation.
	Restarts int `json:"restarts"`
	// Started is the time at which the main process of the instance started last.
	Started *time.Time `json:"started,omitempty"`
}

// Check is the result of one check of the pack in one sample period.
type Check struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Primary bool   `json:"primary"`
	// RanFrom is "core", "site" or "host".
	RanFrom string `json:"ran_from"`
	State   State  `json:"state"`
	OK      int    `json:"ok"`
	Total   int    `json:"total"`
	P95Ms   int    `json:"p95_ms"`
}

// Volume is the fill of one volume on one instance, in bytes.
type Volume struct {
	Name     string `json:"name"`
	Instance string `json:"instance"`
	Used     int64  `json:"used"`
	Total    int64  `json:"total"`
}

// Log is one log line of a debug session. Text holds text of the application: it is not
// trusted.
type Log struct {
	Component string `json:"component"`
	Instance  string `json:"instance"`
	Text      string `json:"text"`
}

// Event is one event of a debug session. Text holds text of the application: it is not
// trusted.
type Event struct {
	At        time.Time `json:"at"`
	Component string    `json:"component"`
	Text      string    `json:"text"`
}
