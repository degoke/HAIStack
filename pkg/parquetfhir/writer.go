package parquetfhir

import (
	"context"
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
	index, err := newElementIndex(sd)
	if err != nil {
		return err
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

	batch := make([]map[string]any, 0, len(resources))
	for _, resource := range resources {
		row, err := PrepareRow(resource, index)
		if err != nil {
			_ = writer.Close()
			return err
		}
		batch = append(batch, row)
	}
	if len(batch) == 0 {
		row, err := PrepareRow(map[string]any{"resourceType": sd.Type}, index)
		if err != nil {
			_ = writer.Close()
			return err
		}
		batch = append(batch, row)
	}
	if _, err := writer.Write(batch); err != nil {
		_ = writer.Close()
		return fmt.Errorf("parquetfhir: write rows: %w", err)
	}
	return writer.Close()
}

// WriteResourcesStreaming writes resources in batches using fn as a row source.
func WriteResourcesStreaming(ctx context.Context, w io.Writer, sd *validate.StructureDefinition, fn func(yield func(map[string]any) error) error) (int, error) {
	index, err := newElementIndex(sd)
	if err != nil {
		return 0, err
	}
	builder, err := NewSchemaBuilder(sd)
	if err != nil {
		return 0, err
	}

	var resources []map[string]any
	err = fn(func(raw map[string]any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		resources = append(resources, raw)
		builder.ObserveResource(raw)
		return nil
	})
	if err != nil {
		return 0, err
	}

	schema := builder.BuildSchema()
	writer := parquet.NewGenericWriter[map[string]any](w, schema, parquet.MaxRowsPerRowGroup(DefaultRowGroupSize))
	defer func() { _ = writer.Close() }()

	written := 0
	batch := make([]map[string]any, 0, DefaultRowGroupSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if _, err := writer.Write(batch); err != nil {
			return fmt.Errorf("parquetfhir: write rows: %w", err)
		}
		written += len(batch)
		batch = batch[:0]
		return nil
	}

	if len(resources) == 0 {
		row, err := PrepareRow(map[string]any{"resourceType": sd.Type}, index)
		if err != nil {
			return 0, err
		}
		batch = append(batch, row)
		if err := flush(); err != nil {
			return 0, err
		}
		return written, writer.Close()
	}

	for _, resource := range resources {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		row, err := PrepareRow(resource, index)
		if err != nil {
			return written, err
		}
		batch = append(batch, row)
		if len(batch) >= DefaultRowGroupSize {
			if err := flush(); err != nil {
				return written, err
			}
		}
	}
	if err := flush(); err != nil {
		return written, err
	}
	return written, writer.Close()
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
