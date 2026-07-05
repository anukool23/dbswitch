# dbswitch

A small, explicit, dependency-light data-access library for Go. You describe
tables and run CRUD in plain Go — `dbswitch` builds the right SQL for the
configured database. No struct-tag reflection, no query DSL magic: the
generated SQL is transparent and always parameterized.

**v0.1.0 supports PostgreSQL.** MySQL and MongoDB backends are planned (see
[Roadmap](#roadmap)).

## Install

```bash
go get github.com/anukool23/dbswitch@latest
```

The core package (`dbswitch`) has no third-party dependencies. Each backend
lives in its own sub-package, so you only pull in a driver you actually use:

```bash
go get github.com/anukool23/dbswitch/postgres  # pulls in jackc/pgx
```

## Quick start

```go
ctx := context.Background()

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

## Errors

All backends translate native driver errors into shared values:

```go
// Not found
_, err := db.FindOne(ctx, "users", map[string]any{"email": "nope@x.com"})
if errors.Is(err, dbswitch.ErrNotFound) { /* ... */ }

// Unique-constraint violation, with the constraint name for domain mapping
err = db.Insert(ctx, "users", map[string]any{"email": "a@b.com"})
if errors.Is(err, dbswitch.ErrDuplicate) {
	var dup *dbswitch.DuplicateError
	if errors.As(err, &dup) {
		// dup.Constraint == "users_email_key" — map to your own meaning
	}
}
```

## Column types

Abstract types map to each database's native type:

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
- **Conditions are equality-only, ANDed.** `WHERE col = val AND …`. No `<`,
  `>`, `OR`, `IN`, `LIKE`, joins, ordering, or pagination yet.
- **`SELECT *` only** — you can't yet choose returned columns.
- **`CreateTable` is `CREATE TABLE IF NOT EXISTS`, not migrations.** No schema
  versioning, alters, or indexes-beyond-column-constraints. Use a real
  migration tool for evolving schemas.
- **No transactions API** in v0.1.0.

For anything beyond this surface, use your driver directly — `dbswitch` is not
meant to hide SQL you actually need.

## Roadmap

- MySQL backend (`dbswitch/mysql`)
- MongoDB backend (`dbswitch/mongo`)
- Richer conditions (operators, `OR`, `IN`)

## License

MIT