package setup

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"strings"
)

// AWSDetails is what the step "Names and tags" shows: the zones of the account, what a dry
// run of a launch says about the required tags, and the values of home.tfvars that are not
// private.
type AWSDetails struct {
	Zones       []Zone      `json:"zones"`
	ZonesError  string      `json:"zones_error,omitempty"`
	Launch      LaunchCheck `json:"launch"`
	LaunchError string      `json:"launch_error,omitempty"`
	// SavedZone, SavedTagKeys and SavedModelKey come from home.tfvars. The values of the
	// tags and the key stay in the file.
	SavedZone     string   `json:"saved_zone"`
	SavedTagKeys  []string `json:"saved_tag_keys"`
	SavedModelKey bool     `json:"saved_model_key"`
	Checkout      string   `json:"checkout"`
	Nanos         string   `json:"nanos"`
	Directory     string   `json:"directory"`
}

// placedAccount returns the session of the state, or why the step cannot run yet.
func (s *Server) placedAccount() (AWS, State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state
	if state.Target != TargetAWS || state.Account == "" {
		return AWS{}, state, invalid("sign in to AWS first")
	}
	if state.PlaceMode != PlaceExisting || len(state.Subnets) == 0 {
		return AWS{}, state, invalid("choose a network that exists first: the installer " +
			"does not create a network")
	}
	return s.aws(), state, nil
}

// getDetails reads the zones and makes a dry run of a launch. It changes nothing.
func (s *Server) getDetails(r *http.Request) (any, error) {
	account, state, err := s.placedAccount()
	if err != nil {
		return nil, err
	}
	details := AWSDetails{Zones: []Zone{}, Checkout: state.Source, Nanos: state.Nanos,
		SavedTagKeys: []string{}}
	if details.Checkout == "" {
		details.Checkout = s.cfg.Checkout
	}
	if details.Nanos == "" {
		details.Nanos = DefaultNanos
	}
	if details.Zones, err = account.Zones(r.Context()); err != nil {
		details.ZonesError = err.Error()
	}
	if details.Launch, err = account.CheckLaunch(r.Context(), state.Subnets[0], nil); err != nil {
		details.LaunchError = err.Error()
	}
	if details.Launch.RequiredTags == nil {
		details.Launch.RequiredTags = []string{}
	}
	saved, _, err := s.installer.Defaults(state.Env)
	if err != nil {
		return nil, err
	}
	details.SavedZone, details.SavedModelKey = saved.ZoneID, saved.ModelKey != ""
	details.SavedTagKeys = append(details.SavedTagKeys, slices.Sorted(maps.Keys(saved.Tags))...)
	details.Directory, err = s.installer.Directory(state.Env)
	return details, err
}

// postDetails records the zone, the required tags, the checkout and the kernel. The values
// of the tags stay in memory until the installer writes them to home.tfvars.
func (s *Server) postDetails(r *http.Request) (any, error) {
	var body struct {
		ZoneID   string            `json:"zone_id"`
		Tags     map[string]string `json:"tags"`
		Checkout string            `json:"checkout"`
		Nanos    string            `json:"nanos"`
	}
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	account, state, err := s.placedAccount()
	if err != nil {
		return nil, err
	}
	zone, err := account.PublicZone(r.Context(), body.ZoneID)
	if err != nil {
		return nil, invalid("%v", err)
	}
	if !zone.Delegated {
		return nil, invalid("the zone %s (%s) cannot hold the names: %s", zone.Name, zone.ID,
			zone.Reason)
	}
	tags, err := s.checkTags(state.Env, body.Tags)
	if err != nil {
		return nil, err
	}
	source, err := openCheckout(body.Checkout)
	if err != nil {
		return nil, invalid("%v", err)
	}
	if body.Nanos == "" {
		body.Nanos = DefaultNanos
	}
	if !nanosVersionPattern.MatchString(body.Nanos) {
		return nil, invalid("the Nanos version %q is not valid: use the version of a release, "+
			"for example %s", body.Nanos, DefaultNanos)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.ZoneID, s.state.ZoneName = zone.ID, zone.Name
	s.state.TagKeys = slices.Sorted(maps.Keys(tags))
	s.state.Source, s.state.Nanos = source.dir, body.Nanos
	s.tags = tags
	s.review = nil
	return s.state, s.save()
}

// checkTags checks the keys and the values of the required tags. An empty value keeps the
// value of home.tfvars, which must then have one.
func (s *Server) checkTags(env string, tags map[string]string) (map[string]string, error) {
	saved, _, err := s.installer.Defaults(env)
	if err != nil {
		return nil, err
	}
	kept := map[string]string{}
	for key, value := range tags {
		key = strings.TrimSpace(key)
		if key == "" || len(key) > 128 {
			return nil, invalid("a tag has no key, or a key longer than 128 characters")
		}
		if value == "" && saved.Tags[key] == "" {
			return nil, invalid("give a value for the tag %s", key)
		}
		kept[key] = value
	}
	return kept, nil
}

// installInput is the input of the installer from the choices of the wizard.
func (s *Server) installInput() InstallInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state
	in := InstallInput{Env: state.Env, Profile: state.Profile, Region: state.Region,
		Network: state.Network, Subnets: state.Subnets, RouterGroup: state.RouterGroup,
		ZoneID: state.ZoneID, Tags: maps.Clone(s.tags), ModelKey: s.key, Nanos: state.Nanos,
		Source: state.Source}
	switch state.KeyChoice {
	case ModelBedrock:
		in.ModelProvider, in.BedrockModels = ProviderBedrock, maps.Clone(state.BedrockModels)
	case ModelHosted:
		in.ModelProvider = ProviderOpenRouter
	}
	return in
}

// deployAWS starts the installer. The person pressed Create on the review.
func (s *Server) deployAWS(state State, reviewed bool) error {
	if !reviewed {
		return invalid("review what will be created before you create it")
	}
	plan := awsPlan(state)
	if plan == nil {
		return invalid("choose the network of the control plane before you create it")
	}
	if blocked := plan.Blocked; len(blocked) > 0 {
		return invalid("%s", strings.Join(blocked, "; "))
	}
	if err := s.checkAWSModel(state); err != nil {
		return err
	}
	return s.startJob("install", func(ctx context.Context, current *job) error {
		result, err := s.installer.Install(ctx, s.installInput(), current.report(InstallSteps))
		if err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state.Installed, s.state.Result = true, &result
		s.key, s.modelChosen = "", false
		return s.save()
	})
}

// checkAWSModel refuses a choice of the step Language model that the installer cannot use.
func (s *Server) checkAWSModel(state State) error {
	switch state.KeyChoice {
	case ModelBedrock:
		if len(state.BedrockModels) == 0 {
			return invalid("choose the models of Amazon Bedrock again in the step Language model")
		}
		return nil
	case ModelHosted:
	default:
		return invalid("choose Amazon Bedrock or a hosted key in the step Language model: a " +
			"control plane on AWS cannot reach a model on this machine")
	}
	saved, _, err := s.installer.Defaults(state.Env)
	if err != nil {
		return err
	}
	s.mu.Lock()
	keyHeld := s.key != ""
	s.mu.Unlock()
	if !keyHeld && saved.ModelKey == "" {
		return invalid("give the model key again in the step Language model: the wizard " +
			"keeps it in memory until the installer writes it to home.tfvars")
	}
	return nil
}

// postRemove starts the removal of the environment on AWS. The person must type the name
// of the environment.
func (s *Server) postRemove(r *http.Request) (any, error) {
	var body struct{ Env string }
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	s.mu.Lock()
	state := s.state
	s.mu.Unlock()
	if state.Target != TargetAWS || state.Env == "" {
		return nil, invalid("the wizard removes an environment on AWS only")
	}
	if body.Env != state.Env {
		return nil, invalid("type the name of the environment, %s, to remove it", state.Env)
	}
	input := InstallInput{Env: state.Env, Profile: state.Profile, Region: state.Region}
	err := s.startJob("remove", func(ctx context.Context, current *job) error {
		if err := s.installer.Uninstall(ctx, input, current.report(RemoveSteps)); err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state.Installed, s.state.Result = false, nil
		return s.save()
	})
	return map[string]bool{"started": err == nil}, err
}
