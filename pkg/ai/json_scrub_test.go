package ai

import (
	"encoding/json"
	"testing"

	"github.com/buger/jsonparser"
)

func TestJSONScrubPreservesExtensionArray(t *testing.T) {
	raw := []byte(`{"resourceType":"Patient","extension":[{"url":"http://example.org","valueString":"secret"}]}`)
	catalog := DefaultPHICatalog()
	resolve := catalogOnlyJSONResolve(catalog, "Patient")
	scrubbed, redactions, err := scrubJSONDocument(raw, catalog, DefaultRedactedValue, "Patient", resolve)
	if err != nil {
		t.Fatal(err)
	}
	if len(redactions) == 0 {
		t.Fatal("expected redactions")
	}
	if _, err := jsonparser.ArrayEach(scrubbed, func(_ []byte, _ jsonparser.ValueType, _ int, _ error) {}, "extension"); err != nil {
		t.Fatalf("extension should remain a JSON array: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(scrubbed, &m); err != nil {
		t.Fatal(err)
	}
	ext := m["extension"].([]any)[0].(map[string]any)
	if ext["valueString"] != DefaultRedactedValue {
		t.Fatalf("valueString = %v", ext["valueString"])
	}
}

func TestJSONScrubBundleEntryInOnePass(t *testing.T) {
	raw := []byte(`{"resourceType":"Bundle","type":"searchset","entry":[{"resource":{"resourceType":"Patient","id":"p1","name":[{"family":"Doe"}]}}]}`)
	catalog := DefaultPHICatalog()
	scrubbed, redactions, err := scrubJSONDocument(raw, catalog, DefaultRedactedValue, "Bundle", catalogOnlyJSONResolve(catalog, "Bundle"))
	if err != nil {
		t.Fatal(err)
	}
	if len(redactions) == 0 {
		t.Fatal("expected redactions on entry.resource")
	}
	var m map[string]any
	if err := json.Unmarshal(scrubbed, &m); err != nil {
		t.Fatal(err)
	}
	entry := m["entry"].([]any)[0].(map[string]any)
	pat := entry["resource"].(map[string]any)
	if pat["name"] != DefaultRedactedValue {
		t.Fatalf("name = %v, want redacted", pat["name"])
	}
}

func TestApplyJSONPatchesDedupesPaths(t *testing.T) {
	raw := []byte(`{"a":"x","b":"y"}`)
	paths := [][]string{{"a"}, {"a"}}
	out, err := applyJSONPatches(raw, paths, []byte(`"[redacted]"`))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"a":"[redacted]","b":"y"}` {
		t.Fatalf("got %s", out)
	}
}

func TestJsonParserPathBracketIndexes(t *testing.T) {
	got := jsonParserPath([]string{"extension", "0", "valueString"})
	want := []string{"extension", "[0]", "valueString"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestRedactionListSorted(t *testing.T) {
	s := &jsonScrubber{redactionSet: map[string]struct{}{
		"Patient.b": {},
		"Patient.a": {},
	}}
	out := s.redactionList()
	if len(out) != 2 || out[0] != "Patient.a" || out[1] != "Patient.b" {
		t.Fatalf("got %v", out)
	}
}
