package view

import "strings"

// ParquetLayout selects how Parquet output is encoded.
type ParquetLayout string

const (
	// ParquetLayoutFlat exports ViewDefinition flat columns (default).
	ParquetLayoutFlat ParquetLayout = "flat"
	// ParquetLayoutFHIR exports full nested FHIR resources using Parquet-on-FHIR.
	ParquetLayoutFHIR ParquetLayout = "fhir"
)

// ParseParquetLayout maps _parquetLayout query values.
func ParseParquetLayout(raw string) ParquetLayout {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "fhir", "parquet-on-fhir", "nested":
		return ParquetLayoutFHIR
	default:
		return ParquetLayoutFlat
	}
}
