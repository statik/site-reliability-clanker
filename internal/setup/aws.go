package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// ErrNeedsLogin means that the profile has no valid session.
var ErrNeedsLogin = errors.New("the AWS profile has no valid session")

var (
	envNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,22}[a-z0-9]$`)
	regionPattern  = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d$`)
	profilePattern = regexp.MustCompile(`^[A-Za-z0-9_.@+=,][A-Za-z0-9_.@+=,/-]{0,127}$`)
)

// CLI runs the aws program and returns its standard output. Tests replace it.
type CLI func(ctx context.Context, args ...string) ([]byte, error)

func realCLI(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "aws", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("aws %s: %w: %s", args[0]+" "+args[1], err,
			lastPart(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// lastPart returns the end of the output of a program, where its error is.
func lastPart(output string) string {
	text := strings.TrimSpace(output)
	if len(text) > 600 {
		text = text[len(text)-600:]
	}
	return text
}

// AWS talks to one account through one profile of the AWS CLI. The wizard uses the
// session of the person who runs it. It stores no credential.
type AWS struct {
	Profile string
	Region  string
	Env     string
	Run     CLI
}

// Validate checks the three names before they go into a command line.
func (a AWS) Validate() error {
	if !profilePattern.MatchString(a.Profile) {
		return fmt.Errorf("the profile name %q is not valid", a.Profile)
	}
	if !regionPattern.MatchString(a.Region) {
		return fmt.Errorf("the region %q is not valid: use a name such as us-east-1", a.Region)
	}
	if !envNamePattern.MatchString(a.Env) {
		return fmt.Errorf("the environment name %q is not valid: use 3 to 24 lowercase "+
			"letters, digits or hyphens, and start with a letter", a.Env)
	}
	return nil
}

func (a AWS) cli(ctx context.Context, args ...string) ([]byte, error) {
	full := append(args, "--profile", a.Profile, "--region", a.Region, "--output", "json")
	return a.Run(ctx, full...)
}

// Profiles lists the profiles of the AWS CLI on this machine.
func Profiles(ctx context.Context, run CLI) ([]string, error) {
	out, err := run(ctx, "configure", "list-profiles")
	if err != nil {
		return nil, err
	}
	profiles := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			profiles = append(profiles, name)
		}
	}
	return profiles, nil
}

// Identity is the answer of AWS to "who am I".
type Identity struct {
	Account string `json:"Account"`
	Arn     string `json:"Arn"`
}

// WhoAmI returns the account and principal of the profile, or ErrNeedsLogin.
func (a AWS) WhoAmI(ctx context.Context) (Identity, error) {
	out, err := a.cli(ctx, "sts", "get-caller-identity")
	if err != nil {
		text := strings.ToLower(err.Error())
		for _, sign := range []string{"sso", "expired", "token", "credentials"} {
			if strings.Contains(text, sign) {
				return Identity{}, fmt.Errorf("%w: %v", ErrNeedsLogin, err)
			}
		}
		return Identity{}, err
	}
	var id Identity
	if err := json.Unmarshal(out, &id); err != nil {
		return Identity{}, fmt.Errorf("decode the answer of sts get-caller-identity: %w", err)
	}
	return id, nil
}

// Login starts the browser sign-in of the profile and waits for it.
func (a AWS) Login(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if _, err := a.Run(ctx, "sso", "login", "--profile", a.Profile); err != nil {
		return fmt.Errorf("%w. If the profile %s gets its credentials from another tool, "+
			"sign in with that tool and choose \"Sign in\" again", err, a.Profile)
	}
	return nil
}

// Change is one part that the review lists.
type Change struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	Type   string `json:"type"`
}
