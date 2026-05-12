package schema

import (
	"context"
	"fmt"
)

// Querier abstracts a database connection for schema introspection.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (RowScanner, error)
}

// RowScanner abstracts a result set of rows.
type RowScanner interface {
	Close()
	Next() bool
	Scan(dest ...any) error
	Err() error
}

// Introspect extracts the full database schema from the given connection.
func Introspect(ctx context.Context, q Querier) (*Schema, error) {
	s := NewSchema()

	tables, err := fetchTables(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("fetching tables: %w", err)
	}

	for _, t := range tables {
		qualified := t.Schema + "." + t.Name
		tm := &TableMeta{
			Schema:  t.Schema,
			Name:    t.Name,
			Columns: make(map[string]*ColumnMeta),
		}

		if err := fillColumns(ctx, q, tm); err != nil {
			return nil, fmt.Errorf("table %s: %w", qualified, err)
		}
		if err := fillPrimaryKey(ctx, q, tm); err != nil {
			return nil, fmt.Errorf("table %s pk: %w", qualified, err)
		}
		if err := fillUnique(ctx, q, tm); err != nil {
			return nil, fmt.Errorf("table %s unique: %w", qualified, err)
		}
		if err := fillForeignKeys(ctx, q, tm); err != nil {
			return nil, fmt.Errorf("table %s fk: %w", qualified, err)
		}

		s.Tables[qualified] = tm
	}

	return s, nil
}

// tableRef is a lightweight pair used during introspection.
type tableRef struct {
	Schema string
	Name   string
}

func fetchTables(ctx context.Context, q Querier) ([]tableRef, error) {
	rows, err := q.Query(ctx, `
		SELECT table_schema, table_name
		FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		  AND table_type = 'BASE TABLE'
		ORDER BY table_schema, table_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []tableRef
	for rows.Next() {
		var tr tableRef
		if err := rows.Scan(&tr.Schema, &tr.Name); err != nil {
			return nil, err
		}
		out = append(out, tr)
	}
	return out, rows.Err()
}

func fillColumns(ctx context.Context, q Querier, tm *TableMeta) error {
	rows, err := q.Query(ctx, `
		SELECT column_name, data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`, tm.Schema, tm.Name)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var name, dataType, nullable, defaultVal string
		if err := rows.Scan(&name, &dataType, &nullable, &defaultVal); err != nil {
			return err
		}
		tm.Columns[name] = &ColumnMeta{
			Name:         name,
			DataType:     dataType,
			IsNullable:   nullable == "YES",
			DefaultValue: defaultVal,
		}
	}
	return rows.Err()
}

func fillPrimaryKey(ctx context.Context, q Querier, tm *TableMeta) error {
	rows, err := q.Query(ctx, `
		SELECT kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_schema = kcu.constraint_schema
		  AND tc.constraint_name = kcu.constraint_name
		WHERE tc.constraint_type = 'PRIMARY KEY'
		  AND tc.table_schema = $1
		  AND tc.table_name = $2
		ORDER BY kcu.ordinal_position
	`, tm.Schema, tm.Name)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			return err
		}
		tm.PrimaryKey = append(tm.PrimaryKey, colName)
		if c, ok := tm.Columns[colName]; ok {
			c.IsPrimaryKey = true
		}
	}
	return rows.Err()
}

func fillUnique(ctx context.Context, q Querier, tm *TableMeta) error {
	rows, err := q.Query(ctx, `
		SELECT kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_schema = kcu.constraint_schema
		  AND tc.constraint_name = kcu.constraint_name
		WHERE tc.constraint_type = 'UNIQUE'
		  AND tc.table_schema = $1
		  AND tc.table_name = $2
	`, tm.Schema, tm.Name)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			return err
		}
		if c, ok := tm.Columns[colName]; ok {
			c.IsUnique = true
		}
	}
	return rows.Err()
}

func fillForeignKeys(ctx context.Context, q Querier, tm *TableMeta) error {
	rows, err := q.Query(ctx, `
		SELECT
			kcu.column_name,
			ccu.table_schema,
			ccu.table_name,
			ccu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_schema = kcu.constraint_schema
		  AND tc.constraint_name = kcu.constraint_name
		JOIN information_schema.constraint_column_usage ccu
		  ON tc.constraint_schema = ccu.constraint_schema
		  AND tc.constraint_name = ccu.constraint_name
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_schema = $1
		  AND tc.table_name = $2
	`, tm.Schema, tm.Name)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var colName, refSchema, refTable, refColumn string
		if err := rows.Scan(&colName, &refSchema, &refTable, &refColumn); err != nil {
			return err
		}
		if c, ok := tm.Columns[colName]; ok {
			c.ForeignKey = &ForeignKeyMeta{
				RefTableSchema: refSchema,
				RefTableName:   refTable,
				RefColumnName:  refColumn,
			}
		}
	}
	return rows.Err()
}
