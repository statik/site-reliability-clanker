// Package buildinfo holds the values that the build sets with -ldflags -X.
// One code base produces every release; these values select the behaviour.
package buildinfo

import (
	"fmt"
	"strconv"
)

// Set at build time. The defaults describe a development build of the newest feature level.
var (
	Version      = "dev"
	BaseImage    = "none"
	FeatureLevel = "2"
	Fault        = "none"
)

// FaultInventory is the Fault value that makes the inventory list unreliable.
const FaultInventory = "inventory"

// Info is the parsed build configuration.
type Info struct {
	Version   string
	BaseImage string
	// Level is the feature level and also the newest schema version this build can apply.
	Level          int
	InventoryFault bool
}

// Load parses the build-time values and fails on a build that was configured wrongly.
func Load() (Info, error) {
	level, err := strconv.Atoi(FeatureLevel)
	if err != nil || level < 1 || level > 2 {
		return Info{}, fmt.Errorf("build value FeatureLevel=%q is not 1 or 2", FeatureLevel)
	}
	if Fault != "none" && Fault != FaultInventory {
		return Info{}, fmt.Errorf("build value Fault=%q is not none or %s", Fault, FaultInventory)
	}
	return Info{
		Version:        Version,
		BaseImage:      BaseImage,
		Level:          level,
		InventoryFault: Fault == FaultInventory,
	}, nil
}
