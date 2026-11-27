package setup

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/agent"
)

// The providers of the model on AWS, for `clanker install --model-provider`.
const (
	ProviderBedrock    = "bedrock"
	ProviderOpenRouter = "openrouter"
)

// profileIDPattern is the shape of the ID of a system inference profile, such as
// us.anthropic.claude-sonnet-5-5. The ID goes into a command line and into home.tfvars.
var profileIDPattern = regexp.MustCompile(`^[a-z]+\.[a-z0-9-]+\.[A-Za-z0-9.:-]+$`)

// heavyRoles draft and review changes; the other roles explain and sort. The wizard asks
// for one model for each group.
var heavyRoles = []string{agent.RoleAuthor, agent.RoleInvestigator, agent.RoleReviewer}

// InferenceProfile is a system inference profile of Amazon Bedrock in one region.
type InferenceProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// InferenceProfiles lists the active system inference profiles of the region of the
// session: those of Anthropic first, then the others, each group by ID.
func (a AWS) InferenceProfiles(ctx context.Context) ([]InferenceProfile, error) {
	var found []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := a.query(ctx, a.Region, &found, "bedrock", "list-inference-profiles",
		"--type-equals", "SYSTEM_DEFINED", "--query",
		"inferenceProfileSummaries[].{id:inferenceProfileId,name:inferenceProfileName,"+
			"status:status}"); err != nil {
		return nil, fmt.Errorf("list the inference profiles of Amazon Bedrock in %s: %w. "+
			"Your sign-in needs bedrock:ListInferenceProfiles", a.Region, err)
	}
	profiles := []InferenceProfile{}
	for _, profile := range found {
		if profile.Status == "ACTIVE" && profileIDPattern.MatchString(profile.ID) {
			profiles = append(profiles, InferenceProfile{ID: profile.ID, Name: profile.Name})
		}
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		first, second := isAnthropic(profiles[i].ID), isAnthropic(profiles[j].ID)
		if first != second {
			return first
		}
		return profiles[i].ID < profiles[j].ID
	})
	return profiles, nil
}

func isAnthropic(id string) bool {
	_, rest, _ := strings.Cut(id, ".")
	return strings.HasPrefix(rest, "anthropic.")
}

// checkProfile reads one inference profile, as the home stack reads it for the role of
// the core, and refuses one that is not active.
func (a AWS) checkProfile(ctx context.Context, id string) error {
	var status string
	if err := a.query(ctx, a.Region, &status, "bedrock", "get-inference-profile",
		"--inference-profile-identifier", id, "--query", "status"); err != nil {
		return fmt.Errorf("read the inference profile %s in %s: %w. Choose a profile that "+
			"`aws bedrock list-inference-profiles` lists there; the home stack also reads it "+
			"with bedrock:GetInferenceProfile", id, a.Region, err)
	}
	if status != "ACTIVE" {
		return fmt.Errorf("the inference profile %s in %s is %s, not ACTIVE", id, a.Region,
			status)
	}
	return nil
}

// converseCheck makes one small call of Converse with a model, as the person of the
// profile, and returns the tokens of the answer. It proves that the account has access to
// the model. An answer that stops at the small token limit also proves it.
func (a AWS) converseCheck(ctx context.Context, id string) (int, error) {
	var answer struct {
		StopReason string `json:"stopReason"`
		Usage      struct {
			OutputTokens int `json:"outputTokens"`
		} `json:"usage"`
	}
	err := a.query(ctx, a.Region, &answer, "bedrock-runtime", "converse", "--model-id", id,
		"--messages", `[{"role":"user","content":[{"text":"Reply with the word ready."}]}]`,
		"--inference-config", `{"maxTokens":64}`)
	if err != nil {
		return 0, fmt.Errorf("call the model %s in %s: %w. The account needs access to the "+
			"model in Amazon Bedrock, and your sign-in needs bedrock:InvokeModel", id, a.Region,
			err)
	}
	return answer.Usage.OutputTokens, nil
}

// CheckBedrock checks each model of a choice with the session of the person: the profile
// exists and is active in the region, and one small call answers. It changes nothing.
func (a AWS) CheckBedrock(ctx context.Context, models map[string]string,
	log func(string)) error {
	if err := checkRoles(models); err != nil {
		return err
	}
	for _, id := range slices.Sorted(maps.Keys(distinct(models))) {
		if err := a.checkProfile(ctx, id); err != nil {
			return err
		}
		tokens, err := a.converseCheck(ctx, id)
		if err != nil {
			return err
		}
		log(fmt.Sprintf("Amazon Bedrock in %s answered with %s: %d tokens.", a.Region, id,
			tokens))
	}
	return nil
}

func distinct(models map[string]string) map[string]bool {
	ids := map[string]bool{}
	for _, id := range models {
		ids[id] = true
	}
	return ids
}

// checkRoles refuses a choice that misses a role, names another role, or names a model
// that is not the ID of an inference profile.
func checkRoles(models map[string]string) error {
	roles := agent.DefaultBedrockModels()
	for role, id := range models {
		if _, known := roles[role]; !known {
			return fmt.Errorf("the agent has no role %q: the roles are %s", role,
				strings.Join(slices.Sorted(maps.Keys(roles)), ", "))
		}
		if !profileIDPattern.MatchString(id) {
			return fmt.Errorf("the model %q of the role %s is not the ID of an inference "+
				"profile, such as us.anthropic.claude-sonnet-5-5", id, role)
		}
	}
	for role := range roles {
		if models[role] == "" {
			return fmt.Errorf("the role %s has no model", role)
		}
	}
	return nil
}

// bedrockChoice gives each role a model: the one of the choice, else the saved one, else
// the default of the role.
func bedrockChoice(chosen, saved map[string]string) map[string]string {
	models := agent.DefaultBedrockModels()
	for _, layer := range []map[string]string{saved, chosen} {
		for role, id := range layer {
			if id != "" {
				models[role] = id
			}
		}
	}
	return models
}

// bedrockSession returns the session of the sign-in in the region of the network, or why
// the step cannot read Bedrock yet.
func (s *Server) bedrockSession() (AWS, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Target != TargetAWS || s.state.Account == "" {
		return AWS{}, invalid("Amazon Bedrock in this account needs the AWS target and the " +
			"sign-in first")
	}
	if s.state.Region == "" || s.state.PlaceMode == "" {
		return AWS{}, invalid("choose the network first: Amazon Bedrock runs in its region")
	}
	return s.aws(), nil
}

// getBedrockProfiles lists the inference profiles of the region with the session of the
// person. It changes nothing.
func (s *Server) getBedrockProfiles(r *http.Request) (any, error) {
	session, err := s.bedrockSession()
	if err != nil {
		return nil, err
	}
	profiles, err := session.InferenceProfiles(r.Context())
	if err != nil {
		return nil, err
	}
	priced := []string{}
	for _, profile := range profiles {
		if agent.HasBedrockPrice(profile.ID) {
			priced = append(priced, profile.ID)
		}
	}
	return map[string]any{"region": session.Region, "profiles": profiles,
		"defaults": profileDefaults(profiles), "priced": priced}, nil
}

// profileDefaults proposes a profile for each role among the profiles of a region: the
// default of the role, else the same model under another prefix of the list, a geographic
// one before global. A role with neither gets no proposal, so the person chooses.
func profileDefaults(profiles []InferenceProfile) map[string]string {
	listed := map[string]bool{}
	for _, profile := range profiles {
		listed[profile.ID] = true
	}
	proposed := map[string]string{}
	for role, id := range agent.DefaultBedrockModels() {
		_, model, _ := strings.Cut(id, ".")
		candidates := []string{id}
		for _, profile := range profiles {
			if prefix, rest, _ := strings.Cut(profile.ID, "."); rest == model && prefix != "global" {
				candidates = append(candidates, profile.ID)
			}
		}
		candidates = append(candidates, "global."+model)
		proposed[role] = ""
		for _, candidate := range candidates {
			if listed[candidate] {
				proposed[role] = candidate
				break
			}
		}
	}
	return proposed
}

// budgetText says which daily limit of the core stops the calls of a choice. The core knows
// the list price of a few profiles; the calls of another one record no cost.
func budgetText(models map[string]string) string {
	unpriced := []string{}
	for _, id := range slices.Sorted(maps.Keys(distinct(models))) {
		if !agent.HasBedrockPrice(id) {
			unpriced = append(unpriced, id)
		}
	}
	if len(unpriced) == 0 {
		return "S.R.C. stops the model calls of a day at 5 USD at list price."
	}
	return "S.R.C. has no price for " + strings.Join(unpriced, " and ") + ", so the model " +
		"calls of a day stop at 5 million tokens of it, not at 5 USD."
}

// chooseBedrock starts the job that calls each model of the choice once, and records the
// choice when every model answers. Bedrock needs no key.
func (s *Server) chooseBedrock(models map[string]string) error {
	session, err := s.bedrockSession()
	if err != nil {
		return err
	}
	if err := checkRoles(models); err != nil {
		return invalid("%v", err)
	}
	chosen := maps.Clone(models)
	return s.start("model", func(ctx context.Context, log func(string)) error {
		log("Calling each model once in " + session.Region + " with your sign-in: " +
			modelSummary(chosen) + ".")
		if err := session.CheckBedrock(ctx, chosen, log); err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.key, s.modelChosen = "", true
		s.state.KeyChoice, s.state.BedrockModels = ModelBedrock, chosen
		log("Every model answered. Bedrock bills each token to this account. " +
			budgetText(chosen))
		return s.save()
	})
}

// clearBedrock forgets a choice of Bedrock: it belongs to the account and the region of the
// sign-in. The caller holds the lock.
func (s *Server) clearBedrock() {
	if s.state.KeyChoice == ModelBedrock {
		s.state.KeyChoice, s.state.BedrockModels, s.modelChosen = "", nil, false
	}
}

// checkModel checks the model of the core before anything is created. Amazon Bedrock gets
// one small call for each model, with the session of the person.
func (r *installRun) checkModel(ctx context.Context) error {
	switch r.in.ModelProvider {
	case "", ProviderBedrock, ProviderOpenRouter:
	default:
		return fmt.Errorf("the model provider %q does not exist: use %s or %s",
			r.in.ModelProvider, ProviderBedrock, ProviderOpenRouter)
	}
	if len(r.vars.BedrockModels) > 0 {
		r.log("The control plane calls Amazon Bedrock in %s with the role of its instance: %s. "+
			"Bedrock bills each token to this account. %s", r.vars.Region,
			modelSummary(r.vars.BedrockModels), budgetText(r.vars.BedrockModels))
		session := r.aws
		session.Region = r.vars.Region
		return session.CheckBedrock(ctx, r.vars.BedrockModels, func(line string) {
			r.log("%s", line)
		})
	}
	if r.vars.ModelKey == "" {
		r.log("No model: the control plane answers with recorded responses. Choose Amazon " +
			"Bedrock (--model-provider bedrock) or give an OpenRouter key (--model-key-file), " +
			"or use the step Language model of the wizard.")
	}
	return nil
}

// modelSummary names the models of a choice for a person: the model of the roles that
// draft and review, and the model of the others.
func modelSummary(models map[string]string) string {
	heavy, light := map[string]bool{}, map[string]bool{}
	for role, id := range models {
		if slices.Contains(heavyRoles, role) {
			heavy[id] = true
		} else {
			light[id] = true
		}
	}
	join := func(ids map[string]bool) string {
		return strings.Join(slices.Sorted(maps.Keys(ids)), " and ")
	}
	return fmt.Sprintf("%s for the Author, the Investigator and the Reviewer; %s for Triage "+
		"and the Explainer", join(heavy), join(light))
}
