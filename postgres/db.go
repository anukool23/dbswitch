package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anukool23/dbswitch"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jackc/pgx/v5"
)

var _ dbswitch.Store = (*DB)(nil)

// DB is a Postgres-backed data store. It owns a connection pool and the
// Postgres dialect, and its methods run the generic query builders against
// the real database.
type DB struct {
	pool    *pgxpool.Pool
	dialect Dialect
}

// Open creates a pooled connection to Postgres and verifies it with a ping.
// The caller passes a context so it controls the connect timeout/cancellation.
func Open(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close() // don't leak the pool if the ping fails
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	return &DB{pool: pool, dialect: Dialect{}}, nil
}

// Close releases the connection pool. Call it when the app shuts down.
func (db *DB) Close() {
	db.pool.Close()
}

// CreateTable builds and executes a CREATE TABLE IF NOT EXISTS for the table.
func (db *DB) CreateTable(ctx context.Context, t dbswitch.Table) error {
	sql := dbswitch.BuildCreateTable(db.dialect, t)
	if _, err := db.pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("postgres: create table %q: %w", t.Name, err)
	}
	return nil
}

// Insert executes a single-row insert. On a unique-constraint violation it
// returns a *dbswitch.DuplicateError (which also satisfies
// errors.Is(err, dbswitch.ErrDuplicate)).
func (db *DB) Insert(ctx context.Context, table string, data map[string]any) error {
	sql, args := dbswitch.BuildInsert(db.dialect, table, data)
	if _, err := db.pool.Exec(ctx, sql, args...); err != nil {
		return mapError(err)
	}
	return nil
}

// mapError translates Postgres-native driver errors into dbswitch's generic
// errors. Anything it doesn't recognise is returned unchanged.
func mapError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return &dbswitch.DuplicateError{Constraint: pgErr.ConstraintName}
		}
	}
	return err
}

// FindOne returns the first row matching the conditions. If nothing matches,
// it returns dbswitch.ErrNotFound.
func (db *DB) FindOne(ctx context.Context, table string, where map[string]any) (map[string]any, error) {
	sql, args := dbswitch.BuildSelect(db.dialect, table, where)

	rows, err := db.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapError(err)
	}

	row, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, dbswitch.ErrNotFound
		}
		return nil, err
	}
	return row, nil
}

// Find returns all rows matching the conditions (an empty where means "all
// rows"). No matches is not an error — it returns an empty slice.
func (db *DB) Find(ctx context.Context, table string, where map[string]any) ([]map[string]any, error) {
	sql, args := dbswitch.BuildSelect(db.dialect, table, where)

	rows, err := db.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapError(err)
	}
	return pgx.CollectRows(rows, pgx.RowToMap)
}

// Update sets columns on rows matching where, and returns the number of rows
// changed. BuildUpdate refuses an empty where, so a missing condition is a
// returned error, not a full-table rewrite.
func (db *DB) Update(ctx context.Context, table string, set, where map[string]any) (int64, error) {
	sql, args, err := dbswitch.BuildUpdate(db.dialect, table, set, where)
	if err != nil {
		return 0, err
	}
	tag, err := db.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, mapError(err)
	}
	return tag.RowsAffected(), nil
}

// Delete removes rows matching where, and returns the number of rows deleted.
// BuildDelete refuses an empty where.
func (db *DB) Delete(ctx context.Context, table string, where map[string]any) (int64, error) {
	sql, args, err := dbswitch.BuildDelete(db.dialect, table, where)
	if err != nil {
		return 0, err
	}
	tag, err := db.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, mapError(err)
	}
	return tag.RowsAffected(), nil
}

// List returns rows matching opts (equality filters + optional cursor),
// ordered and limited. Empty result is an empty slice, not an error.
func (db *DB) List(ctx context.Context, table string, opts dbswitch.ListOptions) ([]map[string]any, error) {
	sql, args := dbswitch.BuildList(db.dialect, table, opts)

	rows, err := db.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapError(err)
	}
	return pgx.CollectRows(rows, pgx.RowToMap)
}
// Count returns how many rows match the filter (SELECT COUNT(*)).
func (db *DB) Count(ctx context.Context, table string, filter map[string]any) (int64, error) {
	sql, args := dbswitch.BuildCount(db.dialect, table, filter)

	var n int64
	if err := db.pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		return 0, mapError(err)
	}
	return n, nil
}
