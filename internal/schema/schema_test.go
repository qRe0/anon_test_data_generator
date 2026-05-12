package schema

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// mock querier
// ---------------------------------------------------------------------------

type mockQuerier struct {
	tables   []mockRow   // rows for table listing
	columns  []mockRow   // rows for column listing (per table)
	pk       []mockRow
	uq       []mockRow
	fk       []mockRow
	lastSQL  string
}

func (m *mockQuerier) Query(_ context.Context, sql string, args ...any) (RowScanner, error) {
	m.lastSQL = sql

	switch {
	case strings.Contains(sql, "FROM information_schema.tables"):
		return &mockRows{rows: m.tables, pos: -1}, nil

	case strings.Contains(sql, "FROM information_schema.columns"):
		return &mockRows{rows: m.columns, pos: -1}, nil

	case strings.Contains(sql, "PRIMARY KEY"):
		return &mockRows{rows: m.pk, pos: -1}, nil

	case strings.Contains(sql, "'UNIQUE'"):
		return &mockRows{rows: m.uq, pos: -1}, nil

	case strings.Contains(sql, "FOREIGN KEY"):
		return &mockRows{rows: m.fk, pos: -1}, nil

	default:
		return &mockRows{pos: -1}, nil
	}
}

type mockRow []any

type mockRows struct {
	rows   []mockRow
	pos    int
	closed bool
}

func (r *mockRows) Close()                         { r.closed = true }
func (r *mockRows) Err() error                     { return nil }
func (r *mockRows) Next() bool                     { r.pos++; return r.pos < len(r.rows) }
func (r *mockRows) Scan(dest ...any) error         {
	row := r.rows[r.pos]
	for i, val := range row {
		if i >= len(dest) {
			break
		}
		if val == nil {
			continue
		}
		rv := reflect.ValueOf(dest[i])
		if rv.Kind() != reflect.Pointer || rv.IsNil() {
			continue
		}
		target := reflect.Indirect(rv)
		sv := reflect.ValueOf(val)
		if sv.Type().AssignableTo(target.Type()) {
			target.Set(sv)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func makeSchemaMock() *mockQuerier {
	return &mockQuerier{
		tables: []mockRow{
			{"public", "users"},
			{"public", "orders"},
		},
		columns: []mockRow{
			{"id", "integer", "NO", "nextval('users_id_seq'::regclass)"},
			{"full_name", "character varying", "NO", nil},
			{"email", "character varying", "NO", nil},
			{"phone_number", "character varying", "YES", nil},
			{"birth_date", "date", "YES", nil},
		},
		pk: []mockRow{
			{"id"},
		},
		uq: []mockRow{
			{"email"},
		},
		fk: []mockRow{},
	}
}

// ordersMock provides FK data for the orders table.
func ordersMock() *mockQuerier {
	return &mockQuerier{
		tables: []mockRow{
			{"public", "orders"},
		},
		columns: []mockRow{
			{"id", "uuid", "NO", nil},
			{"user_id", "integer", "NO", nil},
			{"total_amount", "numeric", "NO", nil},
		},
		pk: []mockRow{
			{"id"},
		},
		uq: []mockRow{},
		fk: []mockRow{
			{"user_id", "public", "users", "id"},
		},
	}
}

// ---------------------------------------------------------------------------
// Tests: Introspect
// ---------------------------------------------------------------------------

func TestIntrospect_FullSchema(t *testing.T) {
	m := makeSchemaMock()
	s, err := Introspect(context.Background(), m)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	if len(s.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(s.Tables))
	}

	users, ok := s.Table("public.users")
	if !ok {
		t.Fatal("missing table public.users")
	}
	if users.Schema != "public" || users.Name != "users" {
		t.Errorf("table identity: %s.%s", users.Schema, users.Name)
	}
	if len(users.PrimaryKey) != 1 || users.PrimaryKey[0] != "id" {
		t.Errorf("PK = %v", users.PrimaryKey)
	}
	if len(users.Columns) != 5 {
		t.Errorf("expected 5 columns, got %d", len(users.Columns))
	}

	idCol, _ := users.Column("id")
	if idCol == nil || !idCol.IsPrimaryKey {
		t.Error("id should be primary key")
	}
	if idCol.DataType != "integer" {
		t.Errorf("id DataType = %q", idCol.DataType)
	}
	if idCol.IsNullable {
		t.Error("id should not be nullable")
	}

	fullName, _ := users.Column("full_name")
	if fullName == nil {
		t.Fatal("missing full_name column")
	}
	if fullName.DataType != "character varying" {
		t.Errorf("full_name DataType = %q", fullName.DataType)
	}

	email, _ := users.Column("email")
	if email == nil {
		t.Fatal("missing email column")
	}
	if !email.IsUnique {
		t.Error("email should be unique")
	}

	phone, _ := users.Column("phone_number")
	if phone == nil {
		t.Fatal("missing phone_number column")
	}
	if !phone.IsNullable {
		t.Error("phone_number should be nullable")
	}

	birth, _ := users.Column("birth_date")
	if birth == nil {
		t.Fatal("missing birth_date column")
	}
	if !birth.IsNullable {
		t.Error("birth_date should be nullable")
	}
	if birth.DefaultValue != "" {
		t.Errorf("birth_date default = %q, want empty", birth.DefaultValue)
	}
}

func TestIntrospect_ForeignKeys(t *testing.T) {
	m := ordersMock()
	s, err := Introspect(context.Background(), m)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	orders, ok := s.Table("public.orders")
	if !ok {
		t.Fatal("missing table public.orders")
	}

	userID, _ := orders.Column("user_id")
	if userID == nil {
		t.Fatal("missing user_id column")
	}
	if userID.ForeignKey == nil {
		t.Fatal("user_id should have a foreign key")
	}
	if userID.ForeignKey.RefTableName != "users" {
		t.Errorf("FK ref table = %q", userID.ForeignKey.RefTableName)
	}
	if userID.ForeignKey.RefColumnName != "id" {
		t.Errorf("FK ref column = %q", userID.ForeignKey.RefColumnName)
	}
	if userID.ForeignKey.RefQualifiedName() != "public.users" {
		t.Errorf("FK ref qualified = %q", userID.ForeignKey.RefQualifiedName())
	}
}

func TestIntrospect_NoTables(t *testing.T) {
	m := &mockQuerier{} // no tables
	s, err := Introspect(context.Background(), m)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}
	if len(s.Tables) != 0 {
		t.Errorf("expected 0 tables, got %d", len(s.Tables))
	}
}

func TestIntrospect_MultipleSchemas(t *testing.T) {
	m := &mockQuerier{
		tables: []mockRow{
			{"public", "users"},
			{"audit", "logs"},
		},
		columns: []mockRow{
			{"id", "integer", "NO", nil},
		},
	}
	s, err := Introspect(context.Background(), m)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	names := s.TableNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(names))
	}
	if names[0] != "audit.logs" {
		t.Errorf("TableNames()[0] = %q", names[0])
	}
	if names[1] != "public.users" {
		t.Errorf("TableNames()[1] = %q", names[1])
	}
}

func TestIntrospect_MissingColumn(t *testing.T) {
	m := &mockQuerier{
		tables: []mockRow{
			{"public", "empty_table"},
		},
	}
	s, err := Introspect(context.Background(), m)
	if err != nil {
		t.Fatalf("Introspect() error: %v", err)
	}

	tm, ok := s.Table("public.empty_table")
	if !ok {
		t.Fatal("missing table")
	}
	if len(tm.Columns) != 0 {
		t.Errorf("expected 0 columns, got %d", len(tm.Columns))
	}
}

func TestIntrospect_TableNotFound(t *testing.T) {
	s := NewSchema()
	_, ok := s.Table("nonexistent")
	if ok {
		t.Error("Table() should return false for missing table")
	}
}

// ---------------------------------------------------------------------------
// Tests: types
// ---------------------------------------------------------------------------

func TestSchema_TableNames_Empty(t *testing.T) {
	s := NewSchema()
	if len(s.TableNames()) != 0 {
		t.Error("empty schema should have no tables")
	}
}

func TestSchema_TableNames_Sorted(t *testing.T) {
	s := NewSchema()
	s.Tables["public.z"] = &TableMeta{Schema: "public", Name: "z"}
	s.Tables["public.a"] = &TableMeta{Schema: "public", Name: "a"}
	s.Tables["public.m"] = &TableMeta{Schema: "public", Name: "m"}

	names := s.TableNames()
	if len(names) != 3 || names[0] != "public.a" || names[1] != "public.m" || names[2] != "public.z" {
		t.Errorf("TableNames() = %v", names)
	}
}

func TestTableMeta_QualifiedName(t *testing.T) {
	tm := TableMeta{Schema: "public", Name: "users"}
	if tm.QualifiedName() != "public.users" {
		t.Errorf("QualifiedName = %q", tm.QualifiedName())
	}
}

func TestTableMeta_Column_NotFound(t *testing.T) {
	tm := TableMeta{Columns: map[string]*ColumnMeta{}}
	_, ok := tm.Column("missing")
	if ok {
		t.Error("Column() should return false for missing column")
	}
}

func TestColumnMeta_NullableByDefault(t *testing.T) {
	c := ColumnMeta{Name: "x", DataType: "text"}
	if c.IsPrimaryKey || c.IsUnique || c.IsNullable {
		t.Error("zero-value column should be NOT NULL")
	}
}

func TestForeignKeyMeta_RefQualifiedName(t *testing.T) {
	fk := ForeignKeyMeta{RefTableSchema: "public", RefTableName: "users", RefColumnName: "id"}
	if fk.RefQualifiedName() != "public.users" {
		t.Errorf("RefQualifiedName = %q", fk.RefQualifiedName())
	}
}
