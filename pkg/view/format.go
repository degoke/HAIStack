package view

// OutputFormat selects SQL-on-FHIR operation output encoding.
type OutputFormat string

const (
	FormatJSON    OutputFormat = "json"
	FormatNDJSON  OutputFormat = "ndjson"
	FormatCSV     OutputFormat = "csv"
	FormatParquet OutputFormat = "parquet"
)

// ParseOutputFormat maps SQL-on-FHIR _format values to OutputFormat.
func ParseOutputFormat(raw string) OutputFormat {
	switch raw {
	case "csv":
		return FormatCSV
	case "ndjson", "nd-json", "nd":
		return FormatNDJSON
	case "parquet":
		return FormatParquet
	case "json", "fhir", "":
		return FormatJSON
	default:
		return FormatJSON
	}
}

// IsOperationOutputFormat reports whether raw selects a SQL-on-FHIR operation
// artifact encoding (csv, ndjson, parquet) rather than a FHIR response envelope.
//
// ParseOutputFormat("json") returns FormatJSON because json is a valid FHIR
// envelope for Parameters/Binary responses, but IsOperationOutputFormat("json")
// is false so Accept-header negotiation still applies to json/xml envelopes.
func IsOperationOutputFormat(raw string) bool {
	switch ParseOutputFormat(raw) {
	case FormatCSV, FormatNDJSON, FormatParquet:
		return true
	default:
		return false
	}
}
