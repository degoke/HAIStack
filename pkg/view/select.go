package view

import (
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
)

// SelectSpec is one node in a ViewDefinition select tree. Columns are evaluated in
// the current iteration context; nested Children are cross-joined with parent rows;
// ForEach / ForEachOrNull iterate over a FHIRPath collection; UnionAll concatenates
// sibling branches.
type SelectSpec struct {
	Columns       []ColumnSpec
	Children      []SelectSpec
	ForEach       string
	ForEachOrNull string
	UnionAll      []SelectSpec

	forEachCompiled       fhirpath.CompiledExpression
	forEachOrNullCompiled fhirpath.CompiledExpression
}

func parseSelect(raw rawSelect) (SelectSpec, error) {
	if raw.ForEach != "" && raw.ForEachOrNull != "" {
		return SelectSpec{}, fmt.Errorf("%w: forEach and forEachOrNull are mutually exclusive", ErrInvalidViewDefinition)
	}

	columns, err := parseColumns(raw.Column)
	if err != nil {
		return SelectSpec{}, err
	}

	children := make([]SelectSpec, 0, len(raw.Select))
	for i, child := range raw.Select {
		parsed, err := parseSelect(child)
		if err != nil {
			return SelectSpec{}, fmt.Errorf("nested select %d: %w", i, err)
		}
		children = append(children, parsed)
	}

	unionAll := make([]SelectSpec, 0, len(raw.UnionAll))
	for i, branch := range raw.UnionAll {
		parsed, err := parseSelect(branch)
		if err != nil {
			return SelectSpec{}, fmt.Errorf("unionAll branch %d: %w", i, err)
		}
		unionAll = append(unionAll, parsed)
	}

	sel := SelectSpec{
		Columns:       columns,
		Children:      children,
		ForEach:       raw.ForEach,
		ForEachOrNull: raw.ForEachOrNull,
		UnionAll:      unionAll,
	}
	if err := sel.validateStructure(); err != nil {
		return SelectSpec{}, err
	}
	return sel, nil
}

func parseRootSelect(raw []rawSelect) (SelectSpec, error) {
	if len(raw) == 0 {
		return SelectSpec{}, fmt.Errorf("%w: at least one select is required", ErrInvalidViewDefinition)
	}
	if len(raw) == 1 {
		return parseSelect(raw[0])
	}
	children := make([]SelectSpec, 0, len(raw))
	for i, item := range raw {
		parsed, err := parseSelect(item)
		if err != nil {
			return SelectSpec{}, fmt.Errorf("root select %d: %w", i, err)
		}
		children = append(children, parsed)
	}
	root := SelectSpec{Children: children}
	if err := root.validateStructure(); err != nil {
		return SelectSpec{}, err
	}
	return root, nil
}

func (s *SelectSpec) validateStructure() error {
	if len(s.UnionAll) > 0 {
		if len(s.Columns) > 0 || len(s.Children) > 0 || s.ForEach != "" || s.ForEachOrNull != "" {
			return fmt.Errorf("%w: unionAll cannot be combined with column, select, forEach, or forEachOrNull on the same block", ErrInvalidViewDefinition)
		}
		for i, branch := range s.UnionAll {
			if err := branch.validateStructure(); err != nil {
				return fmt.Errorf("unionAll branch %d: %w", i, err)
			}
		}
		return nil
	}

	if s.ForEach != "" || s.ForEachOrNull != "" {
		if len(s.Children) > 1 {
			return fmt.Errorf("%w: forEach blocks support at most one nested select child", ErrInvalidViewDefinition)
		}
	}

	if len(s.Columns) == 0 && len(s.Children) == 0 && s.ForEach == "" && s.ForEachOrNull == "" {
		return fmt.Errorf("%w: select block must define column, select, forEach, forEachOrNull, or unionAll", ErrInvalidViewDefinition)
	}
	for i := range s.Children {
		if err := s.Children[i].validateStructure(); err != nil {
			return fmt.Errorf("nested select %d: %w", i, err)
		}
	}
	return nil
}

func (s *SelectSpec) compile(engine fhirpath.Engine) error {
	if s.ForEach != "" {
		compiled, err := engine.Compile(s.ForEach)
		if err != nil {
			return fmt.Errorf("compile forEach %q: %w", s.ForEach, err)
		}
		s.forEachCompiled = compiled
	}
	if s.ForEachOrNull != "" {
		compiled, err := engine.Compile(s.ForEachOrNull)
		if err != nil {
			return fmt.Errorf("compile forEachOrNull %q: %w", s.ForEachOrNull, err)
		}
		s.forEachOrNullCompiled = compiled
	}
	for i := range s.Columns {
		compiled, err := engine.Compile(s.Columns[i].Path)
		if err != nil {
			return fmt.Errorf("compile column %q: %w", s.Columns[i].Name, err)
		}
		s.Columns[i].compiled = compiled
	}
	for i := range s.Children {
		if err := s.Children[i].compile(engine); err != nil {
			return err
		}
	}
	for i := range s.UnionAll {
		if err := s.UnionAll[i].compile(engine); err != nil {
			return err
		}
	}
	return nil
}

// FlattenColumns returns output columns in tree order for metadata.
func (s *SelectSpec) FlattenColumns() []ColumnSpec {
	if len(s.UnionAll) > 0 {
		return s.UnionAll[0].FlattenColumns()
	}
	cols := make([]ColumnSpec, 0, len(s.Columns))
	cols = append(cols, s.Columns...)
	for i := range s.Children {
		cols = append(cols, s.Children[i].FlattenColumns()...)
	}
	return cols
}

func validateUniqueColumnNames(root SelectSpec) error {
	if len(root.UnionAll) > 0 {
		return validateUnionAllColumns(root.UnionAll)
	}
	seen := make(map[string]struct{})
	var walk func(SelectSpec) error
	walk = func(sel SelectSpec) error {
		if len(sel.UnionAll) > 0 {
			return validateUnionAllColumns(sel.UnionAll)
		}
		for _, col := range sel.Columns {
			if _, ok := seen[col.Name]; ok {
				return fmt.Errorf("%w: duplicate column name %q", ErrInvalidViewDefinition, col.Name)
			}
			seen[col.Name] = struct{}{}
		}
		for _, child := range sel.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}

func validateUnionAllColumns(branches []SelectSpec) error {
	if len(branches) == 0 {
		return fmt.Errorf("%w: unionAll requires at least one branch", ErrInvalidViewDefinition)
	}
	expected := columnNames(branches[0].FlattenColumns())
	for i := 1; i < len(branches); i++ {
		got := columnNames(branches[i].FlattenColumns())
		if !sameColumnNames(expected, got) {
			return fmt.Errorf("%w: unionAll branch %d columns %v do not match %v", ErrInvalidViewDefinition, i, got, expected)
		}
	}
	return validateUniqueColumnNames(branches[0])
}

func columnNames(cols []ColumnSpec) []string {
	names := make([]string, len(cols))
	for i, col := range cols {
		names[i] = col.Name
	}
	return names
}

func sameColumnNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *ViewSpec) compileSelectTree(engine fhirpath.Engine) error {
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
	if err := s.RootSelect.compile(engine); err != nil {
		s.compileErr = err
		s.compiled = true
		return s.compileErr
	}
	s.compiled = true
	return nil
}

// compileSelectOnce guards recursive compile without duplicating filter compile logic.
func (s *ViewSpec) compileSelectOnce(engine fhirpath.Engine) error {
	s.mu.RLock()
	if s.compiled {
		err := s.compileErr
		s.mu.RUnlock()
		return err
	}
	s.mu.RUnlock()
	return s.compileSelectTree(engine)
}

// syncColumnsFromRoot updates the legacy Columns slice from the select tree.
func (s *ViewSpec) syncColumnsFromRoot() {
	s.Columns = s.RootSelect.FlattenColumns()
}
