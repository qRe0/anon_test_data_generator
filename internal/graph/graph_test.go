package graph

import (
	"strings"
	"testing"

	"github.com/anomalyco/anon_test_data_generator/internal/schema"
)

func makeTable(name string) *schema.TableMeta {
	parts := strings.SplitN(name, ".", 2)
	s := "public"
	n := name
	if len(parts) == 2 {
		s, n = parts[0], parts[1]
	}
	return &schema.TableMeta{
		Schema:  s,
		Name:    n,
		Columns: make(map[string]*schema.ColumnMeta),
	}
}

func addFK(parent *schema.TableMeta, colName string, refTable string) {
	parts := strings.SplitN(refTable, ".", 2)
	refSchema, refName := "public", refTable
	if len(parts) == 2 {
		refSchema, refName = parts[0], parts[1]
	}
	parent.Columns[colName] = &schema.ColumnMeta{
		Name: colName,
		ForeignKey: &schema.ForeignKeyMeta{
			RefTableSchema: refSchema,
			RefTableName:   refName,
			RefColumnName:  "id",
		},
	}
}

func buildSchema(tables ...*schema.TableMeta) map[string]*schema.TableMeta {
	m := make(map[string]*schema.TableMeta, len(tables))
	for _, tm := range tables {
		m[tm.QualifiedName()] = tm
	}
	return m
}

// ---------------------------------------------------------------------------
// Tests: BuildExecutionPlan
// ---------------------------------------------------------------------------

func TestBuildExecutionPlan_Empty(t *testing.T) {
	plan, err := BuildExecutionPlan(map[string]*schema.TableMeta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Levels) != 0 {
		t.Errorf("expected 0 levels, got %d", len(plan.Levels))
	}
	if plan.TotalTables() != 0 {
		t.Errorf("TotalTables = %d", plan.TotalTables())
	}
}

func TestBuildExecutionPlan_SingleTable(t *testing.T) {
	users := makeTable("public.users")
	plan, err := BuildExecutionPlan(buildSchema(users))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Levels) != 1 {
		t.Fatalf("expected 1 level, got %d", len(plan.Levels))
	}
	if len(plan.Levels[0]) != 1 || plan.Levels[0][0] != "public.users" {
		t.Errorf("Level[0] = %v", plan.Levels[0])
	}
}

func TestBuildExecutionPlan_IndependentTables(t *testing.T) {
	users := makeTable("public.users")
	products := makeTable("public.products")
	categories := makeTable("public.categories")

	plan, err := BuildExecutionPlan(buildSchema(users, products, categories))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Levels) != 1 {
		t.Fatalf("expected 1 level, got %d", len(plan.Levels))
	}
	if len(plan.Levels[0]) != 3 {
		t.Errorf("expected 3 tables in level 0, got %v", plan.Levels[0])
	}
	if plan.TotalTables() != 3 {
		t.Errorf("TotalTables = %d", plan.TotalTables())
	}
}

func TestBuildExecutionPlan_LinearChain(t *testing.T) {
	users := makeTable("public.users")
	orders := makeTable("public.orders")
	addFK(orders, "user_id", "public.users")
	items := makeTable("public.order_items")
	addFK(items, "order_id", "public.orders")

	plan, err := BuildExecutionPlan(buildSchema(users, orders, items))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Levels) != 3 {
		t.Fatalf("expected 3 levels, got %d", len(plan.Levels))
	}
	if plan.Levels[0][0] != "public.users" {
		t.Errorf("Level[0] = %v", plan.Levels[0])
	}
	if plan.Levels[1][0] != "public.orders" {
		t.Errorf("Level[1] = %v", plan.Levels[1])
	}
	if plan.Levels[2][0] != "public.order_items" {
		t.Errorf("Level[2] = %v", plan.Levels[2])
	}
}

func TestBuildExecutionPlan_Diamond(t *testing.T) {
	// users -> reviews    and    users -> orders
	// reviews -> notifications  AND  orders -> notifications
	users := makeTable("public.users")

	reviews := makeTable("public.reviews")
	addFK(reviews, "user_id", "public.users")

	orders := makeTable("public.orders")
	addFK(orders, "user_id", "public.users")

	notif := makeTable("public.notifications")
	addFK(notif, "review_id", "public.reviews")
	addFK(notif, "order_id", "public.orders")

	plan, err := BuildExecutionPlan(buildSchema(users, reviews, orders, notif))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Levels) != 3 {
		t.Fatalf("expected 3 levels, got %d", len(plan.Levels))
	}
	if len(plan.Levels[0]) != 1 || plan.Levels[0][0] != "public.users" {
		t.Errorf("Level[0] = %v", plan.Levels[0])
	}
	if len(plan.Levels[1]) != 2 {
		t.Errorf("Level[1] = %v", plan.Levels[1])
	}
	if len(plan.Levels[2]) != 1 || plan.Levels[2][0] != "public.notifications" {
		t.Errorf("Level[2] = %v", plan.Levels[2])
	}
}

func TestBuildExecutionPlan_FKToUnknownTable(t *testing.T) {
	users := makeTable("public.users")
	orders := makeTable("public.orders")
	addFK(orders, "user_id", "public.users")
	addFK(orders, "coupon_id", "public.coupons")

	plan, err := BuildExecutionPlan(buildSchema(users, orders))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Levels) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(plan.Levels))
	}
}

func TestBuildExecutionPlan_MultipleSchemas(t *testing.T) {
	users := makeTable("public.users")
	logs := makeTable("audit.logs")
	addFK(logs, "user_id", "public.users")

	plan, err := BuildExecutionPlan(buildSchema(users, logs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Levels) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(plan.Levels))
	}
	if plan.Levels[0][0] != "public.users" {
		t.Errorf("Level[0] = %v", plan.Levels[0])
	}
	if plan.Levels[1][0] != "audit.logs" {
		t.Errorf("Level[1] = %v", plan.Levels[1])
	}
}

// ---------------------------------------------------------------------------
// Cycle detection
// ---------------------------------------------------------------------------

func TestBuildExecutionPlan_DirectCycle(t *testing.T) {
	users := makeTable("public.users")
	addFK(users, "last_order_id", "public.orders")

	orders := makeTable("public.orders")
	addFK(orders, "user_id", "public.users")

	_, err := BuildExecutionPlan(buildSchema(users, orders))
	if err == nil {
		t.Fatal("expected cycle error")
	}
	ce, ok := err.(*CycleError)
	if !ok {
		t.Fatalf("expected CycleError, got %T: %v", err, err)
	}
	if len(ce.Remaining) != 2 {
		t.Errorf("expected 2 remaining nodes, got %v", ce.Remaining)
	}
}

func TestBuildExecutionPlan_SelfReference(t *testing.T) {
	cat := makeTable("public.categories")
	addFK(cat, "parent_id", "public.categories")

	_, err := BuildExecutionPlan(buildSchema(cat))
	if err == nil {
		t.Fatal("expected cycle error for self-reference")
	}
}

func TestBuildExecutionPlan_ThreeWayCycle(t *testing.T) {
	a := makeTable("a")
	addFK(a, "b_id", "b")
	b := makeTable("b")
	addFK(b, "c_id", "c")
	c := makeTable("c")
	addFK(c, "a_id", "a")

	_, err := BuildExecutionPlan(buildSchema(a, b, c))
	if err == nil {
		t.Fatal("expected cycle error")
	}
	ce, ok := err.(*CycleError)
	if !ok {
		t.Fatalf("expected CycleError, got %T", err)
	}
	if len(ce.Remaining) != 3 {
		t.Errorf("expected 3 remaining nodes, got %v", ce.Remaining)
	}
}

// ---------------------------------------------------------------------------
// ExecutionPlan methods
// ---------------------------------------------------------------------------

func TestExecutionPlan_Level(t *testing.T) {
	plan := &ExecutionPlan{
		Levels: [][]string{
			{"a", "b"},
			{"c"},
		},
	}
	if plan.Level(-1) != nil {
		t.Error("Level(-1) should be nil")
	}
	if plan.Level(2) != nil {
		t.Error("Level(2) should be nil")
	}
	if len(plan.Level(0)) != 2 {
		t.Errorf("Level(0) = %v", plan.Level(0))
	}
	if len(plan.Level(1)) != 1 {
		t.Errorf("Level(1) = %v", plan.Level(1))
	}
}

func TestExecutionPlan_TotalTables(t *testing.T) {
	plan := &ExecutionPlan{
		Order: []string{"a", "b", "c", "d"},
	}
	if plan.TotalTables() != 4 {
		t.Errorf("TotalTables = %d", plan.TotalTables())
	}
}

// ---------------------------------------------------------------------------
// CycleError
// ---------------------------------------------------------------------------

func TestCycleError_Formatting(t *testing.T) {
	err := &CycleError{Remaining: []string{"a", "b"}}
	msg := err.Error()
	if !strings.Contains(msg, "circular dependency") {
		t.Errorf("error = %q", msg)
	}
	if !strings.Contains(msg, "a") || !strings.Contains(msg, "b") {
		t.Errorf("error = %q", msg)
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestBuildExecutionPlan_TableWithNoColumns(t *testing.T) {
	emptyTable := makeTable("public.empty")
	plan, err := BuildExecutionPlan(buildSchema(emptyTable))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.TotalTables() != 1 {
		t.Errorf("TotalTables = %d", plan.TotalTables())
	}
}
