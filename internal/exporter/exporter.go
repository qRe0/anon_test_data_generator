package exporter

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/anomalyco/anon_test_data_generator/internal/generator"
)

// BatchWriter writes generated rows to PostgreSQL using the COPY protocol.
// It implements generator.BatchSink.
type BatchWriter struct {
	conn    *pgx.Conn
	columns map[string][]string // table → ordered column names
	cleanup map[string]string   // table → cleanup strategy
	cleaned map[string]bool
	mu      sync.Mutex
}

// NewBatchWriter connects to PostgreSQL and returns a BatchWriter.
func NewBatchWriter(ctx context.Context, dsn string, columns map[string][]string, cleanup map[string]string) (*BatchWriter, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgresql: %w", err)
	}
	return &BatchWriter{
		conn:    conn,
		columns: columns,
		cleanup: cleanup,
		cleaned: make(map[string]bool),
	}, nil
}

// WriteBatch inserts a batch of rows into the specified table via COPY.
// On first write to a table, the cleanup strategy is executed.
func (w *BatchWriter) WriteBatch(ctx context.Context, table string, rows []generator.Row) error {
	if err := w.ensureCleanup(ctx, table); err != nil {
		return err
	}

	cols := w.columns[table]
	if len(cols) == 0 {
		return fmt.Errorf("no column order for table %q", table)
	}

	source := newCopySource(rows, cols)
	parts := strings.SplitN(table, ".", 2)
	ident := pgx.Identifier{parts[0], parts[1]}
	if len(parts) == 1 {
		ident = pgx.Identifier{parts[0]}
	}

	_, err := w.conn.CopyFrom(ctx, ident, cols, source)
	if err != nil {
		return fmt.Errorf("COPY %s: %w", table, err)
	}
	return nil
}

// Flush is a no-op since rows are written immediately in WriteBatch.
func (w *BatchWriter) Flush(_ context.Context) error {
	return nil
}

// Close closes the underlying database connection.
func (w *BatchWriter) Close(ctx context.Context) error {
	return w.conn.Close(ctx)
}

// ensureCleanup runs the cleanup strategy for the given table once.
func (w *BatchWriter) ensureCleanup(ctx context.Context, table string) error {
	w.mu.Lock()
	if w.cleaned[table] {
		w.mu.Unlock()
		return nil
	}
	w.cleaned[table] = true
	strategy := w.cleanup[table]
	w.mu.Unlock()

	switch strategy {
	case "truncate":
		return w.truncate(ctx, table)
	case "delete":
		return w.deleteAll(ctx, table)
	default:
		return nil
	}
}

func (w *BatchWriter) truncate(ctx context.Context, table string) error {
	_, err := w.conn.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
	if err != nil {
		return fmt.Errorf("TRUNCATE %s: %w", table, err)
	}
	return nil
}

func (w *BatchWriter) deleteAll(ctx context.Context, table string) error {
	_, err := w.conn.Exec(ctx, fmt.Sprintf("DELETE FROM %s", table))
	if err != nil {
		return fmt.Errorf("DELETE FROM %s: %w", table, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// CopyFromSource adapter
// ---------------------------------------------------------------------------

type copySource struct {
	rows    []generator.Row
	cols    []string
	pos     int
}

func newCopySource(rows []generator.Row, cols []string) *copySource {
	return &copySource{rows: rows, cols: cols, pos: -1}
}

func (s *copySource) Next() bool {
	s.pos++
	return s.pos < len(s.rows)
}

func (s *copySource) Values() ([]any, error) {
	row := s.rows[s.pos]
	vals := make([]any, len(s.cols))
	for i, col := range s.cols {
		vals[i] = row[col]
	}
	return vals, nil
}

func (s *copySource) Err() error {
	return nil
}
