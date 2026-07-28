# dbswitch v0.6.0

Two new operations added to `dbswitch.Store`, implemented across all three backends (PostgreSQL, MongoDB, DynamoDB).

## What's new

### `Upsert` — create or replace

```go
err := store.Upsert(ctx, "identity-otps", map[string]any{
    "id":      "user-123#RESET_PASSWORD",
    "otpHash": "...",
    "ttl":     time.Now().Add(10 * time.Minute).Unix(),
})
```

Creates the row if it doesn't exist; fully replaces it if it does. Never returns `DuplicateError`. Useful for OTP replacement, token refresh, and any "write the latest value" pattern.

| Backend    | Implementation                                          |
|------------|---------------------------------------------------------|
| PostgreSQL | `INSERT … ON CONFLICT ("id") DO UPDATE SET …`          |
| MongoDB    | `ReplaceOne` with `upsert: true`                        |
| DynamoDB   | Unconditional `PutItem` (no condition expression)       |

### `TransactWrite` — atomic multi-operation write

```go
err := store.TransactWrite(ctx, []dbswitch.TxOp{
    {Type: dbswitch.TxOpInsert, Table: "identity-email-locks", Data: map[string]any{"id": email, "userId": userId}},
    {Type: dbswitch.TxOpInsert, Table: "identity-profiles",    Data: profileData},
    {Type: dbswitch.TxOpInsert, Table: "identity-credentials", Data: credentialData},
})
```

All operations succeed or all are rolled back. Supports four op types:

| `TxOpType`    | Behaviour                                                    |
|---------------|--------------------------------------------------------------|
| `TxOpInsert`  | Create; rolls back (or fails) if the id already exists       |
| `TxOpUpsert`  | Create or replace; never causes rollback on its own          |
| `TxOpUpdate`  | Partial SET on matching row; `Where` must include `"id"`     |
| `TxOpDelete`  | Remove matching row; `Where` must include `"id"`             |

**Error behaviour:**
- A `TxOpInsert` on a duplicate id → `*dbswitch.DuplicateError` (same as a plain `Insert`, `errors.Is(err, ErrDuplicate)` works)
- Any other rollback reason → `*dbswitch.TransactionFailedError` (wraps the cause; `errors.Is(err, ErrTransactionFailed)` works, `errors.Unwrap` reaches the cause)

| Backend    | Implementation                                           |
|------------|----------------------------------------------------------|
| PostgreSQL | `BEGIN` / statement per op / `COMMIT`                    |
| MongoDB    | Multi-document transaction via `session.WithTransaction` — **requires a replica set**; standalone `mongod` does not support transactions |
| DynamoDB   | `TransactWriteItems` (up to 100 items, same-region)      |

## New error type

`TransactionFailedError` in `errors.go` — wraps the underlying cause and satisfies `errors.Is(err, ErrTransactionFailed)`. The sentinel `ErrTransactionFailed` is in `dialect.go` alongside `ErrNotFound` and `ErrDuplicate`.

## New SQL builder

`BuildUpsert(d Dialect, table string, data map[string]any) (string, []any)` in `query.go` — generates `INSERT … ON CONFLICT ("id") DO UPDATE SET …` for SQL backends. Non-SQL backends (Mongo, DynamoDB) implement `Upsert` natively and do not use this builder.

## Upgrade

```bash
go get github.com/anukool23/dbswitch@v0.6.0
go mod tidy
```

No breaking changes. The two new `Store` interface methods (`Upsert`, `TransactWrite`) are additive — existing code using the interface as a type in function parameters will need to add stubs only if you have a custom mock; the three built-in backends all satisfy the updated interface.
