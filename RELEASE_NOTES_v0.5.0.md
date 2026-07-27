# dbswitch v0.5.0

**dbswitch now supports DynamoDB**, alongside PostgreSQL and MongoDB — the same `dbswitch.Store` interface and CRUD code now run against all three.

## What's new

- **DynamoDB backend** (`dbswitch/dynamodb`), built on AWS SDK v2.
  - Primary-key operations (`{"id": v}`) go straight to `GetItem`/`PutItem`/`UpdateItem`/`DeleteItem`.
  - Any other equality filter falls back to a `Scan` + `FilterExpression`, so `Find`/`List`/`Count`/`Update`/`Delete` still work with arbitrary conditions — just not for free at scale.
  - `Update`/`Delete` on a non-key filter Scan for matches, then issue one call per match, preserving the same "affects everything matching this filter" contract as the Postgres/Mongo backends.
  - `List`'s `SortBy`/`After`/`Offset`/`Limit` are emulated in memory after the Scan (no sort key or GSI support yet).
  - `Insert` uses a conditional `PutItem` so a duplicate `"id"` returns `*dbswitch.DuplicateError`, same as Postgres/Mongo.
- Shared error values (`dbswitch.ErrNotFound`, `dbswitch.ErrDuplicate`) now behave identically across all three backends.
- New runnable demo: `cmd/demo/dynamodb`.

## Upgrade

```bash
go get github.com/anukool23/dbswitch@v0.5.0
go get github.com/anukool23/dbswitch/dynamodb   # pulls in aws-sdk-go-v2
```

No breaking changes to the `Store` interface or existing Postgres/Mongo backends.

## Known limitations

- The DynamoDB primary key must be a column named `"id"` — no composite (partition + sort key) tables yet.
- `Unique` is only enforceable on `"id"`; marking any other column `Unique` makes `CreateTable` return an error rather than silently not enforcing it.
- Filtering, sorting, updating, or deleting by a non-`"id"` field costs a full-table Scan. Fine for small/medium tables; for hot paths at scale, add a Global Secondary Index and query it directly with the AWS SDK.

## Full changelog

See the [README changelog](README.md#changelog) for the complete version history.
