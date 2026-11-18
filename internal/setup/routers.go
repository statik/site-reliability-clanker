package setup

import (
	"context"
	"slices"
	"strings"
)

// foundRouter is an ECS service that runs the Tailscale image as a subnet router.
type foundRouter struct {
	Service string
	Subnets []string
	Routes  []string
}

type ecsService struct {
	Name    string   `json:"name"`
	Task    string   `json:"task"`
	Running int      `json:"running"`
	Subnets []string `json:"subnets"`
}

type ecsContainer struct {
	Image  string `json:"image"`
	Routes string `json:"routes"`
}

// describeLimit is the number of services that one describe call of ECS takes.
const describeLimit = 10

// services returns the running services of one cluster that have subnets of their own.
func (a AWS) services(ctx context.Context, region, cluster string) ([]ecsService, error) {
	var arns []string
	if err := a.query(ctx, region, &arns, "ecs", "list-services", "--cluster", cluster,
		"--query", "serviceArns"); err != nil {
		return nil, err
	}
	running := []ecsService{}
	for chunk := range slices.Chunk(arns, describeLimit) {
		var described []ecsService
		args := append([]string{"ecs", "describe-services", "--cluster", cluster, "--services"},
			chunk...)
		args = append(args, "--query", "services[].{name:serviceName,task:taskDefinition,"+
			"running:runningCount,subnets:networkConfiguration.awsvpcConfiguration.subnets}")
		if err := a.query(ctx, region, &described, args...); err != nil {
			return nil, err
		}
		for _, service := range described {
			if service.Running > 0 && len(service.Subnets) > 0 {
				running = append(running, service)
			}
		}
	}
	return running, nil
}

// advertised returns the routes of the Tailscale container of a task definition, or nil
// when the task has none. The call reads only the image and the TS_ROUTES setting, and
// no secret of the task.
func (a AWS) advertised(ctx context.Context, region, task string) ([]string, error) {
	var containers []ecsContainer
	if err := a.query(ctx, region, &containers, "ecs", "describe-task-definition",
		"--task-definition", task, "--query", "taskDefinition.containerDefinitions[]."+
			"{image:image,routes:environment[?name=='TS_ROUTES']|[0].value}"); err != nil {
		return nil, err
	}
	for _, container := range containers {
		if !strings.Contains(container.Image, "tailscale") || container.Routes == "" {
			continue
		}
		routes := []string{}
		for _, route := range strings.Split(container.Routes, ",") {
			if route = strings.TrimSpace(route); route != "" {
				routes = append(routes, route)
			}
		}
		return routes, nil
	}
	return nil, nil
}

// routers finds the Tailscale subnet routers that run as ECS services in a region.
func (a AWS) routers(ctx context.Context, region string) ([]foundRouter, error) {
	var clusters []string
	if err := a.query(ctx, region, &clusters, "ecs", "list-clusters",
		"--query", "clusterArns"); err != nil {
		return nil, err
	}
	found := []foundRouter{}
	for _, cluster := range clusters {
		services, err := a.services(ctx, region, cluster)
		if err != nil {
			return nil, err
		}
		for _, service := range services {
			routes, err := a.advertised(ctx, region, service.Task)
			if err != nil {
				return nil, err
			}
			if len(routes) > 0 {
				found = append(found, foundRouter{Service: service.Name, Subnets: service.Subnets,
					Routes: routes})
			}
		}
	}
	return found, nil
}

// routerOf returns the router that runs in a subnet of the network, or nil.
func routerOf(n Network, routers []foundRouter) *Router {
	own := map[string]bool{}
	for _, subnet := range n.Subnets {
		own[subnet.ID] = true
	}
	for _, router := range routers {
		if !slices.ContainsFunc(router.Subnets, func(id string) bool { return own[id] }) {
			continue
		}
		return &Router{Service: router.Service, Routes: router.Routes,
			Covers: covers(router.Routes, n.CIDR)}
	}
	return nil
}

// covers reports whether one of the routes holds the whole address range.
func covers(routes []string, cidr string) bool {
	whole, ok := prefix(cidr)
	if !ok {
		return false
	}
	for _, route := range routes {
		advertised, ok := prefix(route)
		if ok && advertised.Bits() <= whole.Bits() && advertised.Contains(whole.Addr()) {
			return true
		}
	}
	return false
}
