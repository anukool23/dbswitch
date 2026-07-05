package dbswitch

// ColumnType is a database-agnostic column type. It deliberately does NOT
// name a specific SQL type — each Dialect maps these to its own native type
// (e.g. TypeUUID becomes "UUID" on Postgres but "CHAR(36)" on MySQL).
type ColumnType int

const (
	TypeUUID ColumnType = iota
	TypeText
	TypeBool
	TypeInt
	TypeTimestamp
)

// DefaultValue expresses a column default without writing dialect SQL.
// The Dialect renders each of these to the right expression
// (e.g. DefaultGenerateUUID -> "gen_random_uuid()" on Postgres).
type DefaultValue int

const (
	DefaultNone DefaultValue = iota // no DEFAULT clause
	DefaultGenerateUUID
	DefaultCurrentTime
	DefaultFalse
	DefaultTrue
)

// Column describes one column of a table, independent of any database.
type Column struct {
	Name       string
	Type       ColumnType
	PrimaryKey bool
	Unique     bool
	NotNull    bool
	Default    DefaultValue
}

// Table describes a table's structure. You build this value in Go, and the
// configured Dialect turns it into a CREATE TABLE statement — so the
// consuming project never writes DDL SQL itself.
type Table struct {
	Name    string
	Columns []Column
}
