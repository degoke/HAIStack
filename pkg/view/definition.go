package view

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
)

// DefinitionParser loads and validates a FHIR ViewDefinition resource from JSON
// bytes. A FHIRPath engine is required so that all filter and column expressions
// are validated at parse time. Use NewDefinitionParser to create one.
type DefinitionParser struct {
	engine fhirpath.Engine
}

// NewDefinitionParser returns a parser that validates FHIRPath expressions with
// the supplied engine when parsing. Engine must be non-nil.
func NewDefinitionParser(engine fhirpath.Engine) (*DefinitionParser, error) {
	if engine == nil {
		return nil, ErrMissingEngine
	}
	return &DefinitionParser{engine: engine}, nil
}

// Parse loads and validates a ViewDefinition payload into a normalized ViewSpec.
func (p *DefinitionParser) Parse(def []byte) (*ViewSpec, error) {
	return ParseDefinition(def, p.engine)
}

// ParseDefinition loads and validates a ViewDefinition payload into a
// normalized ViewSpec. All filter and column FHIRPath expressions are compiled
// with engine to fail fast on invalid expressions. Engine must be non-nil.
func ParseDefinition(def []byte, engine fhirpath.Engine) (*ViewSpec, error) {
	if engine == nil {
		return nil, ErrMissingEngine
	}

	var raw rawViewDefinition
	if err := json.Unmarshal(def, &raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidViewDefinition, err)
	}
	if raw.ResourceType != "ViewDefinition" {
		return nil, fmt.Errorf("%w: resourceType is %q, want ViewDefinition", ErrInvalidViewDefinition, raw.ResourceType)
	}
	if raw.Resource == "" {
		return nil, fmt.Errorf("%w: missing required source resource", ErrInvalidViewDefinition)
	}
	if raw.Name == "" {
		return nil, fmt.Errorf("%w: missing required name", ErrInvalidViewDefinition)
	}
	if raw.Version == "" {
		return nil, fmt.Errorf("%w: missing required version", ErrInvalidViewDefinition)
	}

	if len(raw.Select) == 0 {
		return nil, fmt.Errorf("%w: at least one select is required", ErrInvalidViewDefinition)
	}
	rootSelect, err := parseRootSelect(raw.Select)
	if err != nil {
		return nil, err
	}
	if err := validateUniqueColumnNames(rootSelect); err != nil {
		return nil, err
	}
	columns := rootSelect.FlattenColumns()
	if len(columns) == 0 {
		return nil, fmt.Errorf("%w: at least one column is required", ErrInvalidViewDefinition)
	}

	filters, err := parseFilters(raw.Where)
	if err != nil {
		return nil, err
	}

	if raw.Metadata == nil {
		raw.Metadata = map[string]string{}
	}

	materialize, materializeKey := parseMaterializeMetadata(raw.Metadata)
	searchParams, searchMode := parseSearchMetadata(raw.Metadata)

	spec := &ViewSpec{
		Name:           raw.Name,
		Version:        raw.Version,
		URL:            raw.URL,
		ResourceType:   raw.Resource,
		Description:    raw.Description,
		Status:         raw.Status,
		RootSelect:     rootSelect,
		Columns:        columns,
		Filters:        filters,
		Permissions:    raw.Permissions,
		Metadata:       raw.Metadata,
		Materialize:    materialize,
		MaterializeKey: materializeKey,
		SearchParams:   searchParams,
		SearchMode:     searchMode,
		Raw:            def,
	}
	if err := spec.compileSelectTree(engine); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidViewDefinition, err)
	}
	return spec, nil
}

func parseMaterializeMetadata(metadata map[string]string) (enabled bool, keyColumn string) {
	if metadata == nil {
		return false, ""
	}
	switch strings.ToLower(strings.TrimSpace(metadata["materialize"])) {
	case "1", "true", "yes":
		enabled = true
	}
	keyColumn = strings.TrimSpace(metadata["materializeKey"])
	return enabled, keyColumn
}

func parseColumns(rawCols []rawColumn) ([]ColumnSpec, error) {
	seen := make(map[string]struct{}, len(rawCols))
	cols := make([]ColumnSpec, 0, len(rawCols))
	for i, rc := range rawCols {
		if rc.Name == "" {
			return nil, fmt.Errorf("%w: column %d missing name", ErrInvalidViewDefinition, i)
		}
		if rc.Path == "" {
			return nil, fmt.Errorf("%w: column %q missing path", ErrInvalidViewDefinition, rc.Name)
		}
		if _, ok := seen[rc.Name]; ok {
			return nil, fmt.Errorf("%w: duplicate column name %q", ErrInvalidViewDefinition, rc.Name)
		}
		seen[rc.Name] = struct{}{}
		cols = append(cols, ColumnSpec{
			Name:        rc.Name,
			Path:        rc.Path,
			Type:        rc.Type,
			Description: rc.Description,
			Collection:  rc.Collection,
		})
	}
	return cols, nil
}

func parseFilters(rawWheres []rawWhere) ([]FilterSpec, error) {
	filters := make([]FilterSpec, 0, len(rawWheres))
	for i, rw := range rawWheres {
		if rw.Path == "" {
			return nil, fmt.Errorf("%w: where clause %d missing path", ErrInvalidViewDefinition, i)
		}
		filters = append(filters, FilterSpec{
			Path:        rw.Path,
			Description: rw.Description,
		})
	}
	return filters, nil
}

// ViewSpec is a normalized internal representation of one parsed FHIR
// ViewDefinition, including source resource type, compiled filter expressions,
// compiled column expressions, declared permissions, and optional metadata tags.
type ViewSpec struct {
	Name         string
	Version      string
	URL          string
	ResourceType string
	Description  string
	Status       string
	RootSelect   SelectSpec
	Columns      []ColumnSpec
	Filters      []FilterSpec
	Permissions  []string
	Metadata     map[string]string
	// Materialize enables writing rows to MaterializedViewStore when configured
	// on the Executor. MaterializeKey names the output column used as the row key.
	Materialize    bool
	MaterializeKey string
	SearchParams   url.Values
	SearchMode     SearchMode
	Raw            []byte

	mu         sync.RWMutex
	compiled   bool
	compileErr error
}

// ColumnSpec describes one output column.
type ColumnSpec struct {
	Name        string
	Path        string
	Type        string
	Description string
	Collection  bool

	compiled fhirpath.CompiledExpression
}

// FilterSpec describes one root filter predicate.
type FilterSpec struct {
	Path        string
	Description string

	compiled fhirpath.CompiledExpression
}

// compile compiles all filter and column FHIRPath expressions using the supplied
// engine. It is safe for concurrent use and caches the result. The returned
// error is the same on every call for a given spec.
func (s *ViewSpec) compile(engine fhirpath.Engine) error {
	return s.compileSelectOnce(engine)
}

// ColumnNames returns the declared output column names in order.
func (s *ViewSpec) ColumnNames() []string {
	cols := s.Columns
	if len(cols) == 0 {
		cols = s.RootSelect.FlattenColumns()
	}
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = col.Name
	}
	return names
}

// ColumnInfo describes one output column for result metadata.
type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// ColumnInfos returns column metadata for the view.
func (s *ViewSpec) ColumnInfos() []ColumnInfo {
	flat := s.Columns
	if len(flat) == 0 {
		flat = s.RootSelect.FlattenColumns()
	}
	cols := make([]ColumnInfo, len(flat))
	for i, col := range flat {
		cols[i] = ColumnInfo{Name: col.Name, Type: col.Type}
	}
	return cols
}

type rawViewDefinition struct {
	ResourceType string            `json:"resourceType"`
	URL          string            `json:"url"`
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Status       string            `json:"status"`
	Description  string            `json:"description"`
	Resource     string            `json:"resource"`
	FHIRVersion  string            `json:"fhirVersion"`
	Select       []rawSelect       `json:"select"`
	Where        []rawWhere        `json:"where"`
	Permissions  []string          `json:"permissions,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type rawSelect struct {
	Column        []rawColumn `json:"column"`
	Select        []rawSelect `json:"select"`
	ForEach       string      `json:"forEach"`
	ForEachOrNull string      `json:"forEachOrNull"`
	UnionAll      []rawSelect `json:"unionAll"`
}

type rawColumn struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Collection  bool   `json:"collection"`
}

type rawWhere struct {
	Path        string `json:"path"`
	Description string `json:"description"`
}
