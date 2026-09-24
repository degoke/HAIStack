package ai

import (
	"bytes"
	"encoding/json"
	"sync"
)

var jsonMarshalPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

func marshalJSONPooled(v any) ([]byte, error) {
	buf := jsonMarshalPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer jsonMarshalPool.Put(buf)
	enc := json.NewEncoder(buf)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encoder.Encode adds a trailing newline; trim for FHIR parsers.
	out := buf.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return append([]byte(nil), out...), nil
}
