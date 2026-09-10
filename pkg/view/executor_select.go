package view

import (
	"context"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
)

func cloneRow(row map[string]any) map[string]any {
	if row == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[k] = v
	}
	return out
}

func (e *Executor) expandResource(ctx context.Context, sel SelectSpec, rootResource any, parentRows []map[string]any) ([]map[string]any, error) {
	if len(sel.UnionAll) > 0 {
		var unionRows []map[string]any
		for i, branch := range sel.UnionAll {
			branchRows, err := e.expandResource(ctx, branch, rootResource, nil)
			if err != nil {
				return nil, fmt.Errorf("unionAll branch %d: %w", i, err)
			}
			unionRows = append(unionRows, branchRows...)
		}
		return unionRows, nil
	}

	baseRows := parentRows
	if len(baseRows) == 0 {
		baseRows = []map[string]any{{}}
	}

	if sel.ForEach != "" || sel.ForEachOrNull != "" {
		return e.expandForEach(ctx, sel, rootResource, baseRows)
	}

	rowSet := make([]map[string]any, 0, len(baseRows))
	for _, base := range baseRows {
		row, err := e.applyColumns(ctx, sel.Columns, rootResource, cloneRow(base))
		if err != nil {
			return nil, err
		}
		rowSet = append(rowSet, row)
	}

	if len(sel.Children) == 0 {
		return rowSet, nil
	}

	var expanded []map[string]any
	for _, row := range rowSet {
		childRows := []map[string]any{row}
		for i, child := range sel.Children {
			next := make([]map[string]any, 0)
			for _, parent := range childRows {
				sub, err := e.expandResource(ctx, child, rootResource, []map[string]any{parent})
				if err != nil {
					return nil, fmt.Errorf("nested select %d: %w", i, err)
				}
				next = append(next, sub...)
			}
			childRows = next
		}
		expanded = append(expanded, childRows...)
	}
	return expanded, nil
}

func (e *Executor) expandForEach(ctx context.Context, sel SelectSpec, rootResource any, parentRows []map[string]any) ([]map[string]any, error) {
	orNull := sel.ForEachOrNull != ""
	rawItems, err := e.forEachRawValues(ctx, sel, rootResource, orNull)
	if err != nil {
		return nil, err
	}
	if len(rawItems) == 0 {
		return nil, nil
	}

	var out []map[string]any
	for _, rawItem := range rawItems {
		iterRows := make([]map[string]any, 0, len(parentRows))
		for _, base := range parentRows {
			row := cloneRow(base)
			if rawItem.Raw() == nil {
				for _, col := range sel.Columns {
					encoded, encErr := e.enc.EncodeColumn(nil, col.Collection)
					if encErr != nil {
						return nil, fmt.Errorf("column %q: %w", col.Name, encErr)
					}
					row[col.Name] = encoded
				}
			} else {
				var colErr error
				row, colErr = e.applyColumnsWithRoot(ctx, sel.Columns, rootResource, rawItem, row)
				if colErr != nil {
					return nil, colErr
				}
			}
			iterRows = append(iterRows, row)
		}

		if len(sel.Children) == 1 {
			child := sel.Children[0]
			var childOut []map[string]any
			contextResource := rootResource
			if rawItem.Raw() != nil {
				resolved, resolveErr := e.resolveIterationContext(ctx, rawItem, rootResource)
				if resolveErr != nil {
					return nil, resolveErr
				}
				if resolved != nil {
					contextResource = resolved
				}
			}
			for _, row := range iterRows {
				sub, err := e.expandResource(ctx, child, contextResource, []map[string]any{row})
				if err != nil {
					return nil, err
				}
				childOut = append(childOut, sub...)
			}
			iterRows = childOut
		} else if len(sel.Children) > 1 {
			return nil, fmt.Errorf("%w: forEach blocks support at most one nested select child", ErrInvalidViewDefinition)
		}

		out = append(out, iterRows...)
	}
	return out, nil
}

func (e *Executor) forEachRawValues(ctx context.Context, sel SelectSpec, rootResource any, orNull bool) ([]fhirpath.Value, error) {
	var (
		vals []fhirpath.Value
		err  error
	)
	if sel.ForEach != "" {
		vals, err = sel.forEachCompiled.Eval(ctx, rootResource)
	} else {
		vals, err = sel.forEachOrNullCompiled.Eval(ctx, rootResource)
	}
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 {
		if orNull {
			return []fhirpath.Value{nilValue()}, nil
		}
		return nil, nil
	}
	return vals, nil
}

func nilValue() fhirpath.Value {
	return fhirpath.NewValue(nil)
}

func (e *Executor) applyColumns(ctx context.Context, cols []ColumnSpec, resource any, row map[string]any) (map[string]any, error) {
	return e.applyColumnsWithRoot(ctx, cols, resource, resource, row)
}

func (e *Executor) applyColumnsWithRoot(ctx context.Context, cols []ColumnSpec, rootResource, contextResource any, row map[string]any) (map[string]any, error) {
	for _, col := range cols {
		values, err := e.evalColumnValues(ctx, col, rootResource, contextResource)
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", col.Name, err)
		}
		encoded, err := e.enc.EncodeColumn(values, col.Collection)
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", col.Name, err)
		}
		row[col.Name] = encoded
	}
	return row, nil
}

func (e *Executor) evalColumnValues(ctx context.Context, col ColumnSpec, rootResource, contextResource any) ([]fhirpath.Value, error) {
	if columnEvaluationResource(col.Path, rootResource, contextResource) == rootResource {
		return col.compiled.Eval(ctx, rootResource)
	}
	focus := focusValue(contextResource)
	expr := col.Path
	if strings.HasPrefix(expr, "$this.") {
		expr = strings.TrimPrefix(expr, "$this.")
	}
	return e.cfg.Engine.EvalWithEnv(ctx, "%i."+expr, rootResource, map[string]any{"i": focus})
}

func focusValue(contextResource any) any {
	switch v := contextResource.(type) {
	case fhirpath.Value:
		if v.Raw() == nil {
			return nil
		}
		return v.Raw()
	default:
		return contextResource
	}
}

func columnEvaluationResource(path string, rootResource, contextResource any) any {
	if path == "" {
		return contextResource
	}
	if strings.HasPrefix(path, "$this") || strings.HasPrefix(path, "%") {
		return contextResource
	}
	if idx := strings.Index(path, "."); idx > 0 {
		prefix := path[:idx]
		if isLikelyResourceType(prefix) {
			return rootResource
		}
	}
	return contextResource
}

func isLikelyResourceType(name string) bool {
	if len(name) == 0 {
		return false
	}
	first := name[0]
	if first < 'A' || first > 'Z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			continue
		}
		if c >= 'a' && c <= 'z' {
			continue
		}
		if c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

func (e *Executor) expandView(ctx context.Context, spec *ViewSpec, rootResource any) ([]map[string]any, error) {
	return e.expandResource(ctx, spec.RootSelect, rootResource, nil)
}
