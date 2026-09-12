package view

import "context"

// EncodeRunResultForTest exposes encodeRunResult for external tests.
func EncodeRunResultForTest(result *Result, format OutputFormat, header bool) ([]byte, string, error) {
	return encodeRunResult(context.Background(), result, format, header, ParquetLayoutFlat, nil, ExecuteRequest{})
}
