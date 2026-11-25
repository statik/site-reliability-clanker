package setup

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

var awsPrices = PriceList{Region: "US West (Oregon)", Checked: "2026-11-18", Lines: []PriceLine{
	{Item: "Virtual machine of the control plane", Size: "t4g.small", Hourly: 0.0168},
	{Item: "PostgreSQL database", Size: "db.t4g.micro", Hourly: 0.016},
}}

// planTagKey is the tag that every resource of S.R.C. carries. Its value is the name of
// the environment. The removal finds the resources by this tag.
const planTagKey = "clanker_env"

// PlanItem is one thing that S.R.C. creates on AWS.
type PlanItem struct {
	What   string `json:"what"`
	Detail string `json:"detail"`
}

// AWSPlan is what the review shows for AWS: what S.R.C. creates, where, with which tag
// and at which price. The wizard creates none of it.
type AWSPlan struct {
	Network  string     `json:"network"`
	Access   string     `json:"access"`
	Creates  []PlanItem `json:"creates"`
	TagKey   string     `json:"tag_key"`
	TagValue string     `json:"tag_value"`
	Prices   PriceList  `json:"prices"`
	Weekly   float64    `json:"weekly_usd"`
	// NotPriced names what the estimate leaves out.
	NotPriced []string `json:"not_priced"`
}

// awsPlan returns the plan for the choices of the state, or nil before the network is
// chosen.
func awsPlan(state State) *AWSPlan {
	if state.Target != TargetAWS || state.PlaceMode == "" {
		return nil
	}
	plan := AWSPlan{Network: "The network " + state.Network + ", which exists. S.R.C. " +
		"changes nothing in it.", TagKey: planTagKey, TagValue: state.Env,
		Prices:    PriceList{Region: awsPrices.Region, Checked: awsPrices.Checked},
		NotPriced: []string{"the storage of the database, of the machine image and of its snapshot"}}
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
	plan.Creates = append(plan.Creates,
		PlanItem{"One virtual machine", "t4g.small, in an auto scaling group of one, which " +
			"replaces a machine that fails. It runs the control plane as a Nanos unikernel."},
		PlanItem{"One PostgreSQL database", "db.t4g.micro. It holds all state of the control plane."},
		PlanItem{"One machine image and its snapshot", "built on this machine with the ops tool"},
		PlanItem{"Roles", "one for the virtual machine and one for the short-lived runners " +
			"on Fargate, which run the jobs"},
		PlanItem{"Parameters", "the settings of the control plane, in Parameter Store " +
			"below " + parameterPath(state.Env)},
		PlanItem{"One security group", "it admits only " + router})
	for _, line := range awsPrices.Lines {
		line.Weekly = line.Hourly * hoursEachWeek
		plan.Prices.Lines = append(plan.Prices.Lines, line)
		plan.Weekly += line.Weekly
	}
	return &plan
}
