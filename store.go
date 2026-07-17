package dbswitch

import "context"

type Store interface {
	CreateTable(ctx context.Context, t Table) error
	Insert(ctx context.Context, table string, data map[string]any) error
	FindOne(ctx context.Context, table string, where map[string]any) (map[string]any, error)
	Find(ctx context.Context, table string, where map[string]any) ([]map[string]any, error)
	List(ctx context.Context, table string, opts ListOptions) ([]map[string]any, error) // new
	Count(ctx context.Context, table string, filter map[string]any) (int64, error)
	Update(ctx context.Context, table string, set, where map[string]any) (int64, error)
	Delete(ctx context.Context, table string, where map[string]any) (int64, error)
	Close()
}