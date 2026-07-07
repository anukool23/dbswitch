# dbswitch

[![Go Reference](https://pkg.go.dev/badge/github.com/anukool23/dbswitch.svg)](https://pkg.go.dev/github.com/anukool23/dbswitch)
[![Docs](https://img.shields.io/badge/docs-forge.anukool.me-blue)](https://forge.anukool.me/dbswitch/)

A small, explicit, dependency-light data-access library for Go. You describe
tables and run CRUD in plain Go — `dbswitch` talks to the configured database
for you. No struct-tag reflection, no query DSL magic: for SQL backends the
generated SQL is transparent and always parameterized.

**v0.2.0 supports PostgreSQL and MongoDB.** Both back a common
[`dbswitch.Store`](#backends) interface, so the same CRUD code runs against
either. MySQL is planned (see [Roadmap](#roadmap)).

## Documentation

- Guides & versioned docs: <https://forge.anukool.me/dbswitch/>
- API reference: <https://pkg.go.dev/github.com/anukool23/dbswitch>

## Install

```bash
go get github.com/anukool23/dbswitch@latest
```

The core package (`dbswitch`) has no third-party dependencies. Each backend
lives in its own sub-package, so you only pull in a driver you actually use:

```bash
go get github.com/anukool23/dbswitch/postgres  # pulls in jackc/pgx
go get github.com/anukool23/dbswitch/mongo     # pulls in the official mongo driver
```

## Quick start

```go
ctx := context.Background()

// PostgreSQL. For MongoDB: mongo.Open(ctx, os.Getenv("MONGO_URI"), "myapp")
db, err := postgres.Open(ctx, os.Getenv("DATABASE_URL"))
if err != nil {
	log.Fatal(err)
}
defer db.Close()

// Describe a table in Go — no CREATE TABLE SQL.
users := dbswitch.Table{
	Name: "users",
	Columns: []dbswitch.Column{
		{Name: "id", Type: dbswitch.TypeUUID, PrimaryKey: true, Default: dbswitch.DefaultGenerateUUID},
		{Name: "email", Type: dbswitch.TypeText, NotNull: true, Unique: true},
		{Name: "created_at", Type: dbswitch.TypeTimestamp, NotNull: true, Default: dbswitch.DefaultCurrentTime},
	},
}
if err := db.CreateTable(ctx, users); err != nil {
	log.Fatal(err)
}

// Create.
err = db.Insert(ctx, "users", map[string]any{"email": "a@b.com"})

// Read one (returns dbswitch.ErrNotFound if nothing matches).
row, err := db.FindOne(ctx, "users", map[string]any{"email": "a@b.com"})

// Read many.
rows, err := db.Find(ctx, "users", nil) // nil where = all rows

// Update / Delete return rows-affected. Both refuse an empty condition.
n, err := db.Update(ctx, "users", map[string]any{"email": "c@d.com"}, map[string]any{"email": "a@b.com"})
n, err = db.Delete(ctx, "users", map[string]any{"email": "c@d.com"})
```

## Backends

Every backend implements one shared interface — `dbswitch.Store` — so the same
CRUD code runs against PostgreSQL or MongoDB:

```go
type Store interface {
	CreateTable(ctx context.Context, t Table) error
	Insert(ctx context.Context, table string, data map[string]any) error
	FindOne(ctx context.Context, table string, where map[string]any) (map[string]any, error)
	Find(ctx context.Context, table string, where map[string]any) ([]map[string]any, error)
	Update(ctx context.Context, table string, set, where map[string]any) (int64, error)
	Delete(ctx context.Context, table string, where map[string]any) (int64, error)
	Close()
}
```

Both `*postgres.DB` and `*mongo.Store` satisfy it (asserted at compile time), so
you can write backend-agnostic code:

```go
func seed(ctx context.Context, db dbswitch.Store) error {
	return db.Insert(ctx, "users", map[string]any{"email": "a@b.com"})
}
```

### PostgreSQL

```go
db, err := postgres.Open(ctx, os.Getenv("DATABASE_URL"))
```

`CreateTable` runs `CREATE TABLE IF NOT EXISTS`; conditions compile to
parameterized `WHERE col = $n AND …`; driver errors map to the shared error
values below (the constraint name comes from `pgconn.PgError`).

### MongoDB

Mongo needs the database name separately from the URI (a collection is reached
as `db(name).Collection(table)`):

```go
db, err := mongo.Open(ctx, os.Getenv("MONGO_URI"), "myapp")
```

Mongo is schemaless, so the abstractions map like this:

| dbswitch concept        | MongoDB                                                         |
|-------------------------|----------------------------------------------------------------|
| Table                   | Collection                                                     |
| `CreateTable`           | No DDL — creates a **unique index** per `Unique` column        |
| `PrimaryKey` column     | Mapped to Mongo's `_id` (already uniquely indexed)             |
| `"id"` on insert / read | Mapped to / from `_id` automatically                          |
| Duplicate key           | `*dbswitch.DuplicateError`; `Constraint` is the index name     |
| Not found               | `dbswitch.ErrNotFound`                                         |

Column **types** and **defaults** are SQL concepts — the Mongo backend ignores
them. Only `Unique` (→ unique index) and the primary key (→ `_id`) have an effect.

## Errors

All backends translate native driver errors into the same shared values, so
`errors.Is` checks work identically no matter the backend:

```go
// Not found
_, err := db.FindOne(ctx, "users", map[string]any{"email": "nope@x.com"})
if errors.Is(err, dbswitch.ErrNotFound) { /* ... */ }

// Unique-constraint violation, with the constraint/index name for domain mapping
err = db.Insert(ctx, "users", map[string]any{"email": "a@b.com"})
if errors.Is(err, dbswitch.ErrDuplicate) {
	var dup *dbswitch.DuplicateError
	if errors.As(err, &dup) {
		// dup.Constraint == "users_email_key" (Postgres) or "email_1" (Mongo)
	}
}
```

## Column types

Abstract types map to each SQL database's native type (ignored by Mongo):

| `dbswitch` type   | PostgreSQL     |
|-------------------|----------------|
| `TypeUUID`        | `UUID`         |
| `TypeText`        | `TEXT`         |
| `TypeBool`        | `BOOLEAN`      |
| `TypeInt`         | `INTEGER`      |
| `TypeTimestamp`   | `TIMESTAMPTZ`  |

Defaults: `DefaultGenerateUUID` → `gen_random_uuid()`,
`DefaultCurrentTime` → `now()`, `DefaultTrue`/`DefaultFalse`.

## Limitations (read these)

`dbswitch` is intentionally small. Know what it does *not* do:

- **Results are `map[string]any`.** Value types are whatever the driver
  returns — e.g. a Postgres `UUID` comes back as a `[16]byte`, a timestamp as
  `time.Time`. Convert at your boundary. There is no struct mapping.
- **Conditions are equality-only, ANDed.** `col = val AND …`. No `<`, `>`,
  `OR`, `IN`, `LIKE`, joins, ordering, or pagination yet.
- **`SELECT *` only** — you can't yet choose returned columns.
- **`CreateTable` is not migrations.** No schema versioning, alters, or
  indexes-beyond-column-constraints. Use a real migration tool for evolving
  schemas.
- **No transactions API** in v0.2.0.

For anything beyond this surface, use your driver directly — `dbswitch` is not
meant to hide SQL you actually need.

## Roadmap

Shipped in v0.2.0: the `Store` interface and the MongoDB backend. Still planned:

- MySQL backend (`dbswitch/mysql`)
- Richer conditions (operators, `OR`, `IN`)
- Transactions API

## Changelog

### v0.2.0

- **MongoDB backend** (`dbswitch/mongo`) built on the official Go driver.
- **`dbswitch.Store` interface** implemented by both backends — write CRUD once,
  run it on Postgres or Mongo.
- Unified errors (`ErrNotFound` / `ErrDuplicate`) across both backends.
- Runnable demos reorganized under `cmd/demo/postgres` and `cmd/demo/mongo`.

### v0.1.1

- Added `LICENSE`, package docs, and documentation links in the README. No API
  changes.

### v0.1.0

- Initial release: explicit schema definition, parameterized CRUD over
  `map[string]any`, shared error values, pure query builders, and the
  PostgreSQL backend.

## License

[MIT](LICENSE)
