package parquetfhir

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/parquet-go/parquet-go"
)

const (
	// DefaultRowGroupSize controls buffered row groups for resource exports.
	DefaultRowGroupSize = 1000
)

// WriteResources encodes FHIR resources as one Parquet-on-FHIR file.
func WriteResources(w io.Writer, sd *validate.StructureDefinition, catalog validate.ProfileCatalog, resources []map[string]any) error {
	_, err := writeResourcesBatch(context.Background(), w, sd, catalog, resources)
	return err
}

// WriteResourcesStreaming performs a single store scan, observes schema from each
// resource, spills raw resources to a temp NDJSON file, then encodes parquet in
// bounded batches without holding all resources in memory. The supplied fn is
// invoked once.
func WriteResourcesStreaming(ctx context.Context, w io.Writer, sd *validate.StructureDefinition, catalog validate.ProfileCatalog, fn func(yield func(map[string]any) error) error) (int, error) {
	builder, err := NewSchemaBuilder(sd, catalog)
	if err != nil {
		return 0, err
	}

	spill, err := os.CreateTemp("", "parquetfhir-resources-*.ndjson")
	if err != nil {
		return 0, fmt.Errorf("parquetfhir: create resource spill file: %w", err)
	}
	spillPath := spill.Name()
	defer func() { _ = os.Remove(spillPath) }()

	encoder := json.NewEncoder(spill)
	if err := fn(func(raw map[string]any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		builder.ObserveResource(raw)
		if err := encoder.Encode(raw); err != nil {
			return fmt.Errorf("parquetfhir: spill resource: %w", err)
		}
		return nil
	}); err != nil {
		_ = spill.Close()
		return 0, err
	}
	if err := spill.Close(); err != nil {
		return 0, fmt.Errorf("parquetfhir: close resource spill file: %w", err)
	}

	return encodeResourcesFromSpill(ctx, w, sd, builder, spillPath)
}

func encodeResourcesFromSpill(ctx context.Context, w io.Writer, sd *validate.StructureDefinition, builder *SchemaBuilder, spillPath string) (int, error) {
	index := builder.Index()
	schema := builder.BuildSchema()
	writer := parquet.NewGenericWriter[map[string]any](w, schema, parquet.MaxRowsPerRowGroup(DefaultRowGroupSize))
	defer func() { _ = writer.Close() }()

	spill, err := os.Open(spillPath)
	if err != nil {
		return 0, fmt.Errorf("parquetfhir: open resource spill file: %w", err)
	}
	defer spill.Close()

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

	reader := bufio.NewReader(spill)
	dec := json.NewDecoder(reader)
	resourceCount := 0
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		var resource map[string]any
		if err := dec.Decode(&resource); err != nil {
			if err == io.EOF {
				break
			}
			return written, fmt.Errorf("parquetfhir: read spilled resource: %w", err)
		}
		resourceCount++
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

	if resourceCount == 0 {
		row, err := PrepareRow(map[string]any{"resourceType": sd.Type}, index)
		if err != nil {
			return 0, err
		}
		batch = append(batch, row)
	}
	if err := flush(); err != nil {
		return written, err
	}
	return written, writer.Close()
}

func writeResourcesBatch(ctx context.Context, w io.Writer, sd *validate.StructureDefinition, catalog validate.ProfileCatalog, resources []map[string]any) (int, error) {
	builder, err := NewSchemaBuilder(sd, catalog)
	if err != nil {
		return 0, err
	}
	for _, resource := range resources {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		builder.ObserveResource(resource)
	}
	index := builder.Index()
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
