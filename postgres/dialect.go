package postgres

import (
	"fmt"

	"github.com/anukool23/dbswitch"
)

// Dialect implements dbswitch.Dialect for PostgreSQL. It holds no state — it's
// just a bundle of Postgres-specific rendering rules — so it's an empty struct.
type Dialect struct{}

// Compile-time proof that Dialect satisfies the interface. If any method
// signature is wrong, the build fails *here*, with a clear message, instead of
// at some confusing call site later.
var _ dbswitch.Dialect = Dialect{}

func (Dialect) Placeholder(n int) string {
	return fmt.Sprintf("$%d", n)
}

func (Dialect) QuoteIdentifier(name string) string {
	return `"` + name + `"`
}

func (Dialect) ColumnTypeSQL(t dbswitch.ColumnType) string {
	switch t {
	case dbswitch.TypeUUID:
		return "UUID"
	case dbswitch.TypeText:
		return "TEXT"
	case dbswitch.TypeBool:
		return "BOOLEAN"
	case dbswitch.TypeInt:
		return "INTEGER"
	case dbswitch.TypeTimestamp:
		return "TIMESTAMPTZ"
	default:
		return "TEXT"
	}
}

func (Dialect) DefaultSQL(d dbswitch.DefaultValue) string {
	switch d {
	case dbswitch.DefaultGenerateUUID:
		return "gen_random_uuid()"
	case dbswitch.DefaultCurrentTime:
		return "now()"
	case dbswitch.DefaultFalse:
		return "FALSE"
	case dbswitch.DefaultTrue:
		return "TRUE"
	default: // DefaultNone
		return ""
	}
}