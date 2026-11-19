package localtarget

import (
	"strings"
	"testing"

	"github.com/statik/site-reliability-clanker/internal/pack/packtest"
)

func TestTheBindingsAreInTheNamespacesOfThePack(t *testing.T) {
	second := packtest.Second(t)
	text := string(appIdentities(second))
	if strings.Contains(text, "{{") {
		t.Fatalf("a placeholder is left:\n%s", text)
	}
	inApp := strings.Count(text, "namespace: "+second.Namespace+"}")
	inData := strings.Count(text, "namespace: "+second.DataNamespace+"}")
	template := string(appIdentitiesManifest)
	if inApp != strings.Count(template, "{{namespace}}") || inApp == 0 ||
		inData != strings.Count(template, "{{data_namespace}}") || inData == 0 {
		t.Errorf("%d bindings in %s and %d in %s:\n%s", inApp, second.Namespace, inData,
			second.DataNamespace, text)
	}
	if strings.Contains(text, packtest.Example(t).Namespace) {
		t.Errorf("the bindings name the example application:\n%s", text)
	}
}
