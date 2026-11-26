package localtarget

import (
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/pack"
)

func TestTheBindingsAreInTheNamespacesOfTheTargets(t *testing.T) {
	text := string(appIdentities(pack.Targets{Namespace: "harbor", DataNamespace: "harbor-store",
		WorkloadsNamespace: "harbor-workloads"}))
	if strings.Contains(text, "{{") {
		t.Fatalf("a placeholder is left:\n%s", text)
	}
	inApp := strings.Count(text, "namespace: harbor}")
	inData := strings.Count(text, "namespace: harbor-store}")
	inWorkloads := strings.Count(text, "namespace: harbor-workloads}")
	if inApp != strings.Count(string(appIdentitiesManifest), "{{namespace}}") || inApp == 0 ||
		inData != strings.Count(string(dataIdentitiesManifest), "{{data_namespace}}") ||
		inData == 0 || inWorkloads != 1 {
		t.Errorf("%d bindings in harbor, %d in harbor-store and %d in harbor-workloads:\n%s",
			inApp, inData, inWorkloads, text)
	}
	if documents := strings.Count(text, "\nkind: "); documents != inApp+inData+inWorkloads {
		t.Errorf("%d documents for %d bindings:\n%s", documents, inApp+inData+inWorkloads,
			text)
	}
}

// The pods of the workloads namespace can have any spec. The applier of the application
// layer gets admin there, for the Role of the chart; the runbook and the debugger get
// nothing.
func TestOnlyTheApplierHasRightsInTheWorkloadsNamespace(t *testing.T) {
	text := string(appIdentities(pack.Targets{Namespace: "coder", DataNamespace: "clanker-data",
		WorkloadsNamespace: "coder-workloads"}))
	there := []string{}
	for _, document := range strings.Split(text, "---\n") {
		if strings.Contains(document, "namespace: coder-workloads}") {
			there = append(there, document)
		}
	}
	applier := "subjects:\n  - {kind: ServiceAccount, name: clanker-applier-50-app, " +
		"namespace: clanker-system}\n"
	if len(there) != 1 || !strings.Contains(there[0], "kind: RoleBinding\n") ||
		!strings.Contains(there[0], "kind: ClusterRole, name: admin}") ||
		!strings.HasSuffix(there[0], applier) {
		t.Errorf("the documents in coder-workloads are not one binding of the applier to "+
			"admin:\n%s", strings.Join(there, "---\n"))
	}
	for _, identity := range []string{"clanker-runbook", "clanker-debugger", "clanker-observer"} {
		for _, document := range there {
			if strings.Contains(document, identity) {
				t.Errorf("%s has rights in coder-workloads:\n%s", identity, document)
			}
		}
	}
}

func TestATargetWithNoDataNamespaceGetsNoBindingForOne(t *testing.T) {
	text := string(appIdentities(pack.Targets{Namespace: "harbor"}))
	if strings.Contains(text, "{{") || strings.Contains(text, "pods/exec") ||
		strings.Count(text, "\nkind: ") != strings.Count(string(appIdentitiesManifest),
			"\nkind: ") {
		t.Errorf("the bindings have a placeholder or the rights of the data namespace:\n%s", text)
	}
	if !strings.Contains(text, "name: clanker-debugger, namespace: harbor}") {
		t.Errorf("the debugger has no binding in the namespace:\n%s", text)
	}
}

// With no targets, the layer of the platform is not applied and no namespace exists.
func TestNoBindingIsMadeBeforeTheTargetsAreKnown(t *testing.T) {
	known := false
	c := Credentials{Targets: func(env string) (pack.Targets, bool) {
		return pack.Targets{Namespace: "harbor-" + env}, known
	}}
	if got := c.bindings("local"); got != nil {
		t.Errorf("bindings before the targets are known:\n%s", got)
	}
	known = true
	if got := string(c.bindings("local")); !strings.Contains(got, "namespace: harbor-local}") {
		t.Errorf("the bindings are not in the namespace of the environment:\n%s", got)
	}
}
