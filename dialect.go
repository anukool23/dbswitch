package dbswitch

import "errors"

// Dialect is the per-database contract. Everything that differs between
// Postgres, MySQL, etc. is captured in these four primitives. The shared
// query builders (in query.go, next) construct all CRUD/DDL SQL using only
// these methods — so a new database is added by implementing this interface,
// never by rewriting how an INSERT or SELECT is built.
type Dialect interface {
	// Placeholder returns the bind-parameter marker for the n-th value
	// (1-based). Postgres: "$1", "$2"; MySQL would return "?" for every n.
	Placeholder(n int) string

	// QuoteIdentifier wraps a table or column name so reserved words and
	// mixed case are safe. Postgres uses double quotes; MySQL uses backticks.
	QuoteIdentifier(name string) string

	// ColumnTypeSQL renders an abstract ColumnType as this database's native
	// type. TypeUUID -> "UUID" on Postgres, "CHAR(36)" on MySQL.
	ColumnTypeSQL(t ColumnType) string

	// DefaultSQL renders an abstract DefaultValue as this database's default
	// expression. DefaultGenerateUUID -> "gen_random_uuid()" on Postgres.
	// Returns "" for DefaultNone (no DEFAULT clause).
	DefaultSQL(d DefaultValue) string
}

// Sentinel errors every Dialect's backend maps its native errors onto, so a
// caller can use errors.Is(err, ErrDuplicate) without knowing which database
// is underneath.
var (
	ErrNotFound  = errors.New("dbswitch: no rows found")
	ErrDuplicate = errors.New("dbswitch: unique constraint violation")
)