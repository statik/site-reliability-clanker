// Package stack carries the OpenTofu root of the home of the control plane: what the
// installer creates before any layer of platform/ exists. It is not in platform/, so the
// forge of the control plane does not get it and no agent changes it.
package stack

import (
	"embed"
	"io/fs"
)

// The patterns name each kind of file, so that a local .terraform directory and the tests
// stay out. A directory pattern would leave out the lock file, whose name starts with a dot.
//
//go:embed aws/*.tf aws/.terraform.lock.hcl aws/keycloak-start.sh
var files embed.FS

// AWS returns the files of the home stack on AWS, with paths relative to its root module.
// aws/README.md describes the inputs and the order of the two phases.
func AWS() (fs.FS, error) {
	return fs.Sub(files, "aws")
}
