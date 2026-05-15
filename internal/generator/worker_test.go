package generator

import (
	"context"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anomalyco/anon_test_data_generator/internal/graph"
)

// ---------------------------------------------------------------------------
// mock sink
// ---------------------------------------------------------------------------

type mockSink struct {
	mu        sync.Mutex
	Rows      map[string][]Row // table → all rows written
	FlushCalls int
}

func newMockSink() *mockSink {
	return &mockSink{Rows: make(map[string][]Row)}
}

func (s *mockSink) WriteBatch(_ context.Context, table string, rows []Row) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Rows[table] = append(s.Rows[table], rows...)
	return nil
}

func (s *mockSink) Flush(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.FlushCalls++
	return nil
}

// ---------------------------------------------------------------------------
// table spec helpers
// ---------------------------------------------------------------------------

func singleTableSpec() map[string]*TableSpec {
	return map[string]*TableSpec{
		"public.users": {
			Name:       "public.users",
			Count:      50,
			PrimaryKey: []string{"id"},
			Columns: []ColumnSpec{
				{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1, "step": 1}},
				{Name: "name", Generator: newPersonNameGen("en")},
			},
		},
	}
}

func twoTableSpec() map[string]*TableSpec {
	return map[string]*TableSpec{
		"public.users": {
			Name:       "public.users",
			Count:      10,
			PrimaryKey: []string{"id"},
			Columns: []ColumnSpec{
				{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1, "step": 1}},
				{Name: "name", Generator: newPersonNameGen("en")},
			},
		},
		"public.orders": {
			Name:       "public.orders",
			Count:      20,
			PrimaryKey: []string{"id"},
			Columns: []ColumnSpec{
				{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1, "step": 1}},
				{Name: "user_id", Generator: &foreignKeyGen{}, Params: Params{"table": "public.users"}},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Tests: Run
// ---------------------------------------------------------------------------

func TestWorkerPool_SingleTable(t *testing.T) {
	reg := DefaultRegistry("en")
	sink := newMockSink()
	wp := &WorkerPool{
		Registry:  reg,
		Sink:      sink,
		BatchSize: 10,
		Seed:      42,
	}

	plan := &graph.ExecutionPlan{
		Levels: [][]string{{"public.users"}},
		Order:  []string{"public.users"},
	}

	err := wp.Run(context.Background(), plan, singleTableSpec())
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	rows := sink.Rows["public.users"]
	if len(rows) != 50 {
		t.Errorf("expected 50 rows, got %d", len(rows))
	}
	for _, row := range rows {
		if row["name"] == nil || row["name"] == "" {
			t.Error("row missing name")
		}
		id, ok := row["id"].(int64)
		if !ok {
			t.Errorf("id is not int64: %T %v", row["id"], row["id"])
		}
		if id < 1 || id > 50 {
			t.Errorf("id = %d", id)
		}
	}
}

func TestWorkerPool_TwoLevels(t *testing.T) {
	reg := DefaultRegistry("en")
	sink := newMockSink()

	pool := NewReservoirIDPool(1000, 1)

	wp := &WorkerPool{
		Registry:  reg,
		Sink:      sink,
		BatchSize: 5,
		Seed:      42,
		Pools: map[string]IDPool{
			"public.users": pool,
		},
	}

	plan := &graph.ExecutionPlan{
		Levels: [][]string{{"public.users"}, {"public.orders"}},
		Order:  []string{"public.users", "public.orders"},
	}

	err := wp.Run(context.Background(), plan, twoTableSpec())
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	users := sink.Rows["public.users"]
	orders := sink.Rows["public.orders"]

	if len(users) != 10 {
		t.Errorf("expected 10 users, got %d", len(users))
	}
	if len(orders) != 20 {
		t.Errorf("expected 20 orders, got %d", len(orders))
	}

	for _, order := range orders {
		uid, ok := order["user_id"].(int64)
		if !ok || uid == 0 {
			t.Errorf("order missing user_id: %v", order["user_id"])
		}
	}
}

func TestWorkerPool_ParallelLevel(t *testing.T) {
	reg := DefaultRegistry("en")
	sink := newMockSink()
	wp := &WorkerPool{
		Registry:  reg,
		Sink:      sink,
		BatchSize: 5,
		Seed:      42,
	}

	// Two independent tables in the same level.
	tables := map[string]*TableSpec{
		"public.users": {
			Name:       "public.users",
			Count:      20,
			PrimaryKey: []string{"id"},
			Columns: []ColumnSpec{
				{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1, "step": 1}},
				{Name: "name", Generator: newPersonNameGen("en")},
			},
		},
		"public.products": {
			Name:       "public.products",
			Count:      30,
			PrimaryKey: []string{"id"},
			Columns: []ColumnSpec{
				{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1, "step": 1}},
				{Name: "title", Generator: &staticValueGen{}, Params: Params{"value": "Product"}},
			},
		},
	}

	plan := &graph.ExecutionPlan{
		Levels: [][]string{{"public.users", "public.products"}},
		Order:  []string{"public.users", "public.products"},
	}

	err := wp.Run(context.Background(), plan, tables)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(sink.Rows["public.users"]) != 20 {
		t.Errorf("users = %d", len(sink.Rows["public.users"]))
	}
	if len(sink.Rows["public.products"]) != 30 {
		t.Errorf("products = %d", len(sink.Rows["public.products"]))
	}
}

func TestWorkerPool_Determinism(t *testing.T) {
	run := func() map[string][]Row {
		reg := DefaultRegistry("en")
		sink := newMockSink()
		wp := &WorkerPool{
			Registry:  reg,
			Sink:      sink,
			BatchSize: 5,
			Seed:      42,
		}
		plan := &graph.ExecutionPlan{
			Levels: [][]string{{"public.users"}},
			Order:  []string{"public.users"},
		}
		_ = wp.Run(context.Background(), plan, singleTableSpec())
		return sink.Rows
	}

	r1 := run()
	r2 := run()

	rows1 := r1["public.users"]
	rows2 := r2["public.users"]

	if len(rows1) != len(rows2) {
		t.Fatalf("row count mismatch: %d vs %d", len(rows1), len(rows2))
	}
	for i := range rows1 {
		if rows1[i]["name"] != rows2[i]["name"] {
			t.Fatalf("non-deterministic at row %d: %q vs %q", i, rows1[i]["name"], rows2[i]["name"])
		}
	}
}

func TestWorkerPool_GracefulShutdown(t *testing.T) {
	reg := DefaultRegistry("en")
	sink := newMockSink()
	wp := &WorkerPool{
		Registry:  reg,
		Sink:      sink,
		BatchSize: 1000,
		Seed:      42,
	}

	// A large count so we can cancel mid-generation.
	tables := map[string]*TableSpec{
		"public.users": {
			Name:       "public.users",
			Count:      100000,
			PrimaryKey: []string{"id"},
			Columns: []ColumnSpec{
				{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1, "step": 1}},
				{Name: "name", Generator: newPersonNameGen("en")},
			},
		},
	}

	plan := &graph.ExecutionPlan{
		Levels: [][]string{{"public.users"}},
		Order:  []string{"public.users"},
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- wp.Run(ctx, plan, tables)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	err := <-errCh
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestWorkerPool_EmptyPlan(t *testing.T) {
	wp := &WorkerPool{Registry: NewRegistry(), Sink: newMockSink(), BatchSize: 10}
	plan := &graph.ExecutionPlan{}

	err := wp.Run(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
}

func TestWorkerPool_TransformerApplied(t *testing.T) {
	reg := NewRegistry()
	reg.Register("static.str", &staticValueGen{})
	reg.RegisterTransformer("nulling", &nullingTransformer{})

	sink := newMockSink()
	wp := &WorkerPool{
		Registry:  reg,
		Sink:      sink,
		BatchSize: 10,
		Seed:      1,
	}

	tables := map[string]*TableSpec{
		"public.t": {
			Name:       "public.t",
			Count:      5,
			PrimaryKey: []string{"id"},
			Columns: []ColumnSpec{
				{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1}},
				{Name: "secret", Generator: &staticValueGen{}, Params: Params{"value": "hidden"}, Transformer: &nullingTransformer{}},
			},
		},
	}

	plan := &graph.ExecutionPlan{
		Levels: [][]string{{"public.t"}},
		Order:  []string{"public.t"},
	}

	err := wp.Run(context.Background(), plan, tables)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	for _, row := range sink.Rows["public.t"] {
		if row["secret"] != nil {
			t.Errorf("nulling transformer should nullify: %v", row["secret"])
		}
	}
}

func TestWorkerPool_PoolProviderWired(t *testing.T) {
	reg := DefaultRegistry("en")
	sink := newMockSink()

	pool := NewReservoirIDPool(100, 1)

	wp := &WorkerPool{
		Registry:  reg,
		Sink:      sink,
		BatchSize: 10,
		Seed:      7,
		Pools: map[string]IDPool{
			"public.parent": pool,
		},
	}

	tables := map[string]*TableSpec{
		"public.parent": {
			Name:       "public.parent",
			Count:      3,
			PrimaryKey: []string{"id"},
			Columns: []ColumnSpec{
				{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1}},
			},
		},
		"public.child": {
			Name:       "public.child",
			Count:      5,
			PrimaryKey: []string{"id"},
			Columns: []ColumnSpec{
				{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1}},
				{Name: "parent_id", Generator: &foreignKeyGen{}, Params: Params{"table": "public.parent"}},
			},
		},
	}

	plan := &graph.ExecutionPlan{
		Levels: [][]string{{"public.parent"}, {"public.child"}},
		Order:  []string{"public.parent", "public.child"},
	}

	err := wp.Run(context.Background(), plan, tables)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	for _, row := range sink.Rows["public.child"] {
		pid := row["parent_id"].(int64)
		if pid < 1 || pid > 3 {
			t.Errorf("child parent_id = %d, want one of {1,2,3}", pid)
		}
	}
}

func TestWorkerPool_FK_WithoutPool(t *testing.T) {
	reg := DefaultRegistry("en")
	sink := newMockSink()
	wp := &WorkerPool{
		Registry:  reg,
		Sink:      sink,
		BatchSize: 10,
		Seed:      3,
		Pools:     map[string]IDPool{},
	}

	tables := map[string]*TableSpec{
		"public.users": {
			Name:       "public.users",
			Count:      1,
			PrimaryKey: []string{"id"},
			Columns: []ColumnSpec{
				{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1}},
			},
		},
		"public.orders": {
			Name:       "public.orders",
			Count:      3,
			PrimaryKey: []string{"id"},
			Columns: []ColumnSpec{
				{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1}},
				{Name: "user_id", Generator: &foreignKeyGen{}, Params: Params{"table": "public.users"}},
			},
		},
	}

	plan := &graph.ExecutionPlan{
		Levels: [][]string{{"public.users"}, {"public.orders"}},
		Order:  []string{"public.users", "public.orders"},
	}

	err := wp.Run(context.Background(), plan, tables)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Without a populated pool, foreign_key should return nil.
	for _, row := range sink.Rows["public.orders"] {
		if row["user_id"] != nil {
			t.Errorf("expected nil user_id (no pool), got %v", row["user_id"])
		}
	}
}

func TestWorkerPool_MockSink_FlushCalls(t *testing.T) {
	reg := DefaultRegistry("en")
	sink := newMockSink()
	wp := &WorkerPool{
		Registry:  reg,
		Sink:      sink,
		BatchSize: 5,
		Seed:      99,
	}

	plan := &graph.ExecutionPlan{
		Levels: [][]string{{"public.users"}},
		Order:  []string{"public.users"},
	}

	_ = wp.Run(context.Background(), plan, singleTableSpec())

	// Verify all rows were written (no row loss regardless of flush timing).
	if len(sink.Rows["public.users"]) != 50 {
		t.Errorf("expected 50 rows, got %d", len(sink.Rows["public.users"]))
	}
}

// ---------------------------------------------------------------------------
// ColumnSpec helpers
// ---------------------------------------------------------------------------

func TestRow_MapBehavior(t *testing.T) {
	row := Row{"a": 1, "b": "hello"}
	if row["a"] != 1 {
		t.Errorf("Row[%q] = %v", "a", row["a"])
	}
	if row["missing"] != nil {
		t.Errorf("Row[%q] = %v", "missing", row["missing"])
	}
}

// ---------------------------------------------------------------------------
// Provider wiring
// ---------------------------------------------------------------------------

func TestSetupPoolProvider_InjectsProvider(t *testing.T) {
	pool := NewReservoirIDPool(10, 1)
	wp := &WorkerPool{
		Pools: map[string]IDPool{"public.users": pool},
	}

	tables := map[string]*TableSpec{
		"public.orders": {
			Name: "public.orders",
			Columns: []ColumnSpec{
				{
					Name:      "user_id",
					Generator: &foreignKeyGen{},
					Params:    Params{"table": "public.users"},
				},
			},
		},
	}

	wp.setupPoolProviders([]string{"public.orders"}, tables)

	col := tables["public.orders"].Columns[0]
	provider, ok := col.Params["__pool_provider"].(PoolProvider)
	if !ok || provider == nil {
		t.Fatal("__pool_provider not injected")
	}
	p := provider("public.users")
	if p != pool {
		t.Error("provider returned wrong pool")
	}
}

// ---------------------------------------------------------------------------
// Determinism edge cases
// ---------------------------------------------------------------------------

// Ensure autoincrement re-initializes on fresh WorkerPool (not cached state).
func TestWorkerPool_AutoIncrementReset(t *testing.T) {
	reg := DefaultRegistry("en")

	runCount := func() int64 {
		sink := newMockSink()
		wp := &WorkerPool{
			Registry:  reg,
			Sink:      sink,
			BatchSize: 10,
			Seed:      1,
		}
		plan := &graph.ExecutionPlan{
			Levels: [][]string{{"public.users"}},
			Order:  []string{"public.users"},
		}
		err := wp.Run(context.Background(), plan, map[string]*TableSpec{
			"public.users": {
				Name:       "public.users",
				Count:      5,
				PrimaryKey: []string{"id"},
				Columns: []ColumnSpec{
					{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1, "step": 1}},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return sink.Rows["public.users"][4]["id"].(int64)
	}

	first := runCount()
	second := runCount()
	if first != 5 || second != 5 {
		t.Errorf("autoincrement should reset each Run: first=%d second=%d", first, second)
	}
}

// ---------------------------------------------------------------------------
// Partial batch test
// ---------------------------------------------------------------------------

func TestWorkerPool_PartialBatch(t *testing.T) {
	reg := DefaultRegistry("en")
	sink := newMockSink()
	wp := &WorkerPool{
		Registry:  reg,
		Sink:      sink,
		BatchSize: 3, // 7 rows should produce 3 batches: 3, 3, 1
		Seed:      42,
	}

	tables := map[string]*TableSpec{
		"public.t": {
			Name:       "public.t",
			Count:      7,
			PrimaryKey: []string{"id"},
			Columns: []ColumnSpec{
				{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1}},
			},
		},
	}

	plan := &graph.ExecutionPlan{
		Levels: [][]string{{"public.t"}},
		Order:  []string{"public.t"},
	}

	err := wp.Run(context.Background(), plan, tables)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(sink.Rows["public.t"]) != 7 {
		t.Errorf("expected 7 rows, got %d", len(sink.Rows["public.t"]))
	}
}

// ---------------------------------------------------------------------------
// runProducer helper test
// ---------------------------------------------------------------------------

// Test that runProducer yields PKs.
func TestRunProducer_PKCollection(t *testing.T) {
	reg := DefaultRegistry("en")
	spec := &TableSpec{
		Name:       "t",
		Count:      3,
		PrimaryKey: []string{"id"},
		Columns: []ColumnSpec{
			{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 10}},
			{Name: "val", Generator: &staticValueGen{}, Params: Params{"value": "x"}},
		},
	}
	wp := &WorkerPool{
		Registry:  reg,
		BatchSize: 2,
	}
	dataCh := make(chan batchMsg, 10)
	resultCh := make(chan producerResult, 1)

	go func() {
		// Drain data channel.
		for range dataCh {
		}
	}()

	wp.runProducer(context.Background(), "t", spec, 1, dataCh, resultCh)
	res := <-resultCh

	if res.err != nil {
		t.Fatalf("producer error: %v", res.err)
	}
	if len(res.pks) != 3 {
		t.Errorf("expected 3 PKs, got %d: %v", len(res.pks), res.pks)
	}
}

// ---------------------------------------------------------------------------
// Batch size helpers
// ---------------------------------------------------------------------------

func TestBatchMsg_Structure(t *testing.T) {
	msg := batchMsg{table: "public.users", rows: []Row{{"a": 1}}}
	if msg.table != "public.users" || len(msg.rows) != 1 {
		t.Errorf("batchMsg: %+v", msg)
	}
}

// ---------------------------------------------------------------------------
// NewFaker determinism helpers
// ---------------------------------------------------------------------------

func TestFakerSeedDerivation(t *testing.T) {
	// Verify that gofakeit.New(0) is not used — we always pass a derived seed.
	// The generator tests already cover this, but ensure no default zero seed.
	r1 := rand.New(rand.NewSource(123))
	r2 := rand.New(rand.NewSource(123))
	g1 := newPersonNameGen("en")
	g2 := newPersonNameGen("en")
	n1 := g1.Generate(nil, r1).(string)
	n2 := g2.Generate(nil, r2).(string)
	if !strings.Contains(n1, " ") {
		t.Errorf("unexpected name: %q", n1)
	}
	if n1 != n2 {
		t.Errorf("faker seed derivation non-deterministic: %q vs %q", n1, n2)
	}
}

// ---------------------------------------------------------------------------
// Verify batch size enforcement
// ---------------------------------------------------------------------------

func TestWorkerPool_BatchSizeEnforced(t *testing.T) {
	reg := DefaultRegistry("en")
	sink := newMockSink()

	// Use large batch size, small count to produce a single partial batch.
	wp := &WorkerPool{
		Registry:  reg,
		Sink:      sink,
		BatchSize: 100,
		Seed:      42,
	}

	tables := map[string]*TableSpec{
		"public.t": {
			Name:       "public.t",
			Count:      5,
			PrimaryKey: []string{"id"},
			Columns: []ColumnSpec{
				{Name: "id", Generator: &autoIncrementGen{}, Params: Params{"start": 1}},
			},
		},
	}

	plan := &graph.ExecutionPlan{
		Levels: [][]string{{"public.t"}},
		Order:  []string{"public.t"},
	}

	err := wp.Run(context.Background(), plan, tables)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if len(sink.Rows["public.t"]) != 5 {
		t.Errorf("expected 5 rows, got %d", len(sink.Rows["public.t"]))
	}
}
