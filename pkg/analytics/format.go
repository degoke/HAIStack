package analytics

// ExportFormat selects export sink encoding.
type ExportFormat string

const (
	FormatJSON    ExportFormat = "json"
	FormatNDJSON  ExportFormat = "ndjson"
	FormatCSV     ExportFormat = "csv"
	FormatParquet ExportFormat = "parquet"
)

// ParseExportFormat maps SQL-on-FHIR _format values to ExportFormat.
func ParseExportFormat(raw string) ExportFormat {
	switch raw {
	case "csv":
		return FormatCSV
	case "parquet":
		return FormatParquet
	case "json":
		return FormatJSON
	case "ndjson", "nd-json", "nd", "":
		return FormatNDJSON
	default:
		return FormatNDJSON
	}
}
