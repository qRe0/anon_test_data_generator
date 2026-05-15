package generator

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/anomalyco/anon_test_data_generator/internal/graph"
)

// BatchSink receives generated rows for writing to the target database.
type BatchSink interface {
	WriteBatch(ctx context.Context, table string, rows []Row) error
	Flush(ctx context.Context) error
}

// Row is a single generated database row (column name → value).
type Row map[string]any

// ColumnSpec describes how to generate a single column.
type ColumnSpec struct {
	Name        string
	Generator   ValueGenerator
	Transformer Transformer
	Params      Params
}

// TableSpec describes what to generate for a single table.
type TableSpec struct {
	Name       string
	Count      int
	Columns    []ColumnSpec
	PrimaryKey []string // column names that form the PK
}

// WorkerPool orchestrates concurrent generation of synthetic data.
type WorkerPool struct {
	Registry  *Registry
	Sink      BatchSink
	BatchSize int
	Seed      int64
	Pools     map[string]IDPool
}

// Run executes the full generation pipeline according to the plan.
func (wp *WorkerPool) Run(ctx context.Context, plan *graph.ExecutionPlan, tables map[string]*TableSpec) error {
	dataCh := make(chan batchMsg, wp.BatchSize*2+1)
	batcherErrCh := make(chan error, 1)

	go func() {
		batcherErrCh <- wp.runBatcher(ctx, dataCh)
	}()

	for levelIdx, level := range plan.Levels {
		wp.setupPoolProviders(level, tables)

		resultCh := make(chan producerResult, len(level))

		var wg sync.WaitGroup
		for i, tableName := range level {
			spec, ok := tables[tableName]
			if !ok {
				continue
			}
			seed := wp.Seed + int64(levelIdx)*10000 + int64(i)
			wg.Add(1)
			go func(tn string, s *TableSpec, sd int64) {
				defer wg.Done()
				wp.runProducer(ctx, tn, s, sd, dataCh, resultCh)
			}(tableName, spec, seed)
		}
		wg.Wait()
		close(resultCh)

		for res := range resultCh {
			if res.err != nil {
				return res.err
			}
			if pool, ok := wp.Pools[res.table]; ok && len(res.pks) > 0 {
				pool.Append(res.pks...)
			}
		}
	}

	close(dataCh)

	return <-batcherErrCh
}

// ---------------------------------------------------------------------------
// producer
// ---------------------------------------------------------------------------

type batchMsg struct {
	table string
	rows  []Row
}

type producerResult struct {
	table string
	pks   []any
	err   error
}

func (wp *WorkerPool) runProducer(ctx context.Context, tableName string, spec *TableSpec, seed int64, dataCh chan<- batchMsg, resultCh chan<- producerResult) {
	rnd := rand.New(rand.NewSource(seed))

	pkSet := map[string]bool{}
	for _, pk := range spec.PrimaryKey {
		pkSet[pk] = true
	}

	batch := make([]Row, 0, wp.BatchSize)
	var pks []any

	for i := 0; i < spec.Count; i++ {
		select {
		case <-ctx.Done():
			resultCh <- producerResult{table: tableName, pks: pks, err: ctx.Err()}
			return
		default:
		}

		row := Row{}
		for _, col := range spec.Columns {
			val := col.Generator.Generate(col.Params, rnd)
			if col.Transformer != nil {
				val = col.Transformer.Apply(val, col.Params)
			}
			row[col.Name] = val
			if pkSet[col.Name] {
				pks = append(pks, val)
			}
		}
		batch = append(batch, row)

		if len(batch) >= wp.BatchSize {
			select {
			case dataCh <- batchMsg{table: tableName, rows: batch}:
			case <-ctx.Done():
				resultCh <- producerResult{table: tableName, pks: pks, err: ctx.Err()}
				return
			}
			batch = make([]Row, 0, wp.BatchSize)
		}
	}

	if len(batch) > 0 {
		select {
		case dataCh <- batchMsg{table: tableName, rows: batch}:
		case <-ctx.Done():
			resultCh <- producerResult{table: tableName, pks: pks, err: ctx.Err()}
			return
		}
	}

	resultCh <- producerResult{table: tableName, pks: pks, err: nil}
}

// ---------------------------------------------------------------------------
// batcher
// ---------------------------------------------------------------------------

const flushInterval = 500 * time.Millisecond

func (wp *WorkerPool) runBatcher(ctx context.Context, dataCh <-chan batchMsg) error {
	var pending []Row
	var pendingTable string

	flushPending := func() error {
		if len(pending) == 0 {
			return nil
		}
		if err := wp.Sink.WriteBatch(ctx, pendingTable, pending); err != nil {
			return err
		}
		pending = nil
		return nil
	}

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-dataCh:
			if !ok {
				return flushPending()
			}
			// Write immediately if this batch is for a different table or would overflow.
			if pendingTable != "" && pendingTable != msg.table {
				if err := flushPending(); err != nil {
					return err
				}
			}
			pendingTable = msg.table
			pending = append(pending, msg.rows...)

			if len(pending) >= wp.BatchSize {
				if err := flushPending(); err != nil {
					return err
				}
			}

		case <-ticker.C:
			if err := flushPending(); err != nil {
				return err
			}
			if err := wp.Sink.Flush(ctx); err != nil {
				return err
			}

		case <-ctx.Done():
			return flushPending()
		}
	}
}

// ---------------------------------------------------------------------------
// pool provider wiring
// ---------------------------------------------------------------------------

func (wp *WorkerPool) setupPoolProviders(level []string, tables map[string]*TableSpec) {
	if wp.Pools == nil {
		return
	}
	for _, tableName := range level {
		spec, ok := tables[tableName]
		if !ok {
			continue
		}
		for ci := range spec.Columns {
			col := &spec.Columns[ci]
			if col.Params == nil {
				col.Params = Params{}
			}
			if _, hasProvider := col.Params["__pool_provider"]; hasProvider {
				continue
			}
			// Inject pool provider from the pool map.
			col.Params["__pool_provider"] = PoolProvider(func(refTable string) IDPool {
				pool := wp.Pools[refTable]
				return pool
			})
		}
	}
}
