package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/anomalyco/anon_test_data_generator/internal/config"
	"github.com/anomalyco/anon_test_data_generator/internal/generator"
	"github.com/anomalyco/anon_test_data_generator/internal/graph"
	"github.com/anomalyco/anon_test_data_generator/internal/schema"
	"github.com/anomalyco/anon_test_data_generator/internal/validator"
)

func main() {
	configPath := flag.String("config", "configs/example.yaml", "path to YAML configuration file")
	flag.Parse()

	ctx := context.Background()

	// 1. Load and validate configuration.
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	dsn := cfg.Global.DSN
	if dsn == "" {
		dsn = os.Getenv("POSTGRES_DSN")
	}
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "dsn is required in config or POSTGRES_DSN env")
		os.Exit(1)
	}

	// 2. Connect to PostgreSQL.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to PostgreSQL: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = conn.Close(ctx) }()

	// 3. Validate configuration with generator registry.
	reg := generator.DefaultRegistry(cfg.Global.Locale)
	if errs := config.Validate(cfg, reg); len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "config validation failed:\n%s", config.ValidationErrorList(errs))
		os.Exit(1)
	}

	// 4. Introspect database schema.
	sm, err := schema.Introspect(ctx, &schemaAdapter{conn})
	if err != nil {
		fmt.Fprintf(os.Stderr, "schema introspection failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("discovered %d table(s)\n", len(sm.Tables))

	// 5. Build execution plan.
	plan, err := graph.BuildExecutionPlan(sm.Tables)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build execution plan: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("execution plan: %d level(s), %d table(s)\n", len(plan.Levels), plan.TotalTables())
	for i, level := range plan.Levels {
		fmt.Printf("  level %d: %v\n", i, level)
	}

	// 6. Build table specs from config.
	tableSpecs := buildTableSpecs(cfg, reg)
	setPrimaryKeys(tableSpecs, sm)

	// 7. Create ID pools for parent tables.
	pools := buildPools(sm.Tables, cfg.Global.Seed)

	// 8. Create exporter and run generation.
	cleanup := extractCleanup(cfg)
	if err := runCleanup(ctx, conn, cfg, cleanup); err != nil {
		fmt.Fprintf(os.Stderr, "cleanup failed: %v\n", err)
		os.Exit(1)
	}

	sink := &copySink{conn: conn, columns: extractColumnOrder(cfg)}

	wp := &generator.WorkerPool{
		Registry:  reg,
		Sink:      sink,
		BatchSize: cfg.Global.BatchSize,
		Seed:      cfg.Global.Seed,
		Pools:     pools,
	}

	total := totalRows(cfg)
	fmt.Printf("\ngenerating %d row(s) across %d table(s)...\n", total, len(cfg.Tables))
	start := time.Now()
	if err := wp.Run(ctx, plan, tableSpecs); err != nil {
		fmt.Fprintf(os.Stderr, "generation failed: %v\n", err)
		os.Exit(1)
	}
	elapsed := time.Since(start)
	fmt.Printf("done in %v\n", elapsed)

	// 9. Run post-generation validation.
	validationSpecs := buildValidationSpecs(cfg, sm)
	v := validator.New(&validatorAdapter{conn}, validationSpecs)
	result, err := v.Validate(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validation error: %v\n", err)
		os.Exit(1)
	}
	if !result.Passed() {
		fmt.Printf("\nvalidation found %d issue(s):\n", len(result.Issues))
		for _, issue := range result.Issues {
			fmt.Printf("  - %s.%s [%s]: %s\n", issue.Table, issue.Column, issue.Check, issue.Detail)
		}
		os.Exit(1)
	}

	rps := float64(total) / elapsed.Seconds()
	fmt.Printf("\nvalidation passed — %d row(s) at %.0f RPS\n", total, rps)
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

// copySink implements generator.BatchSink via pgx.CopyFrom.
type copySink struct {
	conn    *pgx.Conn
	columns map[string][]string
}

func (s *copySink) WriteBatch(ctx context.Context, table string, rows []generator.Row) error {
	cols := s.columns[table]
	if len(cols) == 0 {
		return fmt.Errorf("no column order for table %q", table)
	}
	source := newCopySource(rows, cols)
	parts := splitTable(table)
	_, err := s.conn.CopyFrom(ctx, pgx.Identifier{parts[0], parts[1]}, cols, source)
	if err != nil {
		return fmt.Errorf("COPY %s: %w", table, err)
	}
	return nil
}

func (s *copySink) Flush(ctx context.Context) error { return nil }

type copySourceStruct struct {
	rows []generator.Row
	cols []string
	pos  int
}

func newCopySource(rows []generator.Row, cols []string) *copySourceStruct {
	return &copySourceStruct{rows: rows, cols: cols, pos: -1}
}

func (s *copySourceStruct) Next() bool {
	s.pos++
	return s.pos < len(s.rows)
}

func (s *copySourceStruct) Values() ([]any, error) {
	row := s.rows[s.pos]
	vals := make([]any, len(s.cols))
	for i, col := range s.cols {
		vals[i] = row[col]
	}
	return vals, nil
}

func (s *copySourceStruct) Err() error { return nil }

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func splitTable(table string) []string {
	parts := splitParts(table)
	if len(parts) == 1 {
		return []string{parts[0]}
	}
	return parts
}

func splitParts(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

func buildTableSpecs(cfg *config.Config, reg *generator.Registry) map[string]*generator.TableSpec {
	specs := make(map[string]*generator.TableSpec, len(cfg.Tables))
	for _, tr := range cfg.Tables {
		ts := &generator.TableSpec{Name: tr.Name, Count: tr.Count}
		for _, cr := range tr.Columns {
			g, ok := reg.Get(cr.Generator)
			if !ok {
				panic(fmt.Sprintf("generator %q not found", cr.Generator))
			}
			cs := generator.ColumnSpec{
				Name:      cr.Name,
				Generator: g,
				Params:    generator.Params(cr.Params),
			}
			if cr.Transformer != "" {
				trf, ok := reg.GetTransformer(cr.Transformer)
				if !ok {
					panic(fmt.Sprintf("transformer %q not found", cr.Transformer))
				}
				cs.Transformer = trf
			}
			ts.Columns = append(ts.Columns, cs)
		}
		specs[tr.Name] = ts
	}
	return specs
}

func setPrimaryKeys(specs map[string]*generator.TableSpec, sm *schema.Schema) {
	for name, ts := range specs {
		if tm, ok := sm.Table(name); ok {
			ts.PrimaryKey = tm.PrimaryKey
		}
	}
}

func buildPools(tables map[string]*schema.TableMeta, seed int64) map[string]generator.IDPool {
	pools := make(map[string]generator.IDPool, len(tables))
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

func runCleanup(ctx context.Context, conn *pgx.Conn, cfg *config.Config, cleanup map[string]string) error {
	for _, tr := range cfg.Tables {
		switch cleanup[tr.Name] {
		case "truncate":
			if _, err := conn.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", tr.Name)); err != nil {
				return err
			}
		case "delete":
			if _, err := conn.Exec(ctx, fmt.Sprintf("DELETE FROM %s", tr.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func totalRows(cfg *config.Config) int {
	total := 0
	for _, tr := range cfg.Tables {
		total += tr.Count
	}
	return total
}
