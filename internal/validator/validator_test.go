package validator

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// mock querier
// ---------------------------------------------------------------------------

type mockQuerier struct {
	countResult  int
	fkHasOrphans bool
	dupCount     int
}

func (m *mockQuerier) Query(_ context.Context, _ string, _ ...any) (Rows, error) {
	return &mockRows{}, nil
}

func (m *mockQuerier) QueryRow(_ context.Context, sql string, _ ...any) Scanner {
	if strings.Contains(sql, "HAVING count(*) > 1") {
		return &mockScanner{val: m.dupCount}
	}
	if strings.Contains(sql, "LEFT JOIN") {
		if m.fkHasOrphans {
			return &mockScanner{val: 1}
		}
		return &mockScanner{noRows: true}
	}
	return &mockScanner{val: m.countResult}
}

type mockScanner struct {
	val    any
	noRows bool
}

func (s *mockScanner) Scan(dest ...any) error {
	if s.noRows {
		return errNoRows
	}
	if len(dest) > 0 {
		switch p := dest[0].(type) {
		case *int:
			*p = s.val.(int)
		}
	}
	return nil
}

var errNoRows = &mockError{"no rows"}

type mockError struct{ msg string }

func (e *mockError) Error() string { return e.msg }

type mockRows struct{}

func (r *mockRows) Close()                          {}
func (r *mockRows) Next() bool                      { return false }
func (r *mockRows) Scan(_ ...any) error             { return nil }
func (r *mockRows) Err() error                      { return nil }

// ---------------------------------------------------------------------------
// Tests: Validate
// ---------------------------------------------------------------------------

func TestValidate_AllPass(t *testing.T) {
	q := &mockQuerier{countResult: 10}
	v := New(q, []TableSpec{
		{
			Name:          "public.users",
			ExpectedCount: 10,
			Columns: []ColumnSpec{
				{Name: "email", IsUnique: true},
				{Name: "status"},
			},
		},
	})

	result, err := v.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if !result.Passed() {
		t.Errorf("expected all pass, got %d issues: %+v", len(result.Issues), result.Issues)
	}
	if len(result.Issues) != 0 {
		t.Errorf("expected 0 issues")
	}
}

func TestValidate_RowCountMismatch(t *testing.T) {
	q := &mockQuerier{countResult: 5}
	v := New(q, []TableSpec{
		{Name: "public.users", ExpectedCount: 10},
	})

	result, err := v.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if result.Passed() {
		t.Fatal("expected count mismatch issue")
	}
	if len(result.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result.Issues))
	}
	issue := result.Issues[0]
	if issue.Check != "row_count" || !strings.Contains(issue.Detail, "expected 10") {
		t.Errorf("issue = %+v", issue)
	}
}

func TestValidate_FK_OrphanFound(t *testing.T) {
	q := &mockQuerier{countResult: 5, fkHasOrphans: true}
	v := New(q, []TableSpec{
		{
			Name:          "public.orders",
			ExpectedCount: 5,
			Columns: []ColumnSpec{
				{Name: "user_id", FK: &FKRef{ParentTable: "public.users", ParentColumn: "id"}},
			},
		},
	})

	result, err := v.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if result.Passed() {
		t.Fatal("expected FK orphan issue")
	}
	issue := result.Issues[0]
	if issue.Check != "foreign_key" {
		t.Errorf("issue = %+v", issue)
	}
}

func TestValidate_FK_NoOrphans(t *testing.T) {
	q := &mockQuerier{countResult: 5, fkHasOrphans: false}
	v := New(q, []TableSpec{
		{
			Name:          "public.orders",
			ExpectedCount: 5,
			Columns: []ColumnSpec{
				{Name: "user_id", FK: &FKRef{ParentTable: "public.users", ParentColumn: "id"}},
			},
		},
	})

	result, err := v.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if !result.Passed() {
		t.Errorf("expected all pass, got: %+v", result.Issues)
	}
}

func TestValidate_UniqueViolation(t *testing.T) {
	q := &mockQuerier{countResult: 10, dupCount: 3}
	v := New(q, []TableSpec{
		{
			Name:          "public.users",
			ExpectedCount: 10,
			Columns: []ColumnSpec{
				{Name: "email", IsUnique: true},
			},
		},
	})

	result, err := v.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if result.Passed() {
		t.Fatal("expected unique violation")
	}
	issue := result.Issues[0]
	if issue.Check != "unique" || !strings.Contains(issue.Detail, "3 duplicate") {
		t.Errorf("issue = %+v", issue)
	}
}

func TestValidate_MultipleTables(t *testing.T) {
	q := &mockQuerier{countResult: 3, fkHasOrphans: true}
	v := New(q, []TableSpec{
		{Name: "public.users", ExpectedCount: 3},
		{Name: "public.orders", ExpectedCount: 5,
			Columns: []ColumnSpec{
				{Name: "user_id", FK: &FKRef{ParentTable: "public.users", ParentColumn: "id"}},
			}},
	})

	result, err := v.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if len(result.Issues) != 2 {
		t.Errorf("expected 2 issues (count mismatch + FK orphan), got %d", len(result.Issues))
	}
}

func TestValidate_EmptyTables(t *testing.T) {
	q := &mockQuerier{}
	v := New(q, nil)

	result, err := v.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if !result.Passed() {
		t.Errorf("expected pass with no tables, got: %+v", result.Issues)
	}
}

func TestResult_Passed(t *testing.T) {
	r := &Result{}
	if !r.Passed() {
		t.Error("empty result should pass")
	}
	r.Issues = append(r.Issues, Issue{})
	if r.Passed() {
		t.Error("result with issues should not pass")
	}
}

func TestIssue_Fields(t *testing.T) {
	issue := Issue{
		Table:  "public.users",
		Column: "email",
		Check:  "unique",
		Detail: "duplicate found",
	}
	if issue.Table != "public.users" || issue.Column != "email" || issue.Check != "unique" {
		t.Errorf("Issue fields = %+v", issue)
	}
}
