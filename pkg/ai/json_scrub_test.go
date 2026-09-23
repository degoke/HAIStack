package ai

import (
	"encoding/json"
	"testing"

	"github.com/buger/jsonparser"
)

func TestJSONScrubPreservesExtensionArray(t *testing.T) {
	raw := []byte(`{"resourceType":"Patient","extension":[{"url":"http://example.org","valueString":"secret"}]}`)
	catalog := DefaultPHICatalog()
	scrubbed, redactions, err := scrubJSONResource(
		"Patient", raw, catalog,
		catalogPathIndex(catalog, "Patient"),
		catalogPathIndex(catalog, "Patient"),
		DefaultRedactedValue, false,
	)
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

func TestJsonParserPathBracketIndexes(t *testing.T) {
	got := jsonParserPath([]string{"extension", "0", "valueString"})
	want := []string{"extension", "[0]", "valueString"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
