package view

// EncodeRunResultForTest exposes encodeRunResult for external tests.
func EncodeRunResultForTest(result *Result, format OutputFormat, header bool) ([]byte, string, error) {
	return encodeRunResult(result, format, header)
}
