package setup

import (
	"net/http"
)

// errSurveyOnly answers the steps after the survey while the installation on AWS is not
// built.
var errSurveyOnly = invalid("the wizard surveys your AWS account and records where the " +
	"control plane goes. It does not create the control plane on AWS in this version")

// clearPlacement forgets where the control plane goes. The caller holds the lock.
func (s *Server) clearPlacement() {
	s.state.PlaceMode, s.state.Network, s.state.Subnets = "", "", nil
	s.state.NetworkCIDR, s.state.Access = "", ""
}

// postDiscover surveys the account of a profile and proposes where the control plane goes.
// It only reads. Without a profile it uses the profile of the sign-in. Without a region it
// surveys the region that is used most.
func (s *Server) postDiscover(r *http.Request) (any, error) {
	var body struct{ Profile, Region string }
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if body.Profile == "" {
		body.Profile = s.state.Profile
	}
	s.mu.Unlock()
	account := AWS{Profile: body.Profile, Run: s.cfg.CLI}
	if err := account.checkNames(body.Region); err != nil {
		return nil, invalid("%v", err)
	}
	found, err := account.Discover(r.Context(), startRegion, body.Region)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.found = &found
	s.mu.Unlock()
	return found, nil
}

// placeIn returns the state fields for a network of the survey, or why it does not fit.
func placeIn(found Discovery, id string) (State, error) {
	for _, network := range found.Networks {
		if network.ID != id {
			continue
		}
		if reason := unfit(network); reason != "" {
			return State{}, invalid("the control plane cannot go into %s: %s", label(network), reason)
		}
		placed := State{PlaceMode: PlaceExisting, Network: network.ID,
			Subnets: subnetsFor(network), Access: AccessNone}
		if covered(network) {
			placed.Access = AccessTailscale
		}
		return placed, nil
	}
	return State{}, invalid("the network %q is not in the survey of %s", id, found.Region)
}

// postPlace records where the control plane goes: a network of the last survey, or a new
// network with the address range that the survey found free. It creates nothing.
func (s *Server) postPlace(r *http.Request) (any, error) {
	var body struct{ Mode, Network string }
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.found == nil {
		return nil, invalid("survey the account before you choose a network")
	}
	var placed State
	switch body.Mode {
	case PlaceExisting:
		var err error
		if placed, err = placeIn(*s.found, body.Network); err != nil {
			return nil, err
		}
	case PlaceNew:
		if s.found.NewNetworkCIDR == "" {
			return nil, invalid("no address range is free for a new network in %s", s.found.Region)
		}
		placed = State{PlaceMode: PlaceNew, NetworkCIDR: s.found.NewNetworkCIDR, Access: AccessNone}
	default:
		return nil, invalid("the mode %q does not exist: use %s or %s", body.Mode, PlaceExisting,
			PlaceNew)
	}
	s.state.Region = s.found.Region
	s.state.PlaceMode, s.state.Network = placed.PlaceMode, placed.Network
	s.state.Subnets = placed.Subnets
	s.state.NetworkCIDR, s.state.Access = placed.NetworkCIDR, placed.Access
	s.review = nil
	return s.state, s.save()
}
