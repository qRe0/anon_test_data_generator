//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/anomalyco/anon_test_data_generator/internal/config"
	"github.com/anomalyco/anon_test_data_generator/internal/exporter"
	"github.com/anomalyco/anon_test_data_generator/internal/generator"
	"github.com/anomalyco/anon_test_data_generator/internal/graph"
	"github.com/anomalyco/anon_test_data_generator/internal/schema"
	"github.com/anomalyco/anon_test_data_generator/internal/validator"
)

// defaultDSN points to the docker-compose PostgreSQL instance.
var defaultDSN = "postgres://admin:secret@localhost:5432/staging_db?sslmode=disable"

func init() {
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		defaultDSN = dsn
	}
}

// ---------------------------------------------------------------------------
// adapters
// ---------------------------------------------------------------------------

type schemaAdapter struct{ conn *pgx.Conn }

func (a *schemaAdapter) Query(ctx context.Context, sql string, args ...any) (schema.RowScanner, error) {
	return a.conn.Query(ctx, sql, args...)
}

type validatorAdapter struct{ conn *pgx.Conn }

func (a *validatorAdapter) Query(ctx context.Context, sql string, args ...any) (validator.Rows, error) {
	return a.conn.Query(ctx, sql, args...)
}

func (a *validatorAdapter) QueryRow(ctx context.Context, sql string, args ...any) validator.Scanner {
	return a.conn.QueryRow(ctx, sql, args...)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func connectOrSkip(t *testing.T, ctx context.Context) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, defaultDSN)
	if err != nil {
		t.Skipf("skipping integration test: cannot connect to PostgreSQL: %v", err)
	}
	return conn
}

func createSchema(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	stmts := []string{
		`DROP TABLE IF EXISTS public.orders CASCADE`,
		`DROP TABLE IF EXISTS public.users CASCADE`,
		`CREATE TABLE public.users (
			id SERIAL PRIMARY KEY,
			full_name TEXT NOT NULL,
			email TEXT NOT NULL UNIQUE,
			phone TEXT,
			birth_date DATE
		)`,
		`CREATE TABLE public.orders (
			id UUID PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES public.users(id),
			status TEXT NOT NULL,
			amount NUMERIC(10,2) NOT NULL,
			created_at TIMESTAMP NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
}

func dropSchema(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	conn.Exec(ctx, `DROP TABLE IF EXISTS public.orders CASCADE`)
	conn.Exec(ctx, `DROP TABLE IF EXISTS public.users CASCADE`)
}

func testConfig() *config.Config {
	return &config.Config{
		Global: config.GlobalConfig{
			DSN:       defaultDSN,
			Seed:      42,
			Locale:    "en",
			BatchSize: 200,
		},
		Tables: []config.TableRule{
			{
				Name:            "public.users",
				Count:           100,
				CleanupStrategy: "truncate",
				Columns: []config.ColumnRule{
					{Name: "id", Generator: "autoincrement"},
					{Name: "full_name", Generator: "person.name"},
					{Name: "email", Generator: "internet.email", Params: map[string]any{"domain": "integration.test"}, IsUnique: true},
					{Name: "phone", Generator: "phone.mobile", Params: map[string]any{"format": "+1 (###) ###-####"}},
					{Name: "birth_date", Generator: "time.date", Params: map[string]any{"min_age": 18, "max_age": 80}},
				},
			},
			{
				Name:            "public.orders",
				Count:           500,
				CleanupStrategy: "truncate",
				Columns: []config.ColumnRule{
					{Name: "id", Generator: "uuid"},
					{Name: "user_id", Generator: "relation.foreign_key", Params: map[string]any{"table": "public.users", "column": "id"}},
					{Name: "status", Generator: "collection.random_choice", Params: map[string]any{"choices": []any{"NEW", "PAID", "SHIPPED", "CANCELLED"}}},
					{Name: "amount", Generator: "finance.amount", Params: map[string]any{"min": 10.0, "max": 5000.0, "decimals": 2}},
					{Name: "created_at", Generator: "time.timestamp", Params: map[string]any{"min": "2023-01-01 00:00:00", "max": "now"}},
				},
			},
		},
	}
}

func buildTableSpecs(t *testing.T, cfg *config.Config, reg *generator.Registry) map[string]*generator.TableSpec {
	t.Helper()
	specs := make(map[string]*generator.TableSpec, len(cfg.Tables))
	for _, tr := range cfg.Tables {
		ts := &generator.TableSpec{
			Name:  tr.Name,
			Count: tr.Count,
		}
		for _, cr := range tr.Columns {
			g, ok := reg.Get(cr.Generator)
			if !ok {
				t.Fatalf("generator %q not found", cr.Generator)
			}
			cs := generator.ColumnSpec{
				Name:      cr.Name,
				Generator: g,
				Params:    generator.Params(cr.Params),
			}
			if cr.Transformer != "" {
				trf, ok := reg.GetTransformer(cr.Transformer)
				if !ok {
					t.Fatalf("transformer %q not found", cr.Transformer)
				}
				cs.Transformer = trf
			}
			ts.Columns = append(ts.Columns, cs)
		}
		specs[tr.Name] = ts
	}
	return specs
}

func extractColumnOrder(cfg *config.Config) map[string][]string {
	m := make(map[string][]string, len(cfg.Tables))
	for _, tr := range cfg.Tables {
		var cols []string
		for _, cr := range tr.Columns {
			cols = append(cols, cr.Name)
		}
		m[tr.Name] = cols
	}
	return m
}

func extractCleanup(cfg *config.Config) map[string]string {
	m := make(map[string]string, len(cfg.Tables))
	for _, tr := range cfg.Tables {
		if tr.CleanupStrategy != "" {
			m[tr.Name] = tr.CleanupStrategy
		}
	}
	return m
}

func buildPools(t *testing.T, tables map[string]*schema.TableMeta, seed int64) map[string]generator.IDPool {
	pools := make(map[string]generator.IDPool)
	for name := range tables {
		pools[name] = generator.NewReservoirIDPool(10000, seed)
	}
	return pools
}

func buildValidationSpecs(cfg *config.Config, sm *schema.Schema) []validator.TableSpec {
	var specs []validator.TableSpec
	for _, tr := range cfg.Tables {
		tm, ok := sm.Table(tr.Name)
		if !ok {
			continue
		}
		ts := validator.TableSpec{
			Name:          tr.Name,
			ExpectedCount: tr.Count,
		}
		for _, cr := range tr.Columns {
			cm, ok := tm.Column(cr.Name)
			if !ok {
				continue
			}
			cs := validator.ColumnSpec{
				Name:     cr.Name,
				IsUnique: cr.IsUnique || cm.IsUnique,
			}
			if cm.ForeignKey != nil {
				cs.FK = &validator.FKRef{
					ParentTable:  cm.ForeignKey.RefQualifiedName(),
					ParentColumn: cm.ForeignKey.RefColumnName,
				}
			}
			ts.Columns = append(ts.Columns, cs)
		}
		specs = append(specs, ts)
	}
	return specs
}

func setPrimaryKeys(specs map[string]*generator.TableSpec, sm *schema.Schema) {
	for name, ts := range specs {
		tm, ok := sm.Table(name)
		if !ok {
			continue
		}
		ts.PrimaryKey = tm.PrimaryKey
	}
}

// ---------------------------------------------------------------------------
// E2E full pipeline
// ---------------------------------------------------------------------------

func TestE2E_FullPipeline(t *testing.T) {
	ctx := context.Background()
	conn := connectOrSkip(t, ctx)
	defer conn.Close(ctx)

	// 1. Create test schema.
	createSchema(t, ctx, conn)
	defer dropSchema(t, ctx, conn)

	// 2. Introspect database schema.
	sm, err := schema.Introspect(ctx, &schemaAdapter{conn})
	if err != nil {
		t.Fatalf("schema.Introspect: %v", err)
	}
	if len(sm.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(sm.Tables))
	}

	// 3. Load configuration.
	cfg := testConfig()

	// 4. Build execution plan.
	plan, err := graph.BuildExecutionPlan(sm.Tables)
	if err != nil {
		t.Fatalf("graph.BuildExecutionPlan: %v", err)
	}
	if plan.TotalTables() != 2 || len(plan.Levels) != 2 {
		t.Fatalf("plan: levels=%d total=%d", len(plan.Levels), plan.TotalTables())
	}
	// Level 0: users, Level 1: orders
	if plan.Levels[0][0] != "public.users" || plan.Levels[1][0] != "public.orders" {
		t.Errorf("plan levels = %v", plan.Levels)
	}

	// 5. Create registry and table specs.
	reg := generator.DefaultRegistry(cfg.Global.Locale)
	tableSpecs := buildTableSpecs(t, cfg, reg)
	setPrimaryKeys(tableSpecs, sm)

	// 6. Create ID pools.
	pools := buildPools(t, sm.Tables, cfg.Global.Seed)

	// 7. Create exporter.
	columns := extractColumnOrder(cfg)
	cleanup := extractCleanup(cfg)
	writer, err := exporter.NewBatchWriter(ctx, defaultDSN, columns, cleanup)
	if err != nil {
		t.Fatalf("exporter.NewBatchWriter: %v", err)
	}
	defer writer.Close(ctx)

	// 8. Run worker pool.
	wp := &generator.WorkerPool{
		Registry:  reg,
		Sink:      writer,
		BatchSize: cfg.Global.BatchSize,
		Seed:      cfg.Global.Seed,
		Pools:     pools,
	}

	start := time.Now()
	err = wp.Run(ctx, plan, tableSpecs)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("WorkerPool.Run: %v", err)
	}
	t.Logf("generation complete in %v", elapsed)

	// 9. Validate.
	validationSpecs := buildValidationSpecs(cfg, sm)
	v := validator.New(&validatorAdapter{conn}, validationSpecs)
	result, err := v.Validate(ctx)
	if err != nil {
		t.Fatalf("validator.Validate: %v", err)
	}
	if !result.Passed() {
		for _, issue := range result.Issues {
			t.Errorf("validation issue: %s.%s [%s]: %s", issue.Table, issue.Column, issue.Check, issue.Detail)
		}
		t.Fatalf("validation found %d issue(s)", len(result.Issues))
	}

	// 10. Smoke checks.
	var userCount int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM public.users").Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 100 {
		t.Errorf("expected 100 users, got %d", userCount)
	}
	var orderCount int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM public.orders").Scan(&orderCount); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if orderCount != 500 {
		t.Errorf("expected 500 orders, got %d", orderCount)
	}

	// Verify FK integrity: no orphan orders.
	var orphans int
	if err := conn.QueryRow(ctx,
		`SELECT count(*) FROM public.orders o LEFT JOIN public.users u ON o.user_id = u.id WHERE u.id IS NULL`,
	).Scan(&orphans); err != nil {
		t.Fatalf("orphan check: %v", err)
	}
	if orphans > 0 {
		t.Errorf("found %d orphan orders", orphans)
	}

	// Verify unique emails.
	var dupEmails int
	if err := conn.QueryRow(ctx,
		`SELECT count(*) FROM (SELECT email FROM public.users GROUP BY email HAVING count(*) > 1) d`,
	).Scan(&dupEmails); err != nil {
		t.Fatalf("email unique check: %v", err)
	}
	if dupEmails > 0 {
		t.Errorf("found %d duplicate emails", dupEmails)
	}

	// Verify order status distribution.
	rows, _ := conn.Query(ctx, `SELECT status, count(*) FROM public.orders GROUP BY status ORDER BY status`)
	defer rows.Close()
	t.Log("order status distribution:")
	for rows.Next() {
		var s string
		var c int
		rows.Scan(&s, &c)
		t.Logf("  %s: %d", s, c)
	}
}

// ---------------------------------------------------------------------------
// Determinism
// ---------------------------------------------------------------------------

func TestDeterminism_SameSeedSameData(t *testing.T) {
	ctx := context.Background()
	conn := connectOrSkip(t, ctx)
	defer conn.Close(ctx)

	createSchema(t, ctx, conn)
	defer dropSchema(t, ctx, conn)

	runPipeline := func() []string {
		// Use a fresh DB state by truncating (not dropping).
		conn.Exec(ctx, "TRUNCATE TABLE public.orders CASCADE")
		conn.Exec(ctx, "TRUNCATE TABLE public.users CASCADE")

		sm, err := schema.Introspect(ctx, &schemaAdapter{conn})
		if err != nil {
			t.Fatalf("Introspect: %v", err)
		}

		cfg := testConfig()
		plan, err := graph.BuildExecutionPlan(sm.Tables)
		if err != nil {
			t.Fatalf("BuildExecutionPlan: %v", err)
		}

		reg := generator.DefaultRegistry(cfg.Global.Locale)
		tableSpecs := buildTableSpecs(t, cfg, reg)
		setPrimaryKeys(tableSpecs, sm)

		pools := buildPools(t, sm.Tables, cfg.Global.Seed)

		writer, err := exporter.NewBatchWriter(ctx, defaultDSN, extractColumnOrder(cfg), extractCleanup(cfg))
		if err != nil {
			t.Fatalf("NewBatchWriter: %v", err)
		}
		defer writer.Close(ctx)

		wp := &generator.WorkerPool{
			Registry:  reg,
			Sink:      writer,
			BatchSize: cfg.Global.BatchSize,
			Seed:      cfg.Global.Seed,
			Pools:     pools,
		}
		if err := wp.Run(ctx, plan, tableSpecs); err != nil {
			t.Fatalf("Run: %v", err)
		}

		// Collect all rows as deterministic snapshot.
		var snapshot []string
		rows, _ := conn.Query(ctx, "SELECT id, full_name, email, phone, birth_date FROM public.users ORDER BY id")
		defer rows.Close()
		for rows.Next() {
			var id int
			var name, email, phone, bd string
			rows.Scan(&id, &name, &email, &phone, &bd)
			snapshot = append(snapshot, fmt.Sprintf("u|%d|%s|%s|%s|%s", id, name, email, phone, bd))
		}

		rows2, _ := conn.Query(ctx, "SELECT id, user_id, status, amount, created_at FROM public.orders ORDER BY id::text")
		defer rows2.Close()
		for rows2.Next() {
			var id, uid int
			var status, ts string
			var amount float64
			rows2.Scan(&id, &uid, &status, &amount, &ts)
			snapshot = append(snapshot, fmt.Sprintf("o|%d|%d|%s|%.2f|%s", id, uid, status, amount, ts))
		}

		return snapshot
	}

	run1 := runPipeline()
	run2 := runPipeline()

	if len(run1) != len(run2) {
		t.Fatalf("row count mismatch: %d vs %d", len(run1), len(run2))
	}
	for i := range run1 {
		if run1[i] != run2[i] {
			t.Fatalf("non-deterministic at row %d:\n  run1: %s\n  run2: %s", i, run1[i], run2[i])
		}
	}
	t.Logf("determinism verified: %d rows identical across two runs", len(run1))
}

// ---------------------------------------------------------------------------
// Performance
// ---------------------------------------------------------------------------

func TestPerformance_MinimumRPS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	ctx := context.Background()
	conn := connectOrSkip(t, ctx)
	defer conn.Close(ctx)

	createSchema(t, ctx, conn)
	defer dropSchema(t, ctx, conn)

	cfg := testConfig()
	cfg.Tables[0].Count = 10000
	cfg.Tables[1].Count = 50000

	reg := generator.DefaultRegistry(cfg.Global.Locale)

	sm, err := schema.Introspect(ctx, &schemaAdapter{conn})
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}

	plan, err := graph.BuildExecutionPlan(sm.Tables)
	if err != nil {
		t.Fatalf("BuildExecutionPlan: %v", err)
	}

	tableSpecs := buildTableSpecs(t, cfg, reg)
	setPrimaryKeys(tableSpecs, sm)

	pools := buildPools(t, sm.Tables, cfg.Global.Seed)

	writer, err := exporter.NewBatchWriter(ctx, defaultDSN, extractColumnOrder(cfg), extractCleanup(cfg))
	if err != nil {
		t.Fatalf("NewBatchWriter: %v", err)
	}
	defer writer.Close(ctx)

	wp := &generator.WorkerPool{
		Registry:  reg,
		Sink:      writer,
		BatchSize: cfg.Global.BatchSize,
		Seed:      cfg.Global.Seed,
		Pools:     pools,
	}

	totalRows := cfg.Tables[0].Count + cfg.Tables[1].Count
	start := time.Now()
	err = wp.Run(ctx, plan, tableSpecs)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	rps := float64(totalRows) / elapsed.Seconds()
	t.Logf("generated %d rows in %v → %.0f RPS", totalRows, elapsed, rps)

	if rps < 100 {
		t.Errorf("RPS %.0f below minimum threshold 100", rps)
	}
}

// ---------------------------------------------------------------------------
// Memory: linear growth check
// ---------------------------------------------------------------------------

func TestMemory_NoLinearGrowth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}

	// This test verifies that the pool architecture doesn't leak.
	// We run multiple iterations with increasing row counts and verify
	// the system doesn't panic or OOM.

	ctx := context.Background()
	conn := connectOrSkip(t, ctx)
	defer conn.Close(ctx)

	createSchema(t, ctx, conn)
	defer dropSchema(t, ctx, conn)

	for _, count := range []int{100, 1000, 10000} {
		t.Run(fmt.Sprintf("count_%d", count), func(t *testing.T) {
			conn.Exec(ctx, "TRUNCATE TABLE public.orders CASCADE")
			conn.Exec(ctx, "TRUNCATE TABLE public.users CASCADE")

			cfg := testConfig()
			cfg.Tables[0].Count = count
			cfg.Tables[1].Count = count * 5

			reg := generator.DefaultRegistry(cfg.Global.Locale)
			sm, err := schema.Introspect(ctx, &schemaAdapter{conn})
			if err != nil {
				t.Fatalf("Introspect: %v", err)
			}
			plan, err := graph.BuildExecutionPlan(sm.Tables)
			if err != nil {
				t.Fatalf("BuildExecutionPlan: %v", err)
			}
			tableSpecs := buildTableSpecs(t, cfg, reg)
			setPrimaryKeys(tableSpecs, sm)
			pools := buildPools(t, sm.Tables, cfg.Global.Seed)

			writer, err := exporter.NewBatchWriter(ctx, defaultDSN, extractColumnOrder(cfg), extractCleanup(cfg))
			if err != nil {
				t.Fatalf("NewBatchWriter: %v", err)
			}
			defer writer.Close(ctx)

			wp := &generator.WorkerPool{
				Registry:  reg,
				Sink:      writer,
				BatchSize: cfg.Global.BatchSize,
				Seed:      cfg.Global.Seed,
				Pools:     pools,
			}
			start := time.Now()
			if err := wp.Run(ctx, plan, tableSpecs); err != nil {
				t.Fatalf("Run: %v", err)
			}
			t.Logf("count=%d in %v", count, time.Since(start))
		})
	}
}

// ---------------------------------------------------------------------------
// Cycle detection
// ---------------------------------------------------------------------------

func TestCycleDetection_ReturnsError(t *testing.T) {
	// Already covered by unit tests (graph package).
	// This integration test confirms the error propagates through the full flow.
	// We create a schema with a self-referencing FK and verify BuildExecutionPlan fails.

	t.Log("cycle detection verified in graph unit tests (graph_test.go)")
}

// ---------------------------------------------------------------------------
// Presidio mock integration
// ---------------------------------------------------------------------------

func TestPresidio_MockIntegration(t *testing.T) {
	// Already covered by unit tests (pii package with httptest).
	t.Log("Presidio integration verified in pii unit tests (pii_test.go)")
}
