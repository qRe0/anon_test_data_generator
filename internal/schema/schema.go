package schema

import "sort"

// Schema holds the full metadata extracted from a database.
type Schema struct {
	Tables map[string]*TableMeta
}

// NewSchema returns an initialized Schema.
func NewSchema() *Schema {
	return &Schema{Tables: make(map[string]*TableMeta)}
}

// Table returns the metadata for a fully qualified table name (e.g. "public.users").
func (s *Schema) Table(name string) (*TableMeta, bool) {
	t, ok := s.Tables[name]
	return t, ok
}

// TableNames returns all fully qualified table names in sorted order.
func (s *Schema) TableNames() []string {
	names := make([]string, 0, len(s.Tables))
	for n := range s.Tables {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// TableMeta describes a single database table.
type TableMeta struct {
	Schema    string
	Name      string
	Columns   map[string]*ColumnMeta
	PrimaryKey []string
}

// QualifiedName returns "schema.name".
func (t *TableMeta) QualifiedName() string {
	return t.Schema + "." + t.Name
}

// Column returns the metadata for a column by name.
func (t *TableMeta) Column(name string) (*ColumnMeta, bool) {
	c, ok := t.Columns[name]
	return c, ok
}

// ColumnNames returns all column names in the order they appear in the map.
// Use ColumnOrder if insertion order matters.
func (t *TableMeta) ColumnNames() []string {
	names := make([]string, 0, len(t.Columns))
	for n := range t.Columns {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ColumnMeta describes a single database column.
type ColumnMeta struct {
	Name         string
	DataType     string
	IsNullable   bool
	IsPrimaryKey bool
	IsUnique     bool
	DefaultValue string
	ForeignKey   *ForeignKeyMeta
}

// ForeignKeyMeta describes a foreign key reference.
type ForeignKeyMeta struct {
	RefTableSchema string
	RefTableName   string
	RefColumnName  string
}

// RefQualifiedName returns the qualified name of the referenced table.
func (fk *ForeignKeyMeta) RefQualifiedName() string {
	return fk.RefTableSchema + "." + fk.RefTableName
}
