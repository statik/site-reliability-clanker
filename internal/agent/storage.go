package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/statik/site-reliability-clanker/internal/detect"
	"github.com/statik/site-reliability-clanker/internal/orchestrator"
)

// localVolumeNote says what the grow can and cannot do on the local target, so that the
// approver does not read the change as more than it is.
const localVolumeNote = "Limits on the local target: the database volume is a local-path " +
	"volume, and the cluster does not enforce its size. Kubernetes does not let a " +
	"StatefulSet change its volume claim, so the plan can show that it replaces the " +
	"StatefulSet. Read the plan and its tier before you approve. The change is real in the " +
	"code and in the plan; it does not add disk space on this target."

func storagePullBody(f detect.Finding, cloud, running, value string) string {
	parts := []string{
		fmt.Sprintf("What changes: %s in layer 30-data goes from %s to %s (GiB).",
			detect.StorageVariable, running, value),
		fmt.Sprintf("Why now: %s. The new size holds the used space and %d days of growth "+
			"at this rate, rounded up to a whole GiB. Code computed the size from the "+
			"samples; no model did.", f.Title, detect.StorageRunwayDays),
		"Rollback: revert this pull request before the apply. After the apply a volume " +
			"does not shrink; a smaller size needs a new volume and a restore.",
	}
	if cloud == "local" {
		parts = append(parts, localVolumeNote)
	}
	return strings.Join(parts, "\n\n")
}

// storagePull commits the new size and opens the pull request with the tools of the Author
// role.
func (a *Agent) storagePull(f detect.Finding) (*draft, error) {
	running, _ := f.Detail["running"].(string)
	value, _ := f.Detail["value"].(string)
	if running == "" || value == "" {
		return nil, fmt.Errorf("the finding does not name the size now and the new size")
	}
	d := &draft{finding: f, cloud: a.Orch.Envs[f.Env].Cloud, branch: draftBranch(f),
		typed: true}
	if _, err := a.toolSetTfvar(d, map[string]string{"layer": "30-data",
		"name": detect.StorageVariable, "value": value}); err != nil {
		return nil, err
	}
	title := fmt.Sprintf("Grow the database volume from %s GiB to %s GiB", running, value)
	if _, err := a.toolOpenPull(d, map[string]string{"title": title,
		"body": storagePullBody(f, d.cloud, running, value)}); err != nil {
		return nil, err
	}
	return d, nil
}

// DraftStorageGrowth turns a storage finding into a pull request and a planned work item.
// It is a typed change: the detector computed the new size, and this code writes it. No
// model takes part, so no model can change the number.
func (a *Agent) DraftStorageGrowth(ctx context.Context, f detect.Finding) (
	orchestrator.WorkItem, error) {
	d, err := a.storagePull(f)
	if err != nil {
		return orchestrator.WorkItem{}, fmt.Errorf("storage finding %s: %w", f.Key, err)
	}
	return a.submit(ctx, d)
}
