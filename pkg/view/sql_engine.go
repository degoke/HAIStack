package view

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
	_ "modernc.org/sqlite"
)

var sqlIdentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// SQLQueryEngine executes read-only SQL against reporting table snapshots.
type SQLQueryEngine struct {
	tables store.ReportingTableStore
}

// NewSQLQueryEngine returns an engine backed by ReportingTableStore.
func NewSQLQueryEngine(tables store.ReportingTableStore) *SQLQueryEngine {
	return &SQLQueryEngine{tables: tables}
}

// SQLQueryResult is the tabular output of one SQL query.
type SQLQueryResult struct {
	Columns []ColumnInfo     `json:"columns"`
	Rows    []map[string]any `json:"rows"`
}

// Query executes one read-only SQL statement against loaded reporting tables.
// Table names correspond to sanitized view names from reporting metadata.
func (e *SQLQueryEngine) Query(ctx context.Context, sqlText string, tables []ReportingTableRef) (*SQLQueryResult, error) {
	if e == nil || e.tables == nil {
		return nil, fmt.Errorf("%w: reporting table store is required", ErrMissingReportingTables)
	}
	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return nil, fmt.Errorf("view: sql query is required")
	}
	if !isReadOnlySQL(sqlText) {
		return nil, fmt.Errorf("%w: only read-only SELECT queries are supported", ErrUnsupportedFeature)
	}

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("open sql engine: %w", err)
	}
	defer db.Close()

	for _, ref := range tables {
		if err := e.loadTable(ctx, db, ref); err != nil {
			return nil, err
		}
	}

	rows, err := db.QueryContext(ctx, sqlText)
	if err != nil {
		return nil, fmt.Errorf("execute sql: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read sql columns: %w", err)
	}
	colInfos := make([]ColumnInfo, len(columns))
	for i, name := range columns {
		colInfos[i] = ColumnInfo{Name: name, Type: "string"}
	}

	var outRows []map[string]any
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan sql row: %w", err)
		}
		row := make(map[string]any, len(columns))
		for i, name := range columns {
			row[name] = decodeSQLValue(raw[i])
		}
		outRows = append(outRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sql rows: %w", err)
	}
	return &SQLQueryResult{Columns: colInfos, Rows: outRows}, nil
}

// ReportingTableRef identifies one reporting table to load into the SQL engine.
type ReportingTableRef struct {
	ViewName    string
	ViewVersion string
	TableName   string
}

func (e *SQLQueryEngine) loadTable(ctx context.Context, db *sql.DB, ref ReportingTableRef) error {
	version := ref.ViewVersion
	if version == "" {
		version = "1.0.0"
	}
	meta, err := e.tables.GetMeta(ctx, ref.ViewName, version)
	if err != nil {
		return err
	}
	tableName := ref.TableName
	if tableName == "" {
		tableName = sanitizeSQLTableName(ref.ViewName)
	}
	if !sqlIdentPattern.MatchString(tableName) {
		return fmt.Errorf("invalid sql table name %q", tableName)
	}

	cols := make([]string, 0, len(meta.Columns))
	ddlCols := make([]string, 0, len(meta.Columns))
	for _, col := range meta.Columns {
		if !sqlIdentPattern.MatchString(col.Name) {
			return fmt.Errorf("invalid sql column name %q", col.Name)
		}
		cols = append(cols, quoteSQLIdent(col.Name))
		ddlCols = append(ddlCols, quoteSQLIdent(col.Name)+" TEXT")
	}
	if len(cols) == 0 {
		return fmt.Errorf("reporting table %s has no columns", ref.ViewName)
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s (%s)", quoteSQLIdent(tableName), strings.Join(ddlCols, ", "))); err != nil {
		return fmt.Errorf("create sql table %s: %w", tableName, err)
	}

	rows, err := e.tables.QueryRows(ctx, ref.ViewName, version)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	placeholders := make([]string, len(cols))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", quoteSQLIdent(tableName), strings.Join(cols, ", "), strings.Join(placeholders, ", "))
	for _, row := range rows {
		args := make([]any, len(meta.Columns))
		for i, col := range meta.Columns {
			args[i] = encodeSQLCell(row[col.Name])
		}
		if _, err := db.ExecContext(ctx, insertSQL, args...); err != nil {
			return fmt.Errorf("insert sql row into %s: %w", tableName, err)
		}
	}
	return nil
}

func sanitizeSQLTableName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	if name == "" {
		return "view_table"
	}
	if name[0] >= '0' && name[0] <= '9' {
		return "v_" + name
	}
	return name
}

func quoteSQLIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func encodeSQLCell(value any) any {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case string, bool, float64, float32, int, int64:
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
}

func decodeSQLValue(raw any) any {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []byte:
		s := string(v)
		var decoded any
		if json.Unmarshal(v, &decoded) == nil {
			return decoded
		}
		return s
	case string:
		var decoded any
		if json.Unmarshal([]byte(v), &decoded) == nil {
			switch decoded.(type) {
			case map[string]any, []any:
				return decoded
			}
		}
		return v
	default:
		return v
	}
}

func isReadOnlySQL(sqlText string) bool {
	upper := strings.ToUpper(strings.TrimSpace(sqlText))
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return false
	}
	for _, banned := range []string{" INSERT ", " UPDATE ", " DELETE ", " DROP ", " ALTER ", " ATTACH ", " DETACH ", " PRAGMA "} {
		if strings.Contains(" "+upper+" ", banned) {
			return false
		}
	}
	return true
}
