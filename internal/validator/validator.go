package validator

import (
	"context"
	"fmt"
)

// Scanner abstracts a single database row.
type Scanner interface {
	Scan(dest ...any) error
}

// Querier abstracts a database connection for validation queries.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Scanner
}

// Rows abstracts a result set.
type Rows interface {
	Close()
	Next() bool
	Scan(dest ...any) error
	Err() error
}

// TableSpec describes a table to validate.
type TableSpec struct {
	Name          string
	ExpectedCount int
	Columns       []ColumnSpec
}

// ColumnSpec describes a column to validate.
type ColumnSpec struct {
	Name         string
	IsUnique     bool
	FK           *FKRef
}

// FKRef describes a foreign key relationship.
type FKRef struct {
	ParentTable  string
	ParentColumn string
}

// Issue represents a single validation problem.
type Issue struct {
	Table  string
	Column string
	Check  string
	Detail string
}

// Result holds all validation issues.
type Result struct {
	Issues []Issue
}

// Passed returns true if no issues were found.
func (r *Result) Passed() bool { return len(r.Issues) == 0 }

// Validator runs post-generation integrity checks.
type Validator struct {
	db     Querier
	tables []TableSpec
}

// New creates a Validator from TableSpecs.
func New(q Querier, tables []TableSpec) *Validator {
	return &Validator{db: q, tables: tables}
}

// Validate runs all checks and returns the results.
func (v *Validator) Validate(ctx context.Context) (*Result, error) {
	var result Result

	for _, ts := range v.tables {
		issues, err := v.validateRowCount(ctx, ts)
		if err != nil {
			return nil, fmt.Errorf("count check %s: %w", ts.Name, err)
		}
		result.Issues = append(result.Issues, issues...)

		for _, cs := range ts.Columns {
			if cs.FK != nil {
				issues, err := v.validateFK(ctx, ts.Name, cs)
				if err != nil {
					return nil, fmt.Errorf("FK check %s.%s: %w", ts.Name, cs.Name, err)
				}
				result.Issues = append(result.Issues, issues...)
			}
			if cs.IsUnique {
				issues, err := v.validateUnique(ctx, ts.Name, cs.Name)
				if err != nil {
					return nil, fmt.Errorf("unique check %s.%s: %w", ts.Name, cs.Name, err)
				}
				result.Issues = append(result.Issues, issues...)
			}
		}
	}

	return &result, nil
}

func (v *Validator) validateRowCount(ctx context.Context, ts TableSpec) ([]Issue, error) {
	var count int
	err := v.db.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s", ts.Name)).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count != ts.ExpectedCount {
		return []Issue{{
			Table:  ts.Name,
			Check:  "row_count",
			Detail: fmt.Sprintf("expected %d rows, got %d", ts.ExpectedCount, count),
		}}, nil
	}
	return nil, nil
}

func (v *Validator) validateFK(ctx context.Context, table string, cs ColumnSpec) ([]Issue, error) {
	sql := fmt.Sprintf(
		`SELECT 1 FROM %s c LEFT JOIN %s p ON c.%s = p.%s WHERE c.%s IS NOT NULL AND p.%s IS NULL LIMIT 1`,
		table, cs.FK.ParentTable, cs.Name, cs.FK.ParentColumn, cs.Name, cs.FK.ParentColumn,
	)
	var dummy int
	err := v.db.QueryRow(ctx, sql).Scan(&dummy)
	if err == nil {
		return []Issue{{
			Table:  table,
			Column: cs.Name,
			Check:  "foreign_key",
			Detail: fmt.Sprintf("orphan records found referencing %s.%s", cs.FK.ParentTable, cs.FK.ParentColumn),
		}}, nil
	}
	return nil, nil
}

func (v *Validator) validateUnique(ctx context.Context, table, column string) ([]Issue, error) {
	var count int
	sql := fmt.Sprintf(`SELECT count(*) FROM (SELECT %s FROM %s GROUP BY %s HAVING count(*) > 1) dup`, column, table, column)
	err := v.db.QueryRow(ctx, sql).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return []Issue{{
			Table:  table,
			Column: column,
			Check:  "unique",
			Detail: fmt.Sprintf("found %d duplicate group(s)", count),
		}}, nil
	}
	return nil, nil
}
