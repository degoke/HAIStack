package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/buger/jsonparser"
)

func (d *FHIRDeidentifier) deidentifySearchJSON(ctx context.Context, fallbackType string, data []byte, placeholder string, toolName string) (any, []string, error) {
	catalog := d.catalog
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	resolve := d.jsonScrubResolve(ctx, toolName)
	out := data
	var redactions []string
	if err := scrubJSONArrayField(&out, "resources", fallbackType, catalog, placeholder, resolve, &redactions); err != nil {
		return nil, nil, err
	}
	if err := scrubJSONArrayField(&out, "included", "", catalog, placeholder, resolve, &redactions); err != nil {
		return nil, nil, err
	}
	if !json.Valid(out) {
		return nil, nil, fmt.Errorf("deidentify: invalid JSON after search scrub")
	}
	var root map[string]any
	if err := json.Unmarshal(out, &root); err != nil {
		return nil, nil, fmt.Errorf("deidentify: invalid search JSON: %w", err)
	}
	return root, redactions, nil
}

func scrubJSONArrayField(
	out *[]byte,
	field string,
	fallbackType string,
	catalog *PHICatalog,
	placeholder string,
	resolve jsonScrubResolve,
	redactions *[]string,
) error {
	if _, _, _, err := jsonparser.Get(*out, field); err != nil {
		return nil
	}
	var replacements []byteSpan
	var scrubbedElems [][]byte
	var scrubErr error
	_, err := jsonparser.ArrayEach(*out, func(elem []byte, dataType jsonparser.ValueType, offset int, _ error) {
		if scrubErr != nil || dataType != jsonparser.Object {
			return
		}
		rt := resourceTypeFromJSON(elem, fallbackType)
		scrubbed, r, err := scrubJSONDocument(elem, catalog, placeholder, rt, resolve, jsonScrubDocumentOpts{})
		if err != nil {
			scrubErr = err
			return
		}
		*redactions = append(*redactions, r...)
		replacements = append(replacements, byteSpan{start: offset, end: offset + len(elem)})
		scrubbedElems = append(scrubbedElems, scrubbed)
	}, field)
	if scrubErr != nil {
		return scrubErr
	}
	if err != nil {
		return err
	}
	if len(replacements) == 0 {
		return nil
	}
	merged := *out
	for i := len(replacements) - 1; i >= 0; i-- {
		sp := replacements[i]
		merged = spliceJSONBytes(merged, sp.start, sp.end, scrubbedElems[i])
	}
	*out = merged
	return nil
}
