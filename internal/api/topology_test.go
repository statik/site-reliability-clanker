package api

import (
	"slices"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/monitor"
	"github.com/statik/site-reliability-clanker/internal/pack"
	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
)

// The pack can join its services to the nodes of the platform. The list of those nodes in
// the pack package must be the list that the topology view draws.
func TestTheTopologyHasThePlatformNodesAndTheServicesOfThePack(t *testing.T) {
	app := packtest.Second(t)
	layers := map[string]map[string]any{}
	for _, layer := range []string{"10-network", "20-cluster", "30-data", "40-platform",
		app.Layer} {
		layers[layer] = map[string]any{app.VersionVariable: "3.4.0"}
	}
	platform, services := []string{}, []string{}
	for _, node := range topologyNodes(monitor.Snapshot{Layers: layers}, app) {
		if node.Layer != app.Layer {
			platform = append(platform, node.ID)
			continue
		}
		services = append(services, node.ID)
		if node.Detail != "Harbor "+node.ID || node.Version != "3.4.0" {
			t.Errorf("service node %+v", node)
		}
	}
	want := slices.Clone(pack.PlatformNodes)
	slices.Sort(want)
	slices.Sort(platform)
	if !slices.Equal(platform, want) {
		t.Errorf("platform nodes = %v, the pack package has %v", platform, want)
	}
	if !slices.Equal(services, app.Topology.Services) {
		t.Errorf("services = %v, want %v", services, app.Topology.Services)
	}
	for _, edge := range platformEdges {
		if !slices.Contains(pack.PlatformNodes, edge[0]) ||
			!slices.Contains(pack.PlatformNodes, edge[1]) {
			t.Errorf("the platform edge %v names a node that the platform does not have", edge)
		}
	}
}
