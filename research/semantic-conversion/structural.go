package semanticconversion

import "encoding/json"

// structuralOK requires Convert output to equal gold R5 except gold-only
// meta.source (the authored oracle stamp Convert does not emit). Unchanged
// pairs have no such stamp. Transformed pairs do. Copy-through fields (id,
// subject, reason, and the rest of the document) are part of that equality,
// so dropping them fails structural even when remap stamps are present.
func structuralOK(pair Pair, got json.RawMessage) (bool, string, error) {
	want, err := goldDocumentForConvert(pair.R5)
	if err != nil {
		return false, "", err
	}
	eq, err := jsonEqual(got, want)
	if err != nil {
		return false, "", err
	}
	if !eq {
		return false, "converted R5 does not match gold except gold-only meta.source", nil
	}
	return true, "", nil
}

// goldDocumentForConvert returns the Convert-comparable gold document: the
// authored R5 with meta.source removed (and meta dropped if it is then empty).
func goldDocumentForConvert(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	meta, ok := obj["meta"].(map[string]any)
	if !ok {
		return canonicalJSON(obj)
	}
	delete(meta, "source")
	if len(meta) == 0 {
		delete(obj, "meta")
	} else {
		obj["meta"] = meta
	}
	return canonicalJSON(obj)
}

func canonicalJSON(v any) (json.RawMessage, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func goldHasAuthoredSource(raw json.RawMessage, spec string) bool {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	meta, _ := obj["meta"].(map[string]any)
	src, _ := meta["source"].(string)
	return src == spec
}
