package setup

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Placement modes: install into a network that exists, or make a new one.
const (
	PlaceExisting = "existing"
	PlaceNew      = "new"
	// Access through a Tailscale subnet router that is there, or no private path yet.
	AccessTailscale = "tailscale"
	AccessNone      = "none"
)

// Rejection says why a network is not a place for the control plane.
type Rejection struct {
	NetworkID string `json:"network_id"`
	Name      string `json:"name,omitempty"`
	Reason    string `json:"reason"`
}

// Placement is the proposal of the wizard for the home of the control plane. A person
// can choose another network of the discovery.
type Placement struct {
	Mode string `json:"mode"`
	// NetworkID and Subnets name the network that exists and one private subnet in each of
	// two availability zones.
	NetworkID string   `json:"network_id,omitempty"`
	Subnets   []string `json:"subnets,omitempty"`
	// CIDR is the address range of the new network. No network of the region overlaps it.
	CIDR string `json:"cidr,omitempty"`
	// Access says how a person reaches the control plane, which has no public address.
	Access   string      `json:"access"`
	Reasons  []string    `json:"reasons"`
	Rejected []Rejection `json:"rejected,omitempty"`
}

// unfit returns why the control plane cannot go into the network, or "" when it can.
func unfit(n Network) string {
	if n.Default {
		return "it is the default network of the region, where every subnet is public"
	}
	if zones := n.privateZones(); len(zones) < zonesNeeded {
		return fmt.Sprintf("it has private subnets with a NAT gateway in %d availability "+
			"zones, and the database needs %d", len(zones), zonesNeeded)
	}
	return ""
}

func covered(n Network) bool { return n.Router != nil && n.Router.Covers }

// subnetsFor picks one private subnet in each of the first two availability zones.
func subnetsFor(n Network) []string {
	picked := []string{}
	for _, zone := range n.privateZones()[:zonesNeeded] {
		for _, subnet := range n.Subnets {
			if subnet.Zone == zone && subnet.Outbound == outboundNAT {
				picked = append(picked, subnet.ID)
				break
			}
		}
	}
	return picked
}

// Place proposes where the control plane goes. It prefers a network that fits and that a
// Tailscale subnet router already serves, then the network that the fewest instances
// share. When no network fits, it proposes a new one with an address range that is free
// in the region.
func Place(networks []Network) Placement {
	fit := []Network{}
	rejected := []Rejection{}
	for _, network := range networks {
		if reason := unfit(network); reason != "" {
			rejected = append(rejected, Rejection{NetworkID: network.ID, Name: network.Name,
				Reason: reason})
			continue
		}
		fit = append(fit, network)
	}
	if len(fit) == 0 {
		return placeNew(networks, rejected)
	}
	sort.SliceStable(fit, func(i, j int) bool {
		if covered(fit[i]) != covered(fit[j]) {
			return covered(fit[i])
		}
		return fit[i].Instances < fit[j].Instances
	})
	best := fit[0]
	placement := Placement{Mode: PlaceExisting, NetworkID: best.ID, Subnets: subnetsFor(best),
		Access: AccessNone, Rejected: rejected}
	placement.Reasons = append(placement.Reasons, fmt.Sprintf("%s has private subnets with a "+
		"NAT gateway in %d availability zones", label(best), len(best.privateZones())))
	if covered(best) {
		placement.Access = AccessTailscale
		placement.Reasons = append(placement.Reasons, fmt.Sprintf("the Tailscale subnet "+
			"router %s serves its whole address range", best.Router.Service))
	} else {
		placement.Reasons = append(placement.Reasons, "no Tailscale subnet router serves it: "+
			"the control plane has no public address, so a private path is necessary")
	}
	placement.Reasons = append(placement.Reasons, fmt.Sprintf("%d instances run in it now",
		best.Instances))
	return placement
}

func label(n Network) string {
	if n.Name != "" {
		return n.Name
	}
	return n.ID
}

// placeNew proposes a new network with the first address range that no network overlaps.
func placeNew(networks []Network, rejected []Rejection) Placement {
	placement := Placement{Mode: PlaceNew, Access: AccessNone, Rejected: rejected,
		Reasons: []string{"no network of this region has private subnets with a NAT gateway " +
			"in two availability zones"}}
	for second := 64; second <= 255; second++ {
		candidate, _ := prefix(fmt.Sprintf("10.%d.0.0/22", second))
		free := true
		for _, network := range networks {
			if used, ok := prefix(network.CIDR); ok && used.Overlaps(candidate) {
				free = false
				break
			}
		}
		if free {
			placement.CIDR = candidate.String()
			placement.Reasons = append(placement.Reasons, "the address range "+placement.CIDR+
				" overlaps no network of this region")
			return placement
		}
	}
	placement.Reasons = append(placement.Reasons, "no free address range was found between "+
		"10.64.0.0/22 and 10.255.0.0/22: choose one by hand")
	return placement
}

// neededActions are the calls that the wizard and the access broker make to install and
// run the control plane. The wizard checks them before it proposes a change.
var neededActions = []string{
	"iam:CreateRole", "iam:PassRole", "iam:PutRolePolicy", "iam:DeleteRole",
	"ec2:CreateVpc", "ec2:CreateSecurityGroup", "ec2:RunInstances", "ec2:ImportSnapshot",
	"ec2:RegisterImage", "autoscaling:CreateAutoScalingGroup", "rds:CreateDBInstance",
	"ecs:RegisterTaskDefinition", "ecs:RunTask", "s3:CreateBucket", "ssm:PutParameter",
	"logs:CreateLogGroup",
}

// Right is whether the identity of the profile can make one call.
type Right struct {
	Action  string `json:"action"`
	Allowed bool   `json:"allowed"`
}

// principalARN returns the ARN that the policy simulator takes. A session of a role has
// an STS ARN; the simulator needs the ARN of the role itself, which IAM gives.
func (a AWS) principalARN(ctx context.Context, identity Identity) (string, error) {
	const marker = ":assumed-role/"
	at := strings.Index(identity.Arn, marker)
	if at < 0 {
		return identity.Arn, nil
	}
	role, _, _ := strings.Cut(identity.Arn[at+len(marker):], "/")
	var arn string
	err := a.query(ctx, a.Region, &arn, "iam", "get-role", "--role-name", role,
		"--query", "Role.Arn")
	return arn, err
}

// Rights asks the IAM policy simulator whether the identity can make the calls of the
// installation. The simulator reads the policies of the identity. It does not know a
// service control policy of the organization, so an allowed call can still be refused.
func (a AWS) Rights(ctx context.Context, identity Identity) ([]Right, error) {
	arn, err := a.principalARN(ctx, identity)
	if err != nil {
		return nil, fmt.Errorf("find the role of %s: %w", identity.Arn, err)
	}
	var results []struct {
		Action   string `json:"action"`
		Decision string `json:"decision"`
	}
	args := append([]string{"iam", "simulate-principal-policy", "--policy-source-arn", arn,
		"--action-names"}, neededActions...)
	args = append(args, "--query", "EvaluationResults[].{action:EvalActionName,decision:EvalDecision}")
	if err := a.query(ctx, a.Region, &results, args...); err != nil {
		return nil, fmt.Errorf("simulate the policies of %s: %w", arn, err)
	}
	rights := make([]Right, 0, len(results))
	for _, result := range results {
		rights = append(rights, Right{Action: result.Action, Allowed: result.Decision == "allowed"})
	}
	return rights, nil
}
