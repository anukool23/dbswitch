# Changelog

All notable changes to dbswitch are documented here.
Versions follow [Semantic Versioning](https://semver.org/).

---

## [v0.7.0] — 2026-07-31

### Added

- **`Store.IncrementField`** — atomically increments a numeric field by 1
  on the row identified by `where` (must contain `"id"`).
  Implemented natively on all three backends:
  - **DynamoDB** — `ADD #field :inc` via `UpdateItem`.
  - **PostgreSQL** — `UPDATE table SET field = field + 1 WHERE id = $1`.
  - **MongoDB** — `{ $inc: { field: 1 } }` via `UpdateOne`.
  - Returns `ErrNotFound` if no row/document matched.

- **`Store.DecrementField`** — atomically decrements a numeric field by 1
  on the row identified by `where` (must contain `"id"`).
  Implemented natively on all three backends — no read-before-write,
  safe under concurrent calls:
  - **DynamoDB** — `ADD #field :dec` via `UpdateItem` with a
    `attribute_exists` condition guard.
  - **PostgreSQL** — `UPDATE table SET field = field - 1 WHERE id = $1`
    single statement.
  - **MongoDB** — `{ $inc: { field: -1 } }` via `UpdateOne`.
  - Returns `ErrNotFound` if no row/document matched.

### Why

The previous pattern (read `remaining` → pass `remaining-1` to
`DecrementOTPRemaining`) was a read-modify-write race: two concurrent wrong
OTP attempts could both read the same value and both write the same
decremented result, losing one decrement. `DecrementField` fixes this at
the database level without any application-side locking.

---

## [v0.6.0]

- Initial public release with DynamoDB, PostgreSQL, and MongoDB backends.
- `Store` interface: `Insert`, `Upsert`, `FindOne`, `Find`, `List`,
  `Count`, `Update`, `Delete`, `TransactWrite`, `CreateTable`, `Close`.
- `TxOp` types: `TxOpInsert`, `TxOpUpsert`, `TxOpUpdate`, `TxOpDelete`.
- Shared error types: `DuplicateError`, `TransactionFailedError`, `ErrNotFound`.
