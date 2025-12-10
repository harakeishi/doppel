package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var ErrConflict = errors.New("duplicate trace and sequence")

// QueryRecord represents a recorded DB query.
type QueryRecord struct {
	TraceID    string
	Sequence   int
	SQL        string
	Bindings   *string
	Result     *string
	DurationMS int
}

// Store provides persistence for query recordings.
type Store struct {
	db *sql.DB
}

// NewStore applies schema migrations and returns a ready store.
func NewStore(db *sql.DB) (*Store, error) {
	if err := migrate(db); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS db_queries (
    trace_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    sql TEXT,
    bindings TEXT,
    result TEXT,
    duration_ms INTEGER,
    UNIQUE(trace_id, sequence)
);
`
	_, err := db.Exec(schema)
	return err
}

// InsertQuery stores a query recording.
func (s *Store) InsertQuery(ctx context.Context, record QueryRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO db_queries (trace_id, sequence, sql, bindings, result, duration_ms) VALUES (?, ?, ?, ?, ?, ?)`,
		record.TraceID, record.Sequence, record.SQL, record.Bindings, record.Result, record.DurationMS,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
			return ErrConflict
		}
		return err
	}
	return nil
}

// GetQuery retrieves a recording by trace ID and sequence.
func (s *Store) GetQuery(ctx context.Context, traceID string, sequence int) (*QueryRecord, error) {
	var (
		bindings sql.NullString
		result   sql.NullString
		rec      QueryRecord
	)

	row := s.db.QueryRowContext(ctx,
		`SELECT trace_id, sequence, sql, bindings, result, duration_ms FROM db_queries WHERE trace_id = ? AND sequence = ?`,
		traceID, sequence,
	)
	if err := row.Scan(&rec.TraceID, &rec.Sequence, &rec.SQL, &bindings, &result, &rec.DurationMS); err != nil {
		return nil, err
	}

	if bindings.Valid {
		rec.Bindings = &bindings.String
	}
	if result.Valid {
		rec.Result = &result.String
	}
	return &rec, nil
}
