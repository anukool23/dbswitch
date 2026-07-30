package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anukool23/dbswitch"
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

// Upsert executes INSERT … ON CONFLICT ("id") DO UPDATE SET …, creating the
// row if absent or fully replacing it if present. Never returns DuplicateError.
func (db *DB) Upsert(ctx context.Context, table string, data map[string]any) error {
	sql, args := dbswitch.BuildUpsert(db.dialect, table, data)
	if _, err := db.pool.Exec(ctx, sql, args...); err != nil {
		return mapError(err)
	}
	return nil
}

// TransactWrite executes multiple write operations inside a single Postgres
// transaction. All succeed or all roll back. A DuplicateError from any inner
// Insert is surfaced directly; any other failure is wrapped in
// *dbswitch.TransactionFailedError (errors.Is(err, ErrTransactionFailed) == true).
func (db *DB) TransactWrite(ctx context.Context, ops []dbswitch.TxOp) error {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck — no-op after a successful Commit

	for _, op := range ops {
		if err := execPostgresTxOp(ctx, tx, db.dialect, op); err != nil {
			return err // DuplicateError passes through unchanged
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return &dbswitch.TransactionFailedError{Cause: err}
	}
	return nil
}

func execPostgresTxOp(ctx context.Context, tx pgx.Tx, d Dialect, op dbswitch.TxOp) error {
	switch op.Type {
	case dbswitch.TxOpInsert:
		sql, args := dbswitch.BuildInsert(d, op.Table, op.Data)
		_, err := tx.Exec(ctx, sql, args...)
		return mapError(err)
	case dbswitch.TxOpUpsert:
		sql, args := dbswitch.BuildUpsert(d, op.Table, op.Data)
		_, err := tx.Exec(ctx, sql, args...)
		return mapError(err)
	case dbswitch.TxOpUpdate:
		sql, args, err := dbswitch.BuildUpdate(d, op.Table, op.Set, op.Where)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, sql, args...)
		return mapError(err)
	case dbswitch.TxOpDelete:
		sql, args, err := dbswitch.BuildDelete(d, op.Table, op.Where)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, sql, args...)
		return mapError(err)
	default:
		return fmt.Errorf("dbswitch: unknown TxOpType %q", op.Type)
	}
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
// DecrementField atomically decrements a numeric column by 1 using a single
// UPDATE statement — no read-before-write, safe under concurrent calls.
// where must contain "id". Returns ErrNotFound if no row matched.
func (db *DB) DecrementField(ctx context.Context, table, field string, where map[string]any) error {
	if len(where) == 0 {
		return errors.New("dbswitch: DecrementField requires a WHERE condition")
	}
	id, ok := where["id"]
	if !ok {
		return errors.New("dbswitch: DecrementField: where must contain \"id\"")
	}
	sql := fmt.Sprintf(`UPDATE %q SET %q = %q - 1 WHERE id = $1`, table, field, field)
	tag, err := db.pool.Exec(ctx, sql, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return dbswitch.ErrNotFound
	}
	return nil
}

// IncrementField atomically increments a numeric column by 1 using a single
// UPDATE statement — no read-before-write, safe under concurrent calls.
// where must contain "id". Returns ErrNotFound if no row matched.
func (db *DB) IncrementField(ctx context.Context, table, field string, where map[string]any) error {
	if len(where) == 0 {
		return errors.New("dbswitch: IncrementField requires a WHERE condition")
	}
	id, ok := where["id"]
	if !ok {
		return errors.New("dbswitch: IncrementField: where must contain \"id\"")
	}
	sql := fmt.Sprintf(`UPDATE %q SET %q = %q + 1 WHERE id = $1`, table, field, field)
	tag, err := db.pool.Exec(ctx, sql, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return dbswitch.ErrNotFound
	}
	return nil
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
