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
	if !slices.Equal(services, app.TopologyNodes()) {
		t.Errorf("services = %v, want %v", services, app.TopologyNodes())
	}
	for _, edge := range platformEdges {
		if !slices.Contains(pack.PlatformNodes, edge[0]) ||
			!slices.Contains(pack.PlatformNodes, edge[1]) {
			t.Errorf("the platform edge %v names a node that the platform does not have", edge)
		}
	}
}

func TestAnUnhealthyApplicationMarksEachComponentOfThePack(t *testing.T) {
	app := packtest.Second(t)
	renamed := map[string]string{}
	for i, name := range []string{"gateway", "console", "jobs"} {
		renamed[app.Components[i].Name] = name
		app.Components[i].Name = name
	}
	for i, edge := range app.Topology.Edges {
		for end, id := range edge {
			if name, ok := renamed[id]; ok {
				app.Topology.Edges[i][end] = name
			}
		}
	}
	layers := map[string]map[string]any{"30-data": {}, app.Layer: {}}
	for _, incident := range []bool{false, true} {
		nodes := topologyNodes(monitor.Snapshot{Layers: layers}, app)
		markComponents(nodes, app, incident)
		degraded := []string{}
		for _, node := range nodes {
			_, component := app.Component(node.ID)
			if (node.Status == "degraded") != component ||
				slices.Contains(node.Badges, "incident") != (component && incident) {
				t.Errorf("incident %v: node %+v, component %v", incident, node, component)
			}
			if component {
				degraded = append(degraded, node.ID)
			}
		}
		if !slices.Equal(degraded, []string{"gateway", "console", "jobs"}) {
			t.Errorf("incident %v: degraded nodes = %v", incident, degraded)
		}
		if len(nodes) == len(degraded) {
			t.Errorf("the topology has only components: %+v", nodes)
		}
	}
}
