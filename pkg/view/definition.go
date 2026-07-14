package view

import (
	"encoding/json"
	"fmt"
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
	if len(raw.Select) > 1 {
		return nil, fmt.Errorf("%w: multiple root selects are not supported in v1", ErrUnsupportedFeature)
	}
	rootSelect := raw.Select[0]
	if err := validateNoNestedSelect(rootSelect); err != nil {
		return nil, err
	}

	columns, err := parseColumns(rootSelect.Column)
	if err != nil {
		return nil, err
	}
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

	spec := &ViewSpec{
		Name:         raw.Name,
		Version:      raw.Version,
		URL:          raw.URL,
		ResourceType: raw.Resource,
		Description:  raw.Description,
		Status:       raw.Status,
		Columns:      columns,
		Filters:      filters,
		Permissions:  raw.Permissions,
		Metadata:     raw.Metadata,
		Raw:          def,
	}
	if err := spec.compile(engine); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidViewDefinition, err)
	}
	return spec, nil
}

func validateNoNestedSelect(s rawSelect) error {
	if len(s.Select) > 0 {
		return fmt.Errorf("%w: nested select blocks are not supported in v1", ErrUnsupportedFeature)
	}
	if s.ForEach != "" {
		return fmt.Errorf("%w: forEach is not supported in v1", ErrUnsupportedFeature)
	}
	if s.ForEachOrNull != "" {
		return fmt.Errorf("%w: forEachOrNull is not supported in v1", ErrUnsupportedFeature)
	}
	if len(s.UnionAll) > 0 {
		return fmt.Errorf("%w: unionAll is not supported in v1", ErrUnsupportedFeature)
	}
	return nil
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
	Columns      []ColumnSpec
	Filters      []FilterSpec
	Permissions  []string
	Metadata     map[string]string
	Raw          []byte

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
	s.mu.RLock()
	if s.compiled {
		err := s.compileErr
		s.mu.RUnlock()
		return err
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.compiled {
		return s.compileErr
	}

	for i := range s.Filters {
		compiled, err := engine.Compile(s.Filters[i].Path)
		if err != nil {
			s.compileErr = fmt.Errorf("compile filter %q: %w", s.Filters[i].Path, err)
			s.compiled = true
			return s.compileErr
		}
		s.Filters[i].compiled = compiled
	}
	for i := range s.Columns {
		compiled, err := engine.Compile(s.Columns[i].Path)
		if err != nil {
			s.compileErr = fmt.Errorf("compile column %q: %w", s.Columns[i].Name, err)
			s.compiled = true
			return s.compileErr
		}
		s.Columns[i].compiled = compiled
	}
	s.compiled = true
	return nil
}

// ColumnNames returns the declared output column names in order.
func (s *ViewSpec) ColumnNames() []string {
	names := make([]string, len(s.Columns))
	for i, col := range s.Columns {
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
	cols := make([]ColumnInfo, len(s.Columns))
	for i, col := range s.Columns {
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
