// Package setup is the first-run wizard: a small web application on the loopback address
// that checks the machine, signs in to the cloud, creates the home of the control plane
// and hands over to the control plane.
package setup

import (
	"fmt"
	"os/exec"
)

// Targets that the wizard can set up.
const (
	TargetLocal = "local"
	TargetAWS   = "aws"
)

// Check is the result of one requirement on this machine.
type Check struct {
	Name   string `json:"name"`
	Why    string `json:"why"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
}

type tool struct {
	name string
	why  string
	fix  string
}

var localTools = []tool{
	{"docker", "runs the local cluster", "Install Docker Desktop, then start it."},
	{"tofu", "plans and applies the infrastructure", "brew install opentofu"},
	{"kubectl", "talks to the cluster", "brew install kubectl"},
	{"helm", "installs the application chart", "brew install helm"},
	{"kind", "creates the local cluster", "brew install kind"},
}

// tunnelTool opens the tunnel to the container of the earlier design.
const tunnelTool = "session-manager-plugin"

var awsTools = []tool{
	{"aws", "signs in to your AWS account and creates the control plane",
		"brew install awscli"},
	{tunnelTool, "opens the private tunnel to the container of the control plane",
		"brew install --cask session-manager-plugin"},
}

// LookPath finds a program. Tests replace it.
var LookPath = exec.LookPath

// DockerAnswers reports whether the Docker daemon answers. Tests replace it.
var DockerAnswers = func() bool { return exec.Command("docker", "info").Run() == nil }

// Checks returns the requirements of a target and whether this machine meets each one.
func Checks(target string) ([]Check, error) {
	var tools []tool
	switch target {
	case TargetLocal:
		tools = localTools
	case TargetAWS:
		tools = awsTools
	default:
		return nil, fmt.Errorf("target %q does not exist: use %s or %s", target, TargetLocal,
			TargetAWS)
	}
	checks := make([]Check, 0, len(tools)+1)
	for _, t := range tools {
		check := Check{Name: t.name, Why: t.why}
		if path, err := LookPath(t.name); err == nil {
			check.OK, check.Detail = true, path
		} else {
			check.Detail, check.Fix = "not installed", t.fix
		}
		checks = append(checks, check)
	}
	if target == TargetLocal {
		daemon := Check{Name: "Docker daemon", Why: "must run before the cluster can start"}
		if DockerAnswers() {
			daemon.OK, daemon.Detail = true, "answers"
		} else {
			daemon.Detail, daemon.Fix = "does not answer", "Start Docker Desktop, then check again."
		}
		checks = append(checks, daemon)
	}
	return checks, nil
}

// Missing returns the names of the checks that failed.
func Missing(checks []Check) []string {
	missing := []string{}
	for _, c := range checks {
		if !c.OK {
			missing = append(missing, c.Name)
		}
	}
	return missing
}
