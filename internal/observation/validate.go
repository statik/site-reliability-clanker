package observation

import (
	"fmt"
	"regexp"

	"github.com/statik/site-reliability-clanker/internal/pack"
)

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[a-z][a-z0-9]*)?$`)

// Validate checks a record that arrives from outside the core against the pack of its
// application. It refuses a component or a check that the pack does not have, a state
// that is not one of the four, a version that does not match the pattern of a version, a
// negative count, a check with more passes than results, and a primary flag that differs
// from the pack.
//
// Args:
//
//	app: the pack of the application that the record is about.
//
// Returns:
//
//	An error that names the first faulty entry, or nil.
func (o Observation) Validate(app pack.Pack) error {
	if err := validVersion("version.wanted", o.Version.Wanted); err != nil {
		return err
	}
	if err := validVersion("version.running", o.Version.Running); err != nil {
		return err
	}
	if err := o.validateComponents(app); err != nil {
		return err
	}
	if err := o.validateChecks(app); err != nil {
		return err
	}
	return o.validateVolumes()
}

func (o Observation) validateComponents(app pack.Pack) error {
	known := map[string]bool{}
	for _, component := range app.Components {
		known[component.Name] = true
	}
	for _, component := range o.Components {
		if !known[component.Name] {
			return fmt.Errorf("the pack %q has no component %q", app.Name, component.Name)
		}
		where := fmt.Sprintf("component %q", component.Name)
		if err := validState(where, component.State); err != nil {
			return err
		}
		for _, instance := range component.Instances {
			if err := validateInstance(where, instance); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateInstance(component string, instance Instance) error {
	where := fmt.Sprintf("%s, instance %q", component, instance.ID)
	if err := validState(where, instance.State); err != nil {
		return err
	}
	if err := validVersion(where, instance.Version); err != nil {
		return err
	}
	return notNegative(where, "restarts", int64(instance.Restarts))
}

func (o Observation) validateChecks(app pack.Pack) error {
	primary := map[string]bool{}
	for _, check := range app.Checks {
		primary[check.Name] = check.Primary
	}
	for _, check := range o.Checks {
		wanted, known := primary[check.Name]
		if !known {
			return fmt.Errorf("the pack %q has no check %q", app.Name, check.Name)
		}
		if check.Primary != wanted {
			return fmt.Errorf("check %q: primary is %t, but the pack says %t",
				check.Name, check.Primary, wanted)
		}
		if err := validateCheck(check); err != nil {
			return err
		}
	}
	return nil
}

func validateCheck(check Check) error {
	where := fmt.Sprintf("check %q", check.Name)
	if err := validState(where, check.State); err != nil {
		return err
	}
	counts := []struct {
		name  string
		value int
	}{{"ok", check.OK}, {"total", check.Total}, {"p95_ms", check.P95Ms}}
	for _, count := range counts {
		if err := notNegative(where, count.name, int64(count.value)); err != nil {
			return err
		}
	}
	if check.OK > check.Total {
		return fmt.Errorf("%s: ok is %d, more than the total %d", where, check.OK, check.Total)
	}
	return nil
}

func (o Observation) validateVolumes() error {
	for _, volume := range o.Volumes {
		where := fmt.Sprintf("volume %q", volume.Name)
		if err := notNegative(where, "used", volume.Used); err != nil {
			return err
		}
		if err := notNegative(where, "total", volume.Total); err != nil {
			return err
		}
	}
	return nil
}

func validState(where string, state State) error {
	switch state {
	case StateOK, StateDegraded, StateDown, StateUnknown:
		return nil
	}
	return fmt.Errorf("%s: the state %q is not ok, degraded, down or unknown", where, state)
}

func validVersion(where, version string) error {
	if version == "" || versionPattern.MatchString(version) {
		return nil
	}
	return fmt.Errorf("%s: %q is not a version", where, version)
}

func notNegative(where, name string, value int64) error {
	if value < 0 {
		return fmt.Errorf("%s: %s is %d, a negative count", where, name, value)
	}
	return nil
}
