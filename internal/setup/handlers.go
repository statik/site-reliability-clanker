package setup

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CostLine is one line of the cost estimate.
type CostLine struct {
	Item    string  `json:"item"`
	Size    string  `json:"size"`
	Monthly float64 `json:"monthly_usd"`
}

// On-demand list prices of Fargate on ARM in us-east-1, in USD for each hour. They are an
// estimate: other regions cost a little more, and tax and data transfer are not included.
const (
	vcpuHourly       = 0.03238
	memoryGBHourly   = 0.00356
	publicIPv4Hourly = 0.005
	storageGBMonthly = 0.30
	storageGB        = 1
	hoursEachMonth   = 730
	taskWaitLimit    = 5 * time.Minute
	dashboardLimit   = 3 * time.Minute
)

func costEstimate(size Size) []CostLine {
	vcpu, memoryGB := float64(size.CPU)/1024, float64(size.MemoryMB)/1024
	container := (vcpu*vcpuHourly + memoryGB*memoryGBHourly) * hoursEachMonth
	return []CostLine{
		{"Container on Fargate", fmt.Sprintf("%g vCPU, %g GB, ARM", vcpu, memoryGB), container},
		{"Encrypted file system", fmt.Sprintf("about %d GB of data", storageGB),
			storageGB * storageGBMonthly},
		{"Public IPv4 address", "1, with no inbound rule", publicIPv4Hourly * hoursEachMonth},
		{"Model key and log", "1 standard parameter, 30 days of log", 0},
	}
}

var localCreates = []Change{
	{"Add", "A Kubernetes cluster in Docker", "kind"},
	{"Add", "An image registry in Docker", "registry"},
	{"Add", "PostgreSQL in the cluster", "CloudNativePG"},
	{"Add", "A shared file system in the cluster", "NFS provisioner"},
	{"Add", "A gateway on localhost:8088", "Envoy Gateway"},
	{"Add", "The Paperboat example application", "Helm release"},
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
		"cost":          costEstimate(SizeOf(state.Size)),
		"aws_release":   s.cfg.Version != "" && s.cfg.Version != "dev",
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
	var body struct{ Target, Env string }
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Deployed && (s.state.Target != body.Target || s.state.Env != body.Env) {
		return nil, invalid("the environment %s on %s is already set up. Remove it before "+
			"you set up another", s.state.Env, s.state.Target)
	}
	s.state.Target, s.state.Env = body.Target, body.Env
	s.review = nil
	return s.state, s.save()
}

func (s *Server) postKey(r *http.Request) (any, error) {
	var body struct{ Key string }
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	key := strings.TrimSpace(body.Key)
	if key != "" && (len(key) < 20 || strings.ContainsAny(key, " \t\r\n\"'")) {
		return nil, invalid("this does not look like an API key: paste the key alone, " +
			"with no quotes or spaces")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.key = key
	s.state.KeyChoice = "recorded"
	if key != "" {
		s.state.KeyChoice = "live"
	}
	return s.state, s.save()
}

func (s *Server) getProfiles(r *http.Request) (any, error) {
	profiles, err := Profiles(r.Context(), s.cfg.CLI)
	if err != nil {
		return nil, err
	}
	return map[string]any{"profiles": profiles}, nil
}

func (s *Server) postSignIn(r *http.Request) (any, error) {
	var body struct{ Profile, Region, Size string }
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	s.mu.Lock()
	candidate := s.aws()
	s.mu.Unlock()
	candidate.Profile, candidate.Region = body.Profile, body.Region
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
	s.state.Size = SizeOf(body.Size).Name
	s.review = nil
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
	if err := candidate.Validate(); err != nil {
		return nil, invalid("%v", err)
	}
	err := s.start("login", func(ctx context.Context, log func(string)) error {
		log("A browser window opens for the AWS sign-in. Finish it there.")
		return candidate.Login(ctx)
	})
	return map[string]bool{"started": err == nil}, err
}

func (s *Server) postReview(r *http.Request) (any, error) {
	s.mu.Lock()
	state := s.state
	account := s.aws()
	hasKey := s.key != ""
	s.mu.Unlock()
	switch state.Target {
	case TargetLocal:
		s.mu.Lock()
		s.review = localCreates
		s.mu.Unlock()
		return map[string]bool{"started": false}, nil
	case TargetAWS:
		if state.Account == "" {
			return nil, invalid("sign in to AWS before the review")
		}
	default:
		return nil, invalid("choose where to run before the review")
	}
	err := s.start("review", func(ctx context.Context, log func(string)) error {
		log("Looking up release " + s.cfg.Version + " of the control plane.")
		binary, err := ReleaseBinary(ctx, http.DefaultClient, s.cfg.ReleaseBase, s.cfg.Version)
		if err != nil {
			return err
		}
		log("Asking CloudFormation what it would create. This creates no resource.")
		changes, err := account.Review(ctx, binary, SizeOf(state.Size), hasKey)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.review = changes
		s.mu.Unlock()
		log(fmt.Sprintf("CloudFormation would create %d resources.", len(changes)))
		return nil
	})
	return map[string]bool{"started": err == nil}, err
}

func (s *Server) postDeploy(r *http.Request) (any, error) {
	s.mu.Lock()
	state, reviewed := s.state, len(s.review) > 0
	s.mu.Unlock()
	// A local control plane that was set up before is only started again: nothing new
	// is created, so there is nothing to review.
	restart := state.Target == TargetLocal && state.Deployed
	if !reviewed && !restart {
		return nil, invalid("review what will be created before you create it")
	}
	if state.KeyChoice == "" {
		return nil, invalid("choose a model key, or the recorded answers, before you deploy")
	}
	work := s.deployLocal
	if state.Target == TargetAWS {
		work = s.deployAWS
	}
	err := s.start("deploy", work)
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
	key, env := s.key, s.state.Env
	s.mu.Unlock()
	if key != "" {
		if err := s.saveLocalKey(key); err != nil {
			return err
		}
		log("The model key is saved on this machine, readable only by you.")
	}
	if s.dashboardUp(ctx) {
		log("The control plane already runs on this machine.")
	} else {
		log("Starting the control plane on this machine.")
		if err := s.cfg.StartLocal(ctx, env); err != nil {
			return err
		}
		if err := s.waitDashboard(ctx, 2*time.Minute); err != nil {
			return err
		}
	}
	log("The control plane runs. It planned the first deployment and waits for approval.")
	return s.markDeployed()
}

// saveLocalKey writes the key to secrets.env in the data directory. `clanker up` reads it.
func (s *Server) saveLocalKey(key string) error {
	if err := os.MkdirAll(s.cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create the data directory %s: %w", s.cfg.DataDir, err)
	}
	path := filepath.Join(s.cfg.DataDir, "secrets.env")
	if err := os.WriteFile(path, []byte("OPENROUTER_API_KEY="+key+"\n"), 0o600); err != nil {
		return fmt.Errorf("save the model key in %s: %w", path, err)
	}
	if err := os.Setenv("OPENROUTER_API_KEY", key); err != nil {
		return fmt.Errorf("set the model key for this process: %w", err)
	}
	return nil
}

func (s *Server) markDeployed() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Deployed = true
	s.key = ""
	return s.save()
}

func (s *Server) deployAWS(ctx context.Context, log func(string)) error {
	s.mu.Lock()
	key, account := s.key, s.aws()
	s.mu.Unlock()
	if key != "" {
		log("Storing the model key in Parameter Store, encrypted.")
		if err := account.PutKey(ctx, key); err != nil {
			return err
		}
	}
	if err := account.Deploy(ctx, log); err != nil {
		return err
	}
	if err := s.markDeployed(); err != nil {
		return err
	}
	log("The service exists and its container passes the health check.")
	return s.connect(ctx, log)
}

// connect opens the tunnel to the control plane in the cloud and waits for the dashboard.
func (s *Server) connect(ctx context.Context, log func(string)) error {
	s.mu.Lock()
	account := s.aws()
	if s.tunnel != nil {
		s.tunnel()
		s.tunnel = nil
	}
	s.mu.Unlock()
	home, err := account.Home(ctx)
	if err != nil {
		return err
	}
	if home.Cluster == "" {
		return fmt.Errorf("the environment has no control plane yet: create it first")
	}
	if s.dashboardUp(ctx) {
		return fmt.Errorf("something already answers at %s: stop the local control plane "+
			"(`pkill -f \"clanker up\"`) and connect again", s.cfg.DashboardURL)
	}
	log("Looking for the running container.")
	task, err := account.WaitTask(ctx, home, taskWaitLimit)
	if err != nil {
		return err
	}
	tunnelCtx, cancel := context.WithCancel(s.baseCtx)
	if err := account.Tunnel(tunnelCtx, task, 7070).Start(); err != nil {
		cancel()
		return fmt.Errorf("open the tunnel to the container %s: %w", task.ID, err)
	}
	s.mu.Lock()
	s.tunnel = cancel
	s.mu.Unlock()
	log("The private tunnel is open. Waiting for the control plane to answer.")
	if err := s.waitDashboard(ctx, dashboardLimit); err != nil {
		return err
	}
	log("The control plane runs in your AWS account.")
	return nil
}

func (s *Server) postConnect(r *http.Request) (any, error) {
	s.mu.Lock()
	target := s.state.Target
	s.mu.Unlock()
	if target != TargetAWS {
		return nil, invalid("only an AWS environment needs a tunnel")
	}
	err := s.start("connect", s.connect)
	return map[string]bool{"started": err == nil}, err
}

func (s *Server) postDestroy(r *http.Request) (any, error) {
	s.mu.Lock()
	state, account := s.state, s.aws()
	s.mu.Unlock()
	if state.Target != TargetAWS {
		return nil, invalid("to remove the local environment, stop this program and run " +
			"`clanker destroy --env local`")
	}
	if state.Account == "" {
		return nil, invalid("sign in to AWS before you remove the environment")
	}
	err := s.start("destroy", func(ctx context.Context, log func(string)) error {
		s.mu.Lock()
		if s.tunnel != nil {
			s.tunnel()
			s.tunnel = nil
		}
		s.mu.Unlock()
		if err := account.Destroy(ctx, log); err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state.Deployed = false
		s.review = nil
		return s.save()
	})
	return map[string]bool{"started": err == nil}, err
}
