package setup

import (
	"maps"
	"slices"
	"strings"
)

// hoursEachWeek turns a price for each hour into a price for one week.
const hoursEachWeek = 168

// PriceLine is the list price of one thing that runs all the time.
type PriceLine struct {
	Item   string  `json:"item"`
	Size   string  `json:"size"`
	Hourly float64 `json:"hourly_usd"`
	Weekly float64 `json:"weekly_usd"`
}

// PriceList holds on-demand list prices of one region. A person read them from the AWS
// price pages on the date in Checked. They are in one table so that a later change can
// read them from the AWS price list.
type PriceList struct {
	Region  string      `json:"region"`
	Checked string      `json:"checked"`
	Lines   []PriceLine `json:"lines"`
}

// The lines of the load balancer, of Keycloak and of the key were read on 2026-11-25 from
// the examples of their price pages, which use the prices of US East (N. Virginia). The
// pages did not show the prices of Oregon.
var awsPrices = PriceList{Region: "US West (Oregon)", Checked: "2026-11-18", Lines: []PriceLine{
	{Item: "Virtual machine of the control plane", Size: "t4g.small", Hourly: 0.0168},
	{Item: "PostgreSQL database", Size: "db.t4g.micro", Hourly: 0.016},
	{Item: "Internal load balancer", Size: "Application Load Balancer", Hourly: 0.0225},
	{Item: "Keycloak, the identity provider", Size: "Fargate ARM64, 1 vCPU, 2 GiB",
		Hourly: 0.03238 + 2*0.00356},
	{Item: "Key of the settings", Size: "KMS customer key, 1 USD a month", Hourly: 1.0 / 730},
}}

// planTagKey is the tag that every resource of S.R.C. carries. Its value is the name of
// the environment. The removal finds the resources by this tag.
const planTagKey = "clanker_env"

// PlanItem is one thing that S.R.C. creates on AWS.
type PlanItem struct {
	What   string `json:"what"`
	Detail string `json:"detail"`
}

// AWSPlan is what the review shows for AWS: what the installer creates, where, with which
// tag and at which price, and what stops it from starting.
type AWSPlan struct {
	Network string `json:"network"`
	Access  string `json:"access"`
	// Model names the language model of the control plane, and who bills it.
	Model    string     `json:"model"`
	Creates  []PlanItem `json:"creates"`
	TagKey   string     `json:"tag_key"`
	TagValue string     `json:"tag_value"`
	Prices   PriceList  `json:"prices"`
	Weekly   float64    `json:"weekly_usd"`
	// NotPriced names what the estimate leaves out.
	NotPriced []string `json:"not_priced"`
	// Blocked says why the installer cannot start with these choices. Empty means it can.
	Blocked []string `json:"blocked"`
}

// awsPlan returns the plan for the choices of the state, or nil before the network is
// chosen.
func awsPlan(state State) *AWSPlan {
	if state.Target != TargetAWS || state.PlaceMode == "" {
		return nil
	}
	plan := AWSPlan{Network: "The network " + state.Network + ", which exists. S.R.C. " +
		"changes nothing in it.", TagKey: planTagKey, TagValue: state.Env,
		Prices: PriceList{Region: awsPrices.Region, Checked: awsPrices.Checked},
		NotPriced: []string{"the storage of the database, of the machine image and of its snapshot",
			"the capacity units of the load balancer", "the runner tasks on Fargate",
			"the transfer of the container images into the network",
			"the storage of the container images and of the two buckets"},
		Blocked: blockers(state)}
	router := "the router of your private network"
	plan.Access = "A Tailscale subnet router serves this network. You reach the control " +
		"plane through it."
	if state.Access != AccessTailscale {
		router = "a router of your private network, which you add"
		plan.Access = "The survey found no Tailscale subnet router that serves this network. " +
			"The control plane has no public address, so you need a private path to it."
	}
	if state.PlaceMode == PlaceNew {
		plan.Network = "A new network with the address range " + state.NetworkCIDR + "."
		plan.Creates = append(plan.Creates, PlanItem{"One network",
			state.NetworkCIDR + ", with private subnets and one NAT gateway"})
		plan.NotPriced = append(plan.NotPriced, "the NAT gateway of the new network")
	}
	zone := "the zone that you choose"
	if state.ZoneName != "" {
		zone = state.ZoneName
	}
	plan.Creates = append(plan.Creates,
		PlanItem{"Two names in " + zone, "src-" + state.Env + "." + zone + " for the control " +
			"plane and src-" + state.Env + "-idp." + zone + " for its identity provider, with " +
			"a TLS certificate that DNS records in the zone validate"},
		PlanItem{"One virtual machine", "t4g.small, in an auto scaling group of one, which " +
			"replaces a machine that fails. It runs the control plane as a Nanos unikernel."},
		PlanItem{"One PostgreSQL database", "db.t4g.micro. It holds all state of the control plane."},
		PlanItem{"One machine image and its snapshot", "built on this machine with the ops tool"},
		PlanItem{"One internal load balancer", "with TLS: a certificate and two names in " +
			"your DNS zone, one for the control plane and one for its identity provider"},
		PlanItem{"One identity provider", "Keycloak on Fargate with two users, for the " +
			"sign-in. S.R.C. does not manage it."},
		PlanItem{"Runners", "a container repository for the runner image and a Fargate " +
			"cluster for the short-lived runners, which run the jobs"},
		PlanItem{"Images", "the runner image and the images of the application, built on " +
			"this machine for linux/arm64, in two container repositories"},
		PlanItem{"Layer 00-bootstrap", "two buckets, for the state of the layers and for " +
			"the evidence, and the roles of the agents, which the control plane assumes"},
		PlanItem{"Log groups", "for the runners and for Keycloak, kept for 7 days"},
		PlanItem{"Roles", "one for the virtual machine, one for the runners and one for " +
			"Keycloak"},
		PlanItem{"Parameters", "the settings of the control plane, in Parameter Store " +
			"below " + parameterPath(state.Env) + ", with a key that only the control " +
			"plane and Keycloak can use: the database URL, the signing key, the sign-in, " +
			"the inputs of the layers with the required tags, and " + modelParameters(state)},
		PlanItem{"Security groups", "the load balancer admits only " + router +
			" and the addresses of the network; each other part admits only its callers"})
	plan.addModel(state)
	for _, line := range awsPrices.Lines {
		line.Weekly = line.Hourly * hoursEachWeek
		plan.Prices.Lines = append(plan.Prices.Lines, line)
		plan.Weekly += line.Weekly
	}
	return &plan
}

// modelParameters names the parameters of the language model.
func modelParameters(state State) string {
	if state.KeyChoice == ModelBedrock {
		return "the provider and the models of the language model"
	}
	return "the model key"
}

// addModel says which language model the control plane uses. Amazon Bedrock gets a line
// for the access of the role, and its tokens are not in the estimate.
func (plan *AWSPlan) addModel(state State) {
	switch state.KeyChoice {
	case ModelBedrock:
		plan.Model = "Amazon Bedrock in this account, " + state.Region + ": " +
			modelSummary(state.BedrockModels) + ". Bedrock bills each token to this account. " +
			budgetText(state.BedrockModels)
		profiles := slices.Sorted(maps.Keys(distinct(state.BedrockModels)))
		plan.Creates = append(plan.Creates, PlanItem{"Access to Amazon Bedrock", "the role " +
			"of the virtual machine may call the inference profiles " +
			strings.Join(profiles, ", ") + " in " + state.Region + " and the foundation " +
			"models that they route to. The control plane needs no model key."})
		plan.NotPriced = append(plan.NotPriced, "the tokens of the language model, which "+
			"Amazon Bedrock bills to this account for each token")
	case ModelHosted:
		plan.Model = "OpenRouter, with the key that you gave. OpenRouter bills each token."
	default:
		plan.Model = "Not chosen"
	}
}

// blockers says why the installer cannot start with the choices of the state.
func blockers(state State) []string {
	blocked := []string{}
	if state.PlaceMode == PlaceNew {
		blocked = append(blocked, "the installer does not create a network: choose a "+
			"network that exists, with a Tailscale subnet router")
	} else if state.RouterGroup == "" {
		blocked = append(blocked, "no Tailscale subnet router serves this network: the "+
			"control plane has no public address, so the installer needs one")
	}
	if state.ZoneID == "" || state.Source == "" {
		blocked = append(blocked, "choose the zone, the tags and the checkout in the step "+
			"Names and tags")
	}
	return blocked
}
