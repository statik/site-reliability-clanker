package setup

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"
)

// checkHealth asks the load balancer for the health of the core, then calls /healthz
// from this machine. The two are separate: this machine reaches the internal load balancer
// only through the Tailscale subnet router.
func (r *installRun) checkHealth(ctx context.Context) error {
	group := r.record.Outputs.TargetGroup
	var states []string
	healthy, err := r.poll(ctx, 30, func() (bool, error) {
		if err := r.aws.query(ctx, r.aws.Region, &states, "elbv2", "describe-target-health",
			"--target-group-arn", group, "--query",
			"TargetHealthDescriptions[].TargetHealth.State"); err != nil {
			return false, fmt.Errorf("read the health of the core from the load balancer: %w",
				err)
		}
		return slices.Contains(states, "healthy"), nil
	})
	if err != nil {
		return err
	}
	if !healthy {
		return fmt.Errorf("the load balancer reports the core as %v, not healthy. The log "+
			"of the instance is in the console of EC2 (Get system log)", states)
	}
	r.log("The load balancer reports the core healthy.")
	url := r.record.Outputs.CoreURL + "/healthz"
	var last error
	reached, err := r.poll(ctx, 12, func() (bool, error) {
		last = r.get(ctx, url)
		return last == nil, nil
	})
	if err != nil {
		return err
	}
	if !reached {
		return fmt.Errorf("the load balancer reports the core healthy, but this machine "+
			"cannot reach %s: %v. Connect this machine to the tailnet of the Tailscale "+
			"subnet router, then run the installation again: it goes on from this check",
			url, last)
	}
	r.log("%s answers from this machine.", url)
	return nil
}

func (r *installRun) get(ctx context.Context, url string) error {
	client := r.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return nil
}
