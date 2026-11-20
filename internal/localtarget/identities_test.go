package localtarget

import (
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/pack"
)

func TestTheBindingsAreInTheNamespacesOfTheTargets(t *testing.T) {
	text := string(appIdentities(pack.Targets{Namespace: "harbor", DataNamespace: "harbor-store"}))
	if strings.Contains(text, "{{") {
		t.Fatalf("a placeholder is left:\n%s", text)
	}
	inApp := strings.Count(text, "namespace: harbor}")
	inData := strings.Count(text, "namespace: harbor-store}")
	if inApp != strings.Count(string(appIdentitiesManifest), "{{namespace}}") || inApp == 0 ||
		inData != strings.Count(string(dataIdentitiesManifest), "{{data_namespace}}") ||
		inData == 0 {
		t.Errorf("%d bindings in harbor and %d in harbor-store:\n%s", inApp, inData, text)
	}
	if documents := strings.Count(text, "\nkind: "); documents != inApp+inData {
		t.Errorf("%d documents for %d bindings:\n%s", documents, inApp+inData, text)
	}
}

func TestATargetWithNoDataNamespaceGetsNoBindingForOne(t *testing.T) {
	text := string(appIdentities(pack.Targets{Namespace: "harbor"}))
	if strings.Contains(text, "{{") || strings.Contains(text, "pods/exec") {
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
