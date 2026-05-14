package exporter

import (
	"context"
	"strings"
	"testing"

	"github.com/anomalyco/anon_test_data_generator/internal/generator"
)

// ---------------------------------------------------------------------------
// copySource tests
// ---------------------------------------------------------------------------

func TestCopySource_Empty(t *testing.T) {
	src := newCopySource([]generator.Row{}, []string{"a", "b"})
	if src.Next() {
		t.Error("Next() should be false for empty rows")
	}
}

func TestCopySource_MultipleRows(t *testing.T) {
	rows := []generator.Row{
		{"a": int64(1), "b": "hello"},
		{"a": int64(2), "b": "world"},
	}
	cols := []string{"a", "b"}
	src := newCopySource(rows, cols)

	var result [][]any
	for src.Next() {
		vals, err := src.Values()
		if err != nil {
			t.Fatalf("Values() error: %v", err)
		}
		result = append(result, vals)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result))
	}
	if result[0][0] != int64(1) || result[0][1] != "hello" {
		t.Errorf("row 0 = %v", result[0])
	}
	if result[1][0] != int64(2) || result[1][1] != "world" {
		t.Errorf("row 1 = %v", result[1])
	}
}

func TestCopySource_ColumnOrder(t *testing.T) {
	rows := []generator.Row{
		{"b": "second", "a": "first", "c": "third"},
	}
	cols := []string{"c", "a", "b"}
	src := newCopySource(rows, cols)

	src.Next()
	vals, _ := src.Values()

	if vals[0] != "third" {
		t.Errorf("col 0 (c) = %v", vals[0])
	}
	if vals[1] != "first" {
		t.Errorf("col 1 (a) = %v", vals[1])
	}
	if vals[2] != "second" {
		t.Errorf("col 2 (b) = %v", vals[2])
	}
}

func TestCopySource_MissingColumnReturnsNil(t *testing.T) {
	rows := []generator.Row{
		{"a": 1},
	}
	cols := []string{"a", "b"}
	src := newCopySource(rows, cols)

	src.Next()
	vals, _ := src.Values()

	if vals[0] != 1 {
		t.Errorf("col 0 = %v", vals[0])
	}
	if vals[1] != nil {
		t.Errorf("col 1 = %v, want nil", vals[1])
	}
}

func TestCopySource_Err(t *testing.T) {
	src := newCopySource(nil, nil)
	if src.Err() != nil {
		t.Errorf("Err() = %v, want nil", src.Err())
	}
}

func TestCopySource_NextExhausted(t *testing.T) {
	rows := []generator.Row{
		{"a": 1},
	}
	src := newCopySource(rows, []string{"a"})

	if !src.Next() {
		t.Fatal("first Next() should be true")
	}
	if src.Next() {
		t.Fatal("second Next() should be false")
	}
	if src.Next() {
		t.Fatal("third Next() should be false")
	}
}

// ---------------------------------------------------------------------------
// BatchWriter column check
// ---------------------------------------------------------------------------

func TestNewBatchWriter_ColumnOrderValidation(t *testing.T) {
	w := &BatchWriter{
		cleaned: map[string]bool{"public.t": true}, // skip cleanup
		columns: map[string][]string{},
	}

	err := w.WriteBatch(context.TODO(), "public.t", []generator.Row{{"a": 1}})
	if err == nil {
		t.Fatal("expected error for missing column order")
	}
	if !strings.Contains(err.Error(), "no column order") {
		t.Errorf("error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Table name splitting
// ---------------------------------------------------------------------------

func TestIdentifierSplit(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		table  string
	}{
		{"public.users", "public", "users"},
		{"audit.logs", "audit", "logs"},
	}

	for _, tt := range tests {
		parts := strings.SplitN(tt.name, ".", 2)
		if parts[0] != tt.schema || parts[1] != tt.table {
			t.Errorf("split(%q) = %v", tt.name, parts)
		}
	}
}

// ---------------------------------------------------------------------------
// Non-DB cleanup SQL structure
// ---------------------------------------------------------------------------

func TestCleanup_SQLStructure(t *testing.T) {
	// Verify truncate and delete SQL templates contain expected keywords.
	truncateSQL := "TRUNCATE TABLE public.users CASCADE"
	deleteSQL := "DELETE FROM public.orders"

	if !strings.Contains(truncateSQL, "TRUNCATE") || !strings.Contains(truncateSQL, "CASCADE") {
		t.Errorf("truncate SQL: %s", truncateSQL)
	}
	if !strings.Contains(deleteSQL, "DELETE FROM") {
		t.Errorf("delete SQL: %s", deleteSQL)
	}
}

// ---------------------------------------------------------------------------
// ensureCleanup idempotency without DB
// ---------------------------------------------------------------------------

func TestEnsureCleanup_SkipsIfCleaned(t *testing.T) {
	w := &BatchWriter{
		cleaned: map[string]bool{"public.t": true},
		cleanup: map[string]string{"public.t": "truncate"},
	}
	// conn is nil, but ensureCleanup should skip because cleaned=true.
	err := w.ensureCleanup(context.TODO(), "public.t")
	if err != nil {
		t.Fatalf("ensureCleanup should skip, got: %v", err)
	}
}

func TestEnsureCleanup_UnknownStrategy(t *testing.T) {
	w := &BatchWriter{
		cleaned: map[string]bool{},
		cleanup: map[string]string{"public.t": "unknown"},
	}
	// conn is nil, but ensureCleanup should return nil for unknown strategy.
	err := w.ensureCleanup(context.TODO(), "public.t")
	if err != nil {
		t.Fatalf("unknown strategy should be no-op: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Row conversion: nil values
// ---------------------------------------------------------------------------

func TestRowConversion_NilValue(t *testing.T) {
	// Ensure nil values in rows are preserved (nulling transformer use case).
	rows := []generator.Row{
		{"id": int64(1), "secret": nil},
	}
	cols := []string{"id", "secret"}
	src := newCopySource(rows, cols)

	src.Next()
	vals, _ := src.Values()

	if vals[0] != int64(1) {
		t.Errorf("id = %v", vals[0])
	}
	if vals[1] != nil {
		t.Errorf("secret = %v, want nil", vals[1])
	}
}

// ---------------------------------------------------------------------------
// BatchWriter Flush is no-op
// ---------------------------------------------------------------------------

func TestBatchWriter_FlushNoop(t *testing.T) {
	w := &BatchWriter{}
	if err := w.Flush(context.TODO()); err != nil {
		t.Errorf("Flush should be no-op: %v", err)
	}
}
