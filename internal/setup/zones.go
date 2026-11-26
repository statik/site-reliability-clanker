package setup

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"
)

var zoneIDPattern = regexp.MustCompile(`^Z[0-9A-Z]{1,32}$`)

// Zone is a public Route 53 zone of the account. The installer puts the names of the core
// and of Keycloak in it, and validates their certificate with records in it.
type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Delegated is true when the name servers of the zone are the ones that the parent zone
	// names for its domain. An account can hold two zones with one name, and only the
	// delegated one answers on the internet.
	Delegated bool   `json:"delegated"`
	Reason    string `json:"reason,omitempty"`
}

// LookupNS returns the name servers of a domain as the internet sees them. Tests replace it.
var LookupNS = net.DefaultResolver.LookupNS

// Zones lists the public Route 53 zones of the account and checks the delegation of each.
func (a AWS) Zones(ctx context.Context) ([]Zone, error) {
	var found []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := a.query(ctx, a.Region, &found, "route53", "list-hosted-zones", "--query",
		"HostedZones[?Config.PrivateZone==`false`].{id:Id,name:Name}"); err != nil {
		return nil, fmt.Errorf("list the public zones of Route 53: %w", err)
	}
	zones := make([]Zone, 0, len(found))
	for _, one := range found {
		zone, err := a.zone(ctx, strings.TrimPrefix(one.ID, "/hostedzone/"),
			strings.TrimSuffix(one.Name, "."))
		if err != nil {
			return nil, err
		}
		zones = append(zones, zone)
	}
	slices.SortFunc(zones, func(x, y Zone) int { return strings.Compare(x.Name, y.Name) })
	return zones, nil
}

// PublicZone returns one zone of the account with the check of its delegation.
func (a AWS) PublicZone(ctx context.Context, id string) (Zone, error) {
	if !zoneIDPattern.MatchString(id) {
		return Zone{}, fmt.Errorf("the zone ID %q is not valid: use an ID such as Z0123EXAMPLE",
			id)
	}
	var found struct {
		Name    string `json:"name"`
		Private bool   `json:"private"`
	}
	if err := a.query(ctx, a.Region, &found, "route53", "get-hosted-zone", "--id", id,
		"--query", "HostedZone.{name:Name,private:Config.PrivateZone}"); err != nil {
		return Zone{}, fmt.Errorf("read the zone %s: %w", id, err)
	}
	if found.Private {
		return Zone{}, fmt.Errorf("the zone %s is private: the certificate needs a public zone",
			id)
	}
	return a.zone(ctx, id, strings.TrimSuffix(found.Name, "."))
}

// zone compares the name servers of the zone with the name servers of its domain.
func (a AWS) zone(ctx context.Context, id, name string) (Zone, error) {
	zone := Zone{ID: id, Name: name}
	var own []string
	if err := a.query(ctx, a.Region, &own, "route53", "get-hosted-zone", "--id", id,
		"--query", "DelegationSet.NameServers"); err != nil {
		return Zone{}, fmt.Errorf("read the name servers of the zone %s: %w", id, err)
	}
	published, err := LookupNS(ctx, name)
	if err != nil {
		zone.Reason = fmt.Sprintf("the name servers of %s were not found on the internet: %v",
			name, err)
		return zone, nil
	}
	internet := make([]string, 0, len(published))
	for _, server := range published {
		internet = append(internet, server.Host)
	}
	if sameServers(own, internet) {
		zone.Delegated = true
		return zone, nil
	}
	zone.Reason = fmt.Sprintf("the parent zone names other name servers for %s: this zone "+
		"does not answer on the internet, so the certificate cannot be validated", name)
	return zone, nil
}

func sameServers(a, b []string) bool {
	normal := func(names []string) []string {
		out := make([]string, 0, len(names))
		for _, name := range names {
			out = append(out, strings.ToLower(strings.TrimSuffix(name, ".")))
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	return len(a) > 0 && slices.Equal(normal(a), normal(b))
}
