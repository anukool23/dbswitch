package dbswitch

import "context"

// TxOpType is the kind of operation inside a TransactWrite call.
type TxOpType string

const (
	// TxOpInsert creates a new row; fails with *DuplicateError if the id already exists.
	TxOpInsert TxOpType = "insert"
	// TxOpUpsert creates or fully replaces a row — never returns DuplicateError.
	TxOpUpsert TxOpType = "upsert"
	// TxOpUpdate applies a partial SET to a row; Where must contain "id".
	TxOpUpdate TxOpType = "update"
	// TxOpDelete removes a row; Where must contain "id".
	TxOpDelete TxOpType = "delete"
)

// TxOp is one write inside a TransactWrite call.
type TxOp struct {
	Type  TxOpType
	Table string
	Data  map[string]any // TxOpInsert, TxOpUpsert: full row (must include "id")
	Where map[string]any // TxOpUpdate, TxOpDelete: condition (must include "id")
	Set   map[string]any // TxOpUpdate: fields to set
}

type Store interface {
	CreateTable(ctx context.Context, t Table) error
	Insert(ctx context.Context, table string, data map[string]any) error
	// Upsert writes a row unconditionally: creates it if absent, fully replaces
	// it if present. Unlike Insert it never returns DuplicateError.
	Upsert(ctx context.Context, table string, data map[string]any) error
	FindOne(ctx context.Context, table string, where map[string]any) (map[string]any, error)
	Find(ctx context.Context, table string, where map[string]any) ([]map[string]any, error)
	List(ctx context.Context, table string, opts ListOptions) ([]map[string]any, error)
	Count(ctx context.Context, table string, filter map[string]any) (int64, error)
	Update(ctx context.Context, table string, set, where map[string]any) (int64, error)
	Delete(ctx context.Context, table string, where map[string]any) (int64, error)
	// TransactWrite executes multiple write operations atomically. All succeed
	// or all are rolled back. A duplicate-id Insert inside the transaction
	// returns *DuplicateError; any other failure returns ErrTransactionFailed.
	TransactWrite(ctx context.Context, ops []TxOp) error
	// DecrementField atomically decrements a numeric field by 1 on the row
	// matching where (must contain "id"). The decrement is a native atomic
	// operation on every backend — no read-before-write, no race condition.
	// Returns ErrNotFound if the row doesn't exist.
	DecrementField(ctx context.Context, table, field string, where map[string]any) error
	// IncrementField atomically increments a numeric field by 1 on the row
	// matching where (must contain "id"). The increment is a native atomic
	// operation on every backend — no read-before-write, no race condition.
	// Returns ErrNotFound if the row doesn't exist.
	IncrementField(ctx context.Context, table, field string, where map[string]any) error
	Close()
}