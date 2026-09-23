package ai

import (
	"testing"

	"github.com/degoke/haistack/pkg/validate"
)

func TestCanonicalCatalogKeyEquivalentCatalogs(t *testing.T) {
	a := DefaultPHICatalog()
	b := DefaultPHICatalog()
	if canonicalCatalogKey(a) != canonicalCatalogKey(b) {
		t.Fatal("equivalent default catalogs should share canonical key")
	}
}

func TestSharedDeidentifierSameLogicalProfileCatalog(t *testing.T) {
	profileJSON := []byte(`{
		"resourceType": "StructureDefinition",
		"url": "http://example.org/StructureDefinition/test",
		"type": "Patient",
		"kind": "resource",
		"status": "active",
		"snapshot": {"element": [{"path": "Patient"}]}
	}`)
	c1, err := validate.LoadProfileCatalogFromJSON([][]byte{profileJSON})
	if err != nil {
		t.Fatal(err)
	}
	c2, err := validate.LoadProfileCatalogFromJSON([][]byte{profileJSON})
	if err != nil {
		t.Fatal(err)
	}
	if canonicalProfileCatalogKey(c1) != canonicalProfileCatalogKey(c2) {
		t.Fatal("identical profile catalogs should share canonical key")
	}
}
