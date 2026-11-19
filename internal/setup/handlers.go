package setup

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// localCreates lists what the local target adds to this machine.
func (s *Server) localCreates() []Change {
	return []Change{
		{"Add", "PostgreSQL for the control plane, in Docker", "container"},
		{"Add", "A Kubernetes cluster in Docker", "kind"},
		{"Add", "An image registry in Docker", "registry"},
		{"Add", "PostgreSQL in the cluster", "CloudNativePG"},
		{"Add", "A shared file system in the cluster", "NFS provisioner"},
		{"Add", "A gateway on localhost:8088", "Envoy Gateway"},
		{"Add", "The " + s.cfg.Application + " example application", "Helm release"},
	}
}

func (s *Server) getState(r *http.Request) (any, error) {
	s.mu.Lock()
	state, review, current := s.state, s.review, s.job
	keyHeld := s.key != ""
	s.mu.Unlock()
	return map[string]any{
		"version":       s.cfg.Version,
		"state":         state,
		"review":        review,
		"job":           current.view(),
		"key_held":      keyHeld,
		"dashboard_url": s.cfg.DashboardURL,
		"dashboard_up":  s.dashboardUp(r.Context()),
		"aws_plan":      awsPlan(state),
		"in_app":        s.cfg.InApp,
	}, nil
}

func (s *Server) getChecks(r *http.Request) (any, error) {
	checks, err := Checks(r.URL.Query().Get("target"))
	if err != nil {
		return nil, invalid("%v", err)
	}
	return map[string]any{"checks": checks, "ready": len(Missing(checks)) == 0}, nil
}

func (s *Server) postTarget(r *http.Request) (any, error) {
	var body struct{ Target, Env, Operator string }
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	if body.Target != TargetLocal && body.Target != TargetAWS {
		return nil, invalid("target %q does not exist: use %s or %s", body.Target, TargetLocal,
			TargetAWS)
	}
	if body.Target == TargetLocal {
		body.Env = "local"
	}
	if !envNamePattern.MatchString(body.Env) {
		return nil, invalid("the environment name %q is not valid: use 3 to 24 lowercase "+
			"letters, digits or hyphens, and start with a letter", body.Env)
	}
	operator, err := operatorFor(body.Target, body.Operator)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Deployed && (s.state.Target != body.Target || s.state.Env != body.Env) {
		return nil, invalid("the environment %s on %s is already set up. Remove it before "+
			"you set up another", s.state.Env, s.state.Target)
	}
	s.state.Target, s.state.Env = body.Target, body.Env
	// The users of an environment that exists do not change with a new name here.
	if !s.state.Deployed {
		s.state.Operator = operator
	}
	s.review = nil
	return s.state, s.save()
}

// operatorFor returns the name of the person who sets up the target. Only the local
// target needs it: on a cloud a person signs in.
func operatorFor(target, name string) (string, error) {
	if target != TargetLocal {
		return "", nil
	}
	name = strings.Join(strings.Fields(name), " ")
	if utf8.RuneCountInString(name) > 60 || !strings.ContainsFunc(name, unicode.IsLetter) {
		return "", invalid("give your name, in at most 60 characters. The dashboard " +
			"shows it when you approve a change")
	}
	return name, nil
}

// postModel records which language model the control plane uses: a hosted key or the
// model on this machine. The model on this machine needs a job, which downloads it when
// Ollama does not hold it.
func (s *Server) postModel(r *http.Request) (any, error) {
	var body struct{ Choice, Key string }
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	switch body.Choice {
	case ModelHosted:
		key := strings.TrimSpace(body.Key)
		if len(key) < 20 || strings.ContainsAny(key, " \t\r\n\"'") {
			return nil, invalid("this does not look like an API key: paste the key alone, " +
				"with no quotes or spaces")
		}
		return map[string]bool{"started": false}, s.chooseModel(ModelHosted, key)
	case ModelLocal:
		s.mu.Lock()
		target := s.state.Target
		s.mu.Unlock()
		if target != TargetLocal {
			return nil, invalid("a control plane in the cloud cannot reach a model on this " +
				"machine: use a hosted key")
		}
		err := s.start("model", s.prepareLocalModel)
		return map[string]bool{"started": err == nil}, err
	default:
		return nil, invalid("the choice %q does not exist: use %s or %s", body.Choice,
			ModelHosted, ModelLocal)
	}
}

func (s *Server) getProfiles(r *http.Request) (any, error) {
	profiles, err := Profiles(r.Context(), s.cfg.CLI)
	if err != nil {
		return nil, err
	}
	return map[string]any{"profiles": profiles}, nil
}

func (s *Server) postSignIn(r *http.Request) (any, error) {
	var body struct{ Profile, Region string }
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	s.mu.Lock()
	candidate := s.aws()
	s.mu.Unlock()
	// Without a region the survey of the next step finds the region that is in use.
	candidate.Profile, candidate.Region = body.Profile, body.Region
	if body.Region == "" {
		candidate.Region = startRegion
	}
	if err := candidate.Validate(); err != nil {
		return nil, invalid("%v", err)
	}
	identity, err := candidate.WhoAmI(r.Context())
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Profile, s.state.Region = body.Profile, body.Region
	s.state.Account, s.state.Principal = identity.Account, identity.Arn
	s.review, s.found = nil, nil
	s.clearPlacement()
	return s.state, s.save()
}

func (s *Server) postLogin(r *http.Request) (any, error) {
	var body struct{ Profile, Region string }
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	s.mu.Lock()
	candidate := s.aws()
	s.mu.Unlock()
	candidate.Profile, candidate.Region = body.Profile, body.Region
	if body.Region == "" {
		candidate.Region = startRegion
	}
	if err := candidate.Validate(); err != nil {
		return nil, invalid("%v", err)
	}
	err := s.start("login", func(ctx context.Context, log func(string)) error {
		log("A browser window opens for the AWS sign-in. Finish it there.")
		return candidate.Login(ctx)
	})
	return map[string]bool{"started": err == nil}, err
}

// postReview makes the list of the review. For this machine it is the list of the parts
// that the control plane plans. For AWS the plan is a part of the state, and the review
// needs only the sign-in and the network.
func (s *Server) postReview(r *http.Request) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.state.Target {
	case TargetLocal:
		s.review = s.localCreates()
	case TargetAWS:
		if s.state.Account == "" {
			return nil, invalid("sign in to AWS before the review")
		}
		if s.state.PlaceMode == "" {
			return nil, invalid("choose the network of the control plane before the review")
		}
	default:
		return nil, invalid("choose where to run before the review")
	}
	return map[string]bool{"started": false}, nil
}

// postDeploy starts the control plane on this machine. The wizard creates nothing on AWS.
func (s *Server) postDeploy(r *http.Request) (any, error) {
	s.mu.Lock()
	state, reviewed := s.state, len(s.review) > 0
	s.mu.Unlock()
	if state.Target != TargetLocal {
		return nil, invalid("the wizard creates nothing on AWS: the review is its last step " +
			"there")
	}
	// A control plane that was set up before is only started again: nothing new is
	// created, so there is nothing to review.
	if !reviewed && !state.Deployed {
		return nil, invalid("review what will be created before you create it")
	}
	if state.KeyChoice == "" {
		return nil, invalid("choose a language model before you start the control plane")
	}
	err := s.start("deploy", s.deployLocal)
	return map[string]bool{"started": err == nil}, err
}

func (s *Server) deployLocal(ctx context.Context, log func(string)) error {
	checks, err := Checks(TargetLocal)
	if err != nil {
		return err
	}
	if missing := Missing(checks); len(missing) > 0 {
		return fmt.Errorf("this machine is not ready: %s", strings.Join(missing, ", "))
	}
	s.mu.Lock()
	key, env, choice, chosen := s.key, s.state.Env, s.state.KeyChoice, s.modelChosen
	operator := s.state.Operator
	s.mu.Unlock()
	if chosen {
		if err := s.saveModelSettings(choice, key); err != nil {
			return err
		}
		log(modelSaved[choice])
	}
	if s.dashboardUp(ctx) {
		log("The control plane already runs on this machine.")
	} else {
		log("Starting the control plane on this machine.")
		if err := s.cfg.StartLocal(ctx, env, operator); err != nil {
			return err
		}
		if err := s.waitDashboard(ctx, 2*time.Minute); err != nil {
			return err
		}
	}
	log("The control plane runs. It planned the first deployment and waits for approval.")
	return s.markDeployed()
}

// modelSaved says what the start of the control plane wrote for each choice.
var modelSaved = map[string]string{
	ModelHosted: "The model key is saved on this machine, readable only by you.",
	ModelLocal:  "The control plane is set to use the model on this machine.",
}

func (s *Server) markDeployed() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Deployed = true
	s.key, s.modelChosen = "", false
	return s.save()
}
