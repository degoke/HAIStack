package validate_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/validate"
)

func TestMergeProfileCatalogsLaterOverridesEarlier(t *testing.T) {
	first, err := validate.LoadProfileCatalogFromJSON([][]byte{[]byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://example.org/Profile/a",
		"type":"Patient",
		"differential":{"element":[{"path":"Patient","min":0,"max":"*"}]}
	}`)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := validate.LoadProfileCatalogFromJSON([][]byte{[]byte(`{
		"resourceType":"StructureDefinition",
		"url":"http://example.org/Profile/a",
		"type":"Patient",
		"differential":{"element":[{"path":"Patient","min":1,"max":"*"}]}
	}`)})
	if err != nil {
		t.Fatal(err)
	}
	merged := validate.MergeProfileCatalogs(first, second)
	sd, ok := merged.GetStructureDefinition("http://example.org/Profile/a")
	if !ok {
		t.Fatal("missing merged profile")
	}
	if len(sd.Elements) == 0 || sd.Elements[0].Min != 1 {
		t.Fatalf("expected later catalog to override, got %+v", sd.Elements)
	}
}
