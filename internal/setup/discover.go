package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
)

// Discovery is what the wizard finds in an AWS account before it proposes where to
// install the control plane. Every call that makes it only reads.
type Discovery struct {
	Identity Identity `json:"identity"`
	// Regions are the enabled regions that hold something, the most used first.
	Regions []RegionUse `json:"regions"`
	// Region is the region of Networks and Placement.
	Region    string    `json:"region"`
	Networks  []Network `json:"networks"`
	Placement Placement `json:"placement"`
	// NewNetworkCIDR is the address range that a new network would get, for a person who
	// does not want a network that exists. It is empty when no range is free.
	NewNetworkCIDR string  `json:"new_network_cidr"`
	Rights         []Right `json:"rights"`
	// RightsError says why the rights are not known. The rest of the discovery stands.
	RightsError string `json:"rights_error,omitempty"`
}

// RegionUse counts what runs in one region.
type RegionUse struct {
	Name      string `json:"name"`
	Instances int    `json:"instances"`
	// Networks counts the VPCs that are not the default VPC of the region.
	Networks int `json:"networks"`
}

// Subnet is one subnet of a network. Outbound is "nat" when its default route goes to a
// NAT gateway, "internet" when it goes to an internet gateway, and "none" without one.
type Subnet struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	CIDR     string `json:"cidr"`
	Zone     string `json:"zone"`
	Outbound string `json:"outbound"`
}

// Router is a Tailscale subnet router that runs in a network.
type Router struct {
	Service string   `json:"service"`
	Routes  []string `json:"routes"`
	// Covers is true when the routes hold the whole address range of the network.
	Covers bool `json:"covers"`
}

// Network is one VPC with what decides whether the control plane fits in it.
type Network struct {
	ID        string            `json:"id"`
	Name      string            `json:"name,omitempty"`
	CIDR      string            `json:"cidr"`
	Default   bool              `json:"default"`
	Tags      map[string]string `json:"tags,omitempty"`
	Subnets   []Subnet          `json:"subnets"`
	Instances int               `json:"instances"`
	Router    *Router           `json:"router,omitempty"`
}

// privateZones returns the availability zones in which the network has a subnet whose
// outbound traffic goes through a NAT gateway.
func (n Network) privateZones() []string {
	seen := map[string]bool{}
	for _, subnet := range n.Subnets {
		if subnet.Outbound == outboundNAT {
			seen[subnet.Zone] = true
		}
	}
	zones := make([]string, 0, len(seen))
	for zone := range seen {
		zones = append(zones, zone)
	}
	sort.Strings(zones)
	return zones
}

const (
	outboundNAT      = "nat"
	outboundInternet = "internet"
	outboundNone     = "none"
	// The managed database needs subnets in two availability zones.
	zonesNeeded = 2
)

// query runs a read call of the AWS CLI in a region and decodes its JSON answer.
func (a AWS) query(ctx context.Context, region string, out any, args ...string) error {
	full := append(args, "--profile", a.Profile, "--region", region, "--output", "json")
	raw, err := a.Run(ctx, full...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode the answer of aws %s %s: %w", args[0], args[1], err)
	}
	return nil
}

// regionUse counts the running instances and the VPCs of one region.
func (a AWS) regionUse(ctx context.Context, region string) (RegionUse, error) {
	use := RegionUse{Name: region}
	if err := a.query(ctx, region, &use.Instances, "ec2", "describe-instances", "--filters",
		"Name=instance-state-name,Values=running",
		"--query", "length(Reservations[].Instances[])"); err != nil {
		return use, err
	}
	err := a.query(ctx, region, &use.Networks, "ec2", "describe-vpcs",
		"--query", "length(Vpcs[?!IsDefault])")
	return use, err
}

// RegionsInUse returns the enabled regions of the account that hold a running instance or
// a VPC of their own, the most used first. start is a region that answers for the list.
func (a AWS) RegionsInUse(ctx context.Context, start string) ([]RegionUse, error) {
	var names []string
	if err := a.query(ctx, start, &names, "ec2", "describe-regions",
		"--query", "Regions[].RegionName"); err != nil {
		return nil, err
	}
	uses := make([]RegionUse, len(names))
	failures := make([]error, len(names))
	var wait sync.WaitGroup
	limit := make(chan struct{}, 6)
	for index, name := range names {
		wait.Add(1)
		go func() {
			defer wait.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			uses[index], failures[index] = a.regionUse(ctx, name)
		}()
	}
	wait.Wait()
	used := []RegionUse{}
	for index, use := range uses {
		if failures[index] != nil {
			return nil, fmt.Errorf("count what runs in %s: %w", names[index], failures[index])
		}
		if use.Instances > 0 || use.Networks > 0 {
			used = append(used, use)
		}
	}
	sort.Slice(used, func(i, j int) bool { return moreUsed(used[i], used[j]) })
	return used, nil
}

// moreUsed orders regions by their instances, then their networks, then their names.
func moreUsed(a, b RegionUse) bool {
	if a.Instances != b.Instances {
		return a.Instances > b.Instances
	}
	if a.Networks != b.Networks {
		return a.Networks > b.Networks
	}
	return a.Name < b.Name
}

type awsTag struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

type awsVPC struct {
	ID      string   `json:"id"`
	CIDR    string   `json:"cidr"`
	Default bool     `json:"default"`
	Tags    []awsTag `json:"tags"`
}

type awsSubnet struct {
	ID   string   `json:"id"`
	VPC  string   `json:"vpc"`
	CIDR string   `json:"cidr"`
	Zone string   `json:"zone"`
	Tags []awsTag `json:"tags"`
}

type awsRouteTable struct {
	VPC     string   `json:"vpc"`
	Main    bool     `json:"main"`
	Subnets []string `json:"subnets"`
	NAT     string   `json:"nat"`
	Gateway string   `json:"gateway"`
}

func (t awsRouteTable) outbound() string {
	switch {
	case t.NAT != "":
		return outboundNAT
	case strings.HasPrefix(t.Gateway, "igw-"):
		return outboundInternet
	default:
		return outboundNone
	}
}

func tagMap(tags []awsTag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	named := make(map[string]string, len(tags))
	for _, tag := range tags {
		named[tag.Key] = tag.Value
	}
	return named
}

// outboundOf maps each subnet to where its default route goes. A subnet with no route
// table of its own uses the main table of its network.
func outboundOf(subnets []awsSubnet, tables []awsRouteTable) map[string]string {
	explicit, main := map[string]string{}, map[string]string{}
	for _, table := range tables {
		if table.Main {
			main[table.VPC] = table.outbound()
		}
		for _, subnet := range table.Subnets {
			if subnet != "" {
				explicit[subnet] = table.outbound()
			}
		}
	}
	result := make(map[string]string, len(subnets))
	for _, subnet := range subnets {
		switch {
		case explicit[subnet.ID] != "":
			result[subnet.ID] = explicit[subnet.ID]
		case main[subnet.VPC] != "":
			result[subnet.ID] = main[subnet.VPC]
		default:
			result[subnet.ID] = outboundNone
		}
	}
	return result
}

// Networks returns the VPCs of a region with their subnets, the number of running
// instances and the Tailscale subnet router, when one runs there.
func (a AWS) Networks(ctx context.Context, region string) ([]Network, error) {
	var vpcs []awsVPC
	if err := a.query(ctx, region, &vpcs, "ec2", "describe-vpcs", "--query",
		"Vpcs[].{id:VpcId,cidr:CidrBlock,default:IsDefault,tags:Tags}"); err != nil {
		return nil, err
	}
	var subnets []awsSubnet
	if err := a.query(ctx, region, &subnets, "ec2", "describe-subnets", "--query",
		"Subnets[].{id:SubnetId,vpc:VpcId,cidr:CidrBlock,zone:AvailabilityZone,tags:Tags}"); err != nil {
		return nil, err
	}
	var tables []awsRouteTable
	if err := a.query(ctx, region, &tables, "ec2", "describe-route-tables", "--query",
		"RouteTables[].{vpc:VpcId,main:length(Associations[?Main])>`0`,"+
			"subnets:Associations[].SubnetId,"+
			"nat:Routes[?DestinationCidrBlock=='0.0.0.0/0']|[0].NatGatewayId,"+
			"gateway:Routes[?DestinationCidrBlock=='0.0.0.0/0']|[0].GatewayId}"); err != nil {
		return nil, err
	}
	var instances []string
	if err := a.query(ctx, region, &instances, "ec2", "describe-instances", "--filters",
		"Name=instance-state-name,Values=running",
		"--query", "Reservations[].Instances[].VpcId"); err != nil {
		return nil, err
	}
	routers, err := a.routers(ctx, region)
	if err != nil {
		return nil, err
	}
	return assemble(vpcs, subnets, outboundOf(subnets, tables), instances, routers), nil
}

// assemble joins the answers of the read calls into one list of networks, in the order of
// their names.
func assemble(vpcs []awsVPC, subnets []awsSubnet, outbound map[string]string,
	instances []string, routers []foundRouter) []Network {
	networks := make([]Network, 0, len(vpcs))
	for _, vpc := range vpcs {
		network := Network{ID: vpc.ID, CIDR: vpc.CIDR, Default: vpc.Default, Tags: tagMap(vpc.Tags),
			Subnets: []Subnet{}}
		network.Name = network.Tags["Name"]
		for _, subnet := range subnets {
			if subnet.VPC == vpc.ID {
				network.Subnets = append(network.Subnets, Subnet{ID: subnet.ID,
					Name: tagMap(subnet.Tags)["Name"], CIDR: subnet.CIDR, Zone: subnet.Zone,
					Outbound: outbound[subnet.ID]})
			}
		}
		sort.Slice(network.Subnets, func(i, j int) bool {
			return network.Subnets[i].CIDR < network.Subnets[j].CIDR
		})
		for _, id := range instances {
			if id == vpc.ID {
				network.Instances++
			}
		}
		network.Router = routerOf(network, routers)
		networks = append(networks, network)
	}
	sort.Slice(networks, func(i, j int) bool {
		if networks[i].Name != networks[j].Name {
			return networks[i].Name < networks[j].Name
		}
		return networks[i].ID < networks[j].ID
	})
	return networks
}

// Discover signs nothing in and changes nothing. It finds who the profile is, which
// regions are in use, the networks of one region and where the control plane fits. With
// an empty region it takes the region that is used most. start is a region that answers
// for the list of regions.
func (a AWS) Discover(ctx context.Context, start, region string) (Discovery, error) {
	if err := a.checkNames(start, region); err != nil {
		return Discovery{}, err
	}
	a.Region = start
	identity, err := a.WhoAmI(ctx)
	if err != nil {
		return Discovery{}, err
	}
	found := Discovery{Identity: identity, Region: region}
	if found.Regions, err = a.RegionsInUse(ctx, start); err != nil {
		return Discovery{}, err
	}
	if found.Region == "" {
		found.Region = mostUsed(found.Regions, start)
	}
	if found.Networks, err = a.Networks(ctx, found.Region); err != nil {
		return Discovery{}, err
	}
	found.Placement = Place(found.Networks)
	found.NewNetworkCIDR = placeNew(found.Networks, nil).CIDR
	if found.Rights, err = a.Rights(ctx, identity); err != nil {
		found.RightsError = err.Error()
	}
	return found, nil
}

// checkNames checks the profile and the regions before they go into a command line.
func (a AWS) checkNames(regions ...string) error {
	if !profilePattern.MatchString(a.Profile) {
		return fmt.Errorf("the profile name %q is not valid", a.Profile)
	}
	for _, name := range regions {
		if name != "" && !regionPattern.MatchString(name) {
			return fmt.Errorf("the region %q is not valid: use a name such as us-east-1", name)
		}
	}
	return nil
}

// mostUsed returns the first region of the list, or the fallback for an empty account.
func mostUsed(regions []RegionUse, fallback string) string {
	if len(regions) > 0 {
		return regions[0].Name
	}
	return fallback
}

// startRegion answers for the list of the regions of an account.
const startRegion = "us-east-1"

// DiscoverAccount runs the discovery with the AWS CLI of this machine. With an empty region
// it surveys the region that is used most.
func DiscoverAccount(ctx context.Context, profile, region string) (Discovery, error) {
	return AWS{Profile: profile, Run: realCLI}.Discover(ctx, startRegion, region)
}

// prefix parses an address range. An answer of AWS that is not a range is not in a result.
func prefix(cidr string) (netip.Prefix, bool) {
	parsed, err := netip.ParsePrefix(cidr)
	if err != nil {
		return netip.Prefix{}, false
	}
	return parsed.Masked(), true
}
