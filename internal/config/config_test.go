package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubRegistry is a GeneratorRegistry backed by a set of names.
type stubRegistry map[string]bool

func (s stubRegistry) IsRegistered(name string) bool { return s[name] }

var knownGenerators = stubRegistry{
	"autoincrement":            true,
	"person.name":              true,
	"internet.email":           true,
	"phone.mobile":             true,
	"time.date":                true,
	"time.timestamp":           true,
	"finance.amount":           true,
	"finance.credit_card":      true,
	"uuid":                     true,
	"collection.random_choice": true,
	"relation.foreign_key":     true,
	"static.value":             true,
}

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

func TestLoad_ValidConfig(t *testing.T) {
	path := writeTempYAML(t, `
global:
  dsn: "postgres://localhost:5432/db"
  seed: 42
tables:
  - name: public.users
    count: 50
    columns:
      - name: id
        generator: autoincrement
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Global.DSN != "postgres://localhost:5432/db" {
		t.Errorf("DSN = %q", cfg.Global.DSN)
	}
	if cfg.Global.Seed != 42 {
		t.Errorf("Seed = %d", cfg.Global.Seed)
	}
	if cfg.Global.Locale != "en_US" {
		t.Errorf("Locale = %q, want default en_US", cfg.Global.Locale)
	}
	if cfg.Global.BatchSize != 1000 {
		t.Errorf("BatchSize = %d, want default 1000", cfg.Global.BatchSize)
	}
	if len(cfg.Tables) != 1 {
		t.Fatalf("len(Tables) = %d", len(cfg.Tables))
	}
	if cfg.Tables[0].Name != "public.users" {
		t.Errorf("Tables[0].Name = %q", cfg.Tables[0].Name)
	}
	if cfg.Tables[0].Count != 50 {
		t.Errorf("Tables[0].Count = %d", cfg.Tables[0].Count)
	}
	if len(cfg.Tables[0].Columns) != 1 {
		t.Fatalf("len(Columns) = %d", len(cfg.Tables[0].Columns))
	}
	if cfg.Tables[0].Columns[0].Name != "id" {
		t.Errorf("Columns[0].Name = %q", cfg.Tables[0].Columns[0].Name)
	}
	if cfg.Tables[0].Columns[0].Generator != "autoincrement" {
		t.Errorf("Columns[0].Generator = %q", cfg.Tables[0].Columns[0].Generator)
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	path := writeTempYAML(t, `
global:
  dsn: "postgres://localhost:5432/db"
  seed: 1
tables:
  - name: t1
    count: 1
    columns:
      - name: c1
        generator: autoincrement
`)

	cfg, _ := Load(path)

	if cfg.Global.Locale != "en_US" {
		t.Errorf("Locale = %q, want en_US", cfg.Global.Locale)
	}
	if cfg.Global.BatchSize != 1000 {
		t.Errorf("BatchSize = %d, want 1000", cfg.Global.BatchSize)
	}
}

func TestLoad_NonExistentFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeTempYAML(t, "{{{ this is not valid yaml")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

// ---------------------------------------------------------------------------
// Validate
// ---------------------------------------------------------------------------

func TestValidate_HappyPath(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			DSN:       "postgres://localhost:5432/db",
			Seed:      42,
			Locale:    "en_US",
			BatchSize: 500,
		},
		Tables: []TableRule{
			{
				Name:            "public.users",
				Count:           1000,
				CleanupStrategy: "truncate",
				Columns: []ColumnRule{
					{Name: "id", Generator: "autoincrement"},
					{Name: "name", Generator: "person.name"},
				},
			},
		},
	}
	errs := Validate(cfg, knownGenerators)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_HappyPathMinimal(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			DSN:       "postgres://localhost:5432/db",
			Seed:      1,
			Locale:    "en_US",
			BatchSize: 1000,
		},
		Tables: []TableRule{
			{
				Name:  "t",
				Count: 1,
				Columns: []ColumnRule{
					{Name: "c", Generator: "uuid"},
				},
			},
		},
	}
	errs := Validate(cfg, nil) // nil registry skips generator checks
	if len(errs) > 0 {
		t.Fatalf("unexpected errors:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_NilConfig(t *testing.T) {
	errs := Validate(nil, nil)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if !strings.Contains(errs[0].Error(), "nil") {
		t.Errorf("expected nil error, got: %v", errs[0])
	}
}

func TestValidate_MissingGlobalFields(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{},
		Tables: []TableRule{
			{Name: "t", Count: 1, Columns: []ColumnRule{{Name: "c", Generator: "uuid"}}},
		},
	}
	errs := Validate(cfg, nil)
	if len(errs) < 3 {
		t.Fatalf("expected at least 3 errors (dsn, seed, batch_size), got %d", len(errs))
	}
}

func TestValidate_NoTables(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1},
	}
	errs := Validate(cfg, nil)
	find := containsMsg(errs, "at least one table is required")
	if !find {
		t.Fatalf("expected 'at least one table' error, got:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_TableWithoutName(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1},
		Tables: []TableRule{
			{Count: 5, Columns: []ColumnRule{{Name: "c", Generator: "uuid"}}},
		},
	}
	errs := Validate(cfg, nil)
	find := containsMsg(errs, "name")
	if !find {
		t.Fatalf("expected error about table name, got:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_DuplicateTable(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1},
		Tables: []TableRule{
			{Name: "users", Count: 1, Columns: []ColumnRule{{Name: "c1", Generator: "uuid"}}},
			{Name: "users", Count: 1, Columns: []ColumnRule{{Name: "c2", Generator: "uuid"}}},
		},
	}
	errs := Validate(cfg, nil)
	find := containsMsg(errs, "duplicate table")
	if !find {
		t.Fatalf("expected duplicate table error, got:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_CountZero(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1},
		Tables: []TableRule{
			{Name: "users", Count: 0, Columns: []ColumnRule{{Name: "c", Generator: "uuid"}}},
		},
	}
	errs := Validate(cfg, nil)
	find := containsMsg(errs, "count")
	if !find {
		t.Fatalf("expected count error, got:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_NegativeBatchSize(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1, BatchSize: -1},
		Tables: []TableRule{
			{Name: "t", Count: 1, Columns: []ColumnRule{{Name: "c", Generator: "uuid"}}},
		},
	}
	errs := Validate(cfg, nil)
	find := containsMsg(errs, "batch_size")
	if !find {
		t.Fatalf("expected batch_size error, got:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_InvalidCleanupStrategy(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1},
		Tables: []TableRule{
			{Name: "users", Count: 1, CleanupStrategy: "drop", Columns: []ColumnRule{{Name: "c", Generator: "uuid"}}},
		},
	}
	errs := Validate(cfg, nil)
	find := containsMsg(errs, "cleanup_strategy")
	if !find {
		t.Fatalf("expected cleanup_strategy error, got:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_NoColumns(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1},
		Tables: []TableRule{
			{Name: "users", Count: 1},
		},
	}
	errs := Validate(cfg, nil)
	find := containsMsg(errs, "at least one column")
	if !find {
		t.Fatalf("expected column error, got:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_ColumnWithoutName(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1},
		Tables: []TableRule{
			{Name: "users", Count: 1, Columns: []ColumnRule{{Generator: "uuid"}}},
		},
	}
	errs := Validate(cfg, nil)
	find := containsMsg(errs, "column")
	if !find {
		t.Fatalf("expected column name error, got:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_DuplicateColumn(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1},
		Tables: []TableRule{
			{Name: "users", Count: 1, Columns: []ColumnRule{
				{Name: "email", Generator: "internet.email"},
				{Name: "email", Generator: "static.value"},
			}},
		},
	}
	errs := Validate(cfg, nil)
	find := containsMsg(errs, "duplicate column")
	if !find {
		t.Fatalf("expected duplicate column error, got:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_MissingGenerator(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1},
		Tables: []TableRule{
			{Name: "users", Count: 1, Columns: []ColumnRule{{Name: "email"}}},
		},
	}
	errs := Validate(cfg, nil)
	find := containsMsg(errs, "generator")
	if !find {
		t.Fatalf("expected generator error, got:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_UnknownGenerator(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1},
		Tables: []TableRule{
			{Name: "users", Count: 1, Columns: []ColumnRule{{Name: "email", Generator: "nonexistent.gen"}}},
		},
	}
	errs := Validate(cfg, knownGenerators)
	find := containsMsg(errs, "not a registered generator")
	if !find {
		t.Fatalf("expected 'not a registered generator' error, got:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_UnknownTransformer(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1},
		Tables: []TableRule{
			{Name: "users", Count: 1, Columns: []ColumnRule{{Name: "phone", Generator: "phone.mobile", Transformer: "super.hash"}}},
		},
	}
	errs := Validate(cfg, knownGenerators)
	find := containsMsg(errs, "transformer")
	if !find {
		t.Fatalf("expected transformer error, got:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_ColumnParamsPreserved(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1, Locale: "en_US", BatchSize: 1000},
		Tables: []TableRule{
			{Name: "users", Count: 1, Columns: []ColumnRule{
				{Name: "age", Generator: "static.value", Params: map[string]any{"value": 25}},
			}},
		},
	}
	errs := Validate(cfg, knownGenerators)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors:\n%s", ValidationErrorList(errs))
	}
}

func TestValidate_ValidTransformers(t *testing.T) {
	for _, tr := range []string{"masking.partial", "nulling"} {
		cfg := &Config{
			Global: GlobalConfig{DSN: "postgres://localhost:5432/db", Seed: 1, Locale: "en_US", BatchSize: 1000},
			Tables: []TableRule{
				{Name: "users", Count: 1, Columns: []ColumnRule{
					{Name: "x", Generator: "uuid", Transformer: tr},
				}},
			},
		}
		errs := Validate(cfg, nil)
		if len(errs) > 0 {
			t.Errorf("transformer %q should be valid, got errors:\n%s", tr, ValidationErrorList(errs))
		}
	}
}

func TestValidationErrorList_Empty(t *testing.T) {
	if s := ValidationErrorList(nil); s != "" {
		t.Errorf("expected empty string, got %q", s)
	}
}

func TestValidationErrorList_Formatting(t *testing.T) {
	errs := []error{
		ValidationError{Field: "global.dsn", Msg: "must not be empty"},
		ValidationError{Table: "users", Column: "email", Field: "generator", Msg: "must not be empty"},
	}
	s := ValidationErrorList(errs)
	if !strings.Contains(s, "2 validation error") {
		t.Errorf("expected count, got: %s", s)
	}
	if !strings.Contains(s, "global.dsn") {
		t.Errorf("expected dsn, got: %s", s)
	}
	if !strings.Contains(s, "users.email.generator") {
		t.Errorf("expected column context, got: %s", s)
	}
}

// ---------------------------------------------------------------------------
// fixture
// ---------------------------------------------------------------------------

func TestLoad_ExampleFixture(t *testing.T) {
	cfg, err := Load("../../configs/example.yaml")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Global.DSN == "" {
		t.Error("DSN must not be empty")
	}
	if cfg.Global.Seed != 42 {
		t.Errorf("Seed = %d, want 42", cfg.Global.Seed)
	}
	if cfg.Global.Locale != "en_US" {
		t.Errorf("Locale = %q, want en_US", cfg.Global.Locale)
	}
	if cfg.Global.BatchSize != 2000 {
		t.Errorf("BatchSize = %d, want 2000", cfg.Global.BatchSize)
	}

	if len(cfg.Tables) != 2 {
		t.Fatalf("len(Tables) = %d, want 2", len(cfg.Tables))
	}

	users := cfg.Tables[0]
	if users.Name != "public.users" {
		t.Errorf("Tables[0].Name = %q", users.Name)
	}
	if users.Count != 10000 {
		t.Errorf("Tables[0].Count = %d", users.Count)
	}
	if users.CleanupStrategy != "truncate" {
		t.Errorf("Tables[0].CleanupStrategy = %q", users.CleanupStrategy)
	}
	if len(users.Columns) != 5 {
		t.Fatalf("len(users.Columns) = %d, want 5", len(users.Columns))
	}

	phoneCol := users.Columns[3]
	if phoneCol.Name != "phone_number" {
		t.Errorf("phone column name = %q", phoneCol.Name)
	}
	if phoneCol.Transformer != "masking.partial" {
		t.Errorf("phone transformer = %q", phoneCol.Transformer)
	}
	pattern, ok := phoneCol.Params["pattern"]
	if !ok {
		t.Error("phone params missing 'pattern'")
	}
	if s, _ := pattern.(string); s == "" {
		t.Error("phone pattern is empty")
	}

	orders := cfg.Tables[1]
	if orders.Name != "public.orders" {
		t.Errorf("Tables[1].Name = %q", orders.Name)
	}
	if orders.Count != 50000 {
		t.Errorf("Tables[1].Count = %d", orders.Count)
	}
	if orders.CleanupStrategy != "delete" {
		t.Errorf("Tables[1].CleanupStrategy = %q", orders.CleanupStrategy)
	}
	if len(orders.Columns) != 5 {
		t.Fatalf("len(orders.Columns) = %d, want 5", len(orders.Columns))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func containsMsg(errs []error, substr string) bool {
	for _, e := range errs {
		if strings.Contains(e.Error(), substr) {
			return true
		}
	}
	return false
}
