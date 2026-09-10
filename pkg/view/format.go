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
