package parquetfhir

import (
	"fmt"
	"io"

	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/parquet-go/parquet-go"
)

const (
	// DefaultRowGroupSize controls buffered row groups for resource exports.
	DefaultRowGroupSize = 1000
)

// WriteResources encodes FHIR resources as one Parquet-on-FHIR file.
func WriteResources(w io.Writer, sd *validate.StructureDefinition, resources []map[string]any) error {
	if sd == nil {
		return fmt.Errorf("parquetfhir: StructureDefinition is required")
	}
	builder, err := NewSchemaBuilder(sd)
	if err != nil {
		return err
	}
	for _, resource := range resources {
		builder.ObserveResource(resource)
	}
	schema := builder.BuildSchema()
	writer := parquet.NewGenericWriter[map[string]any](w, schema, parquet.MaxRowsPerRowGroup(DefaultRowGroupSize))
	defer func() { _ = writer.Close() }()

	batch := make([]map[string]any, 0, len(resources))
	for _, resource := range resources {
		row, err := PrepareRow(resource)
		if err != nil {
			return err
		}
		batch = append(batch, row)
	}
	if len(batch) == 0 {
		empty, err := PrepareRow(map[string]any{"resourceType": sd.Type})
		if err != nil {
			return err
		}
		batch = append(batch, empty)
	}
	if _, err := writer.Write(batch); err != nil {
		return fmt.Errorf("parquetfhir: write rows: %w", err)
	}
	return writer.Close()
}

// ResolveStructureDefinition loads the base StructureDefinition for resourceType.
func ResolveStructureDefinition(catalog validate.ProfileCatalog, resourceType string) (*validate.StructureDefinition, error) {
	if catalog == nil {
		return nil, fmt.Errorf("parquetfhir: profile catalog is required")
	}
	url := validate.BaseStructureDefinitionURL(resourceType)
	if resolver, ok := catalog.(validate.ProfileCatalogResolver); ok {
		return resolver.ResolveStructureDefinition(url)
	}
	sd, ok := catalog.GetStructureDefinition(url)
	if !ok {
		return nil, validate.ErrProfileNotFound
	}
	return sd, nil
}
