// Package dynamodb is a DynamoDB-backed dbswitch.Store. Like the mongo
// package, DynamoDB is schemaless, so there is no Dialect here — Store is
// implemented directly against the AWS SDK.
//
// DynamoDB differs from Postgres/Mongo in ways that shape this backend:
//
//   - Every read/write of a single item needs its exact key. There is no
//     "update/delete everything matching this filter" primitive, so for
//     conditions beyond the primary key, Update/Delete Scan for matches
//     first, then issue one UpdateItem/DeleteItem per match.
//   - There is no secondary index by default, so a filter on a non-key
//     attribute (FindOne/Find/List/Count with a non-"id" where) requires a
//     full-table Scan with a server-side FilterExpression. Correct, but not
//     free at scale — see the README for details and the GSI escape hatch.
//   - Only a single-attribute partition key is supported (no composite
//     partition+sort keys yet), and it must be the column named "id".
//   - Uniqueness is enforced by DynamoDB only for the primary key (via a
//     conditional PutItem). Non-key Unique columns can't be enforced natively
//     without a transactional shadow-item pattern, so CreateTable rejects
//     them rather than silently ignoring the constraint.
package dynamodb

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/anukool23/dbswitch"
)

var _ dbswitch.Store = (*Store)(nil)

// partitionKey is the fixed attribute name dbswitch expects for a table's
// DynamoDB hash key. Unlike Postgres/Mongo, the key's attribute name is baked
// into every GetItem/UpdateItem/DeleteItem call rather than looked up per
// call, so — like Mongo's hardcoded "id" -> "_id" mapping — the convention
// here is fixed: every table's PrimaryKey column must be named "id".
const partitionKey = "id"

// Store is a DynamoDB-backed dbswitch.Store. Each generic "table" maps to a
// DynamoDB table of the same name.
type Store struct {
	client *dynamodb.Client
}

// Open builds a DynamoDB client from the default AWS config chain (env vars,
// shared config/credentials files, IAM role, etc.). Pass optFns to override
// client options — most commonly a custom endpoint for DynamoDB Local:
//
//	dynamodb.Open(ctx, func(o *dynamodb.Options) {
//		o.BaseEndpoint = aws.String("http://localhost:8000")
//	})
func Open(ctx context.Context, optFns ...func(*dynamodb.Options)) (*Store, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("dynamodb: load AWS config: %w", err)
	}
	return &Store{client: dynamodb.NewFromConfig(cfg, optFns...)}, nil
}

// Close is a no-op. The AWS SDK client has no persistent connection to
// release — it reuses a pooled HTTP client under the hood.
func (s *Store) Close() {}

// CreateTable creates the DynamoDB table if it doesn't already exist and
// waits for it to become ACTIVE. Only the PrimaryKey column feeds the
// table's key schema (every other column is a free-form item attribute, same
// as Mongo); billing is PAY_PER_REQUEST so there's no throughput to tune.
//
// The PrimaryKey column must be named "id" (see partitionKey) and its Type
// must be TypeUUID or TypeText (-> S) or TypeInt (-> N) — DynamoDB key
// attributes only support S, N, and B.
func (s *Store) CreateTable(ctx context.Context, t dbswitch.Table) error {
	pk, err := primaryKeyColumn(t)
	if err != nil {
		return err
	}

	keyType, err := keyAttributeType(pk.Type)
	if err != nil {
		return fmt.Errorf("dynamodb: primary key %q: %w", pk.Name, err)
	}

	for _, col := range t.Columns {
		if col.Unique && !col.PrimaryKey {
			return fmt.Errorf("dynamodb: unique constraints on non-primary-key columns are not supported (column %q of table %q)", col.Name, t.Name)
		}
	}

	_, err = s.client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   aws.String(t.Name),
		BillingMode: types.BillingModePayPerRequest,
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String(pk.Name), KeyType: types.KeyTypeHash},
		},
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String(pk.Name), AttributeType: keyType},
		},
	})
	if err != nil {
		var inUse *types.ResourceInUseException
		if errors.As(err, &inUse) {
			// Table already exists — mirrors Postgres's CREATE TABLE IF NOT
			// EXISTS and Mongo's implicit collection creation.
			return nil
		}
		return fmt.Errorf("dynamodb: create table %q: %w", t.Name, err)
	}

	waiter := dynamodb.NewTableExistsWaiter(s.client)
	if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(t.Name)}, 5*time.Minute); err != nil {
		return fmt.Errorf("dynamodb: wait for table %q active: %w", t.Name, err)
	}
	return nil
}

func primaryKeyColumn(t dbswitch.Table) (dbswitch.Column, error) {
	for _, col := range t.Columns {
		if !col.PrimaryKey {
			continue
		}
		if col.Name != partitionKey {
			return dbswitch.Column{}, fmt.Errorf("dynamodb: primary key column must be named %q, got %q", partitionKey, col.Name)
		}
		return col, nil
	}
	return dbswitch.Column{}, errors.New("dynamodb: table has no PrimaryKey column")
}

func keyAttributeType(t dbswitch.ColumnType) (types.ScalarAttributeType, error) {
	switch t {
	case dbswitch.TypeUUID, dbswitch.TypeText:
		return types.ScalarAttributeTypeS, nil
	case dbswitch.TypeInt:
		return types.ScalarAttributeTypeN, nil
	default:
		return "", fmt.Errorf("unsupported key type %v (DynamoDB keys must be S, N, or B — use TypeUUID, TypeText, or TypeInt)", t)
	}
}

// Insert stores one item with a conditional PutItem (attribute_not_exists),
// so writing an id that already exists fails with *dbswitch.DuplicateError —
// the same error type and errors.Is behavior as Postgres/Mongo — instead of
// silently overwriting the item, which is DynamoDB's default PutItem
// behavior.
func (s *Store) Insert(ctx context.Context, table string, data map[string]any) error {
	item, err := attributevalue.MarshalMap(data)
	if err != nil {
		return fmt.Errorf("dynamodb: marshal item: %w", err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                aws.String(table),
		Item:                     item,
		ConditionExpression:      aws.String("attribute_not_exists(#pk)"),
		ExpressionAttributeNames: map[string]string{"#pk": partitionKey},
	})
	if err != nil {
		var cond *types.ConditionalCheckFailedException
		if errors.As(err, &cond) {
			return &dbswitch.DuplicateError{Constraint: table + "." + partitionKey}
		}
		return fmt.Errorf("dynamodb: put item into %q: %w", table, err)
	}
	return nil
}

// FindOne returns the first matching item. If where is exactly {"id": v}, it
// uses a direct GetItem (cheap); otherwise it Scans with a FilterExpression,
// stopping at the first page that contains a match. No match -> ErrNotFound,
// same convention as Postgres/Mongo.
func (s *Store) FindOne(ctx context.Context, table string, where map[string]any) (map[string]any, error) {
	if id, ok := onlyIDCondition(where); ok {
		return s.getByID(ctx, table, id)
	}

	expr, names, values, err := buildFilterExpression(where)
	if err != nil {
		return nil, err
	}

	var startKey map[string]types.AttributeValue
	for {
		input := &dynamodb.ScanInput{TableName: aws.String(table), ExclusiveStartKey: startKey}
		if expr != "" {
			input.FilterExpression = aws.String(expr)
			input.ExpressionAttributeNames = names
			input.ExpressionAttributeValues = values
		}

		out, err := s.client.Scan(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("dynamodb: scan %q: %w", table, err)
		}
		if len(out.Items) > 0 {
			return unmarshalItem(out.Items[0])
		}
		if out.LastEvaluatedKey == nil {
			return nil, dbswitch.ErrNotFound
		}
		startKey = out.LastEvaluatedKey
	}
}

// Find returns every item matching where (empty where = every item in the
// table). No matches is an empty slice, not an error — same convention as
// Postgres/Mongo. See scanAll's doc comment for the full-table-Scan caveat.
func (s *Store) Find(ctx context.Context, table string, where map[string]any) ([]map[string]any, error) {
	items, err := s.scanAll(ctx, table, where)
	if err != nil {
		return nil, err
	}
	return unmarshalItems(items)
}

// List returns items matching opts: filtered, sorted, cursor-advanced, and
// limited — all emulated in memory after a full Scan, since a DynamoDB table
// without a sort key or GSI can't sort or seek server-side on an arbitrary
// attribute. Fine for small-to-medium tables; for large ones, add a GSI and
// query it directly instead of this generic path. See the README.
func (s *Store) List(ctx context.Context, table string, opts dbswitch.ListOptions) ([]map[string]any, error) {
	items, err := s.scanAll(ctx, table, opts.Filter)
	if err != nil {
		return nil, err
	}
	rows, err := unmarshalItems(items)
	if err != nil {
		return nil, err
	}

	if opts.SortBy != "" {
		sortRows(rows, opts.SortBy, opts.SortDir)
	}
	if opts.After != nil && opts.SortBy != "" {
		rows = filterAfter(rows, opts.SortBy, opts.SortDir, opts.After)
	}
	if opts.Offset > 0 {
		if opts.Offset >= len(rows) {
			return []map[string]any{}, nil
		}
		rows = rows[opts.Offset:]
	}
	if opts.Limit > 0 && opts.Limit < len(rows) {
		rows = rows[:opts.Limit]
	}
	return rows, nil
}

// Count returns how many items match filter, via a COUNT-only Scan (cheaper
// than fetching full items, but still a full-table walk when filter isn't
// empty — same caveat as Find/List).
func (s *Store) Count(ctx context.Context, table string, filter map[string]any) (int64, error) {
	expr, names, values, err := buildFilterExpression(filter)
	if err != nil {
		return 0, err
	}

	var total int64
	var startKey map[string]types.AttributeValue
	for {
		input := &dynamodb.ScanInput{
			TableName:         aws.String(table),
			Select:            types.SelectCount,
			ExclusiveStartKey: startKey,
		}
		if expr != "" {
			input.FilterExpression = aws.String(expr)
			input.ExpressionAttributeNames = names
			input.ExpressionAttributeValues = values
		}
		out, err := s.client.Scan(ctx, input)
		if err != nil {
			return 0, fmt.Errorf("dynamodb: count %q: %w", table, err)
		}
		total += int64(out.Count)
		if out.LastEvaluatedKey == nil {
			break
		}
		startKey = out.LastEvaluatedKey
	}
	return total, nil
}

// Update sets fields on every item matching where, returning how many
// changed. If where is exactly {"id": v} it updates that item directly
// (cheap, atomic). Otherwise it Scans for matching items first, then issues
// one UpdateItem per match — preserving the "update everything matching this
// filter" contract the other backends give you, at the cost of N+1 calls.
// Refuses an empty where, same guard as Postgres/Mongo.
func (s *Store) Update(ctx context.Context, table string, set, where map[string]any) (int64, error) {
	if len(set) == 0 {
		return 0, errors.New("dbswitch: update requires at least one column to set")
	}
	if len(where) == 0 {
		return 0, errors.New("dbswitch: update requires a WHERE condition (refusing to update all rows)")
	}

	updateExpr, names, values, err := buildUpdateExpression(set)
	if err != nil {
		return 0, err
	}
	names["#pk"] = partitionKey // shared placeholder for the existence check below

	if id, ok := onlyIDCondition(where); ok {
		keyAV, err := attributevalue.Marshal(id)
		if err != nil {
			return 0, fmt.Errorf("dynamodb: marshal id: %w", err)
		}
		updated, err := s.updateByKey(ctx, table, keyAV, updateExpr, names, values)
		if err != nil || !updated {
			return 0, err
		}
		return 1, nil
	}

	items, err := s.scanAll(ctx, table, where)
	if err != nil {
		return 0, err
	}

	var n int64
	for _, item := range items {
		keyAV, ok := item[partitionKey]
		if !ok {
			continue
		}
		updated, err := s.updateByKey(ctx, table, keyAV, updateExpr, names, values)
		if err != nil {
			return n, err
		}
		if updated {
			n++
		}
	}
	return n, nil
}

func (s *Store) updateByKey(ctx context.Context, table string, keyAV types.AttributeValue, updateExpr string, names map[string]string, values map[string]types.AttributeValue) (bool, error) {
	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(table),
		Key:                       map[string]types.AttributeValue{partitionKey: keyAV},
		UpdateExpression:          aws.String(updateExpr),
		ConditionExpression:       aws.String("attribute_exists(#pk)"),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: values,
	})
	if err != nil {
		var cond *types.ConditionalCheckFailedException
		if errors.As(err, &cond) {
			return false, nil // item didn't exist -> 0 rows affected, not an error
		}
		return false, fmt.Errorf("dynamodb: update item in %q: %w", table, err)
	}
	return true, nil
}

// Delete removes every item matching where, returning how many were
// deleted. Same direct-key fast path / Scan-then-loop fallback as Update,
// and the same empty-where guard as the other backends.
func (s *Store) Delete(ctx context.Context, table string, where map[string]any) (int64, error) {
	if len(where) == 0 {
		return 0, errors.New("dbswitch: delete requires a WHERE condition (refusing to delete all rows)")
	}

	if id, ok := onlyIDCondition(where); ok {
		keyAV, err := attributevalue.Marshal(id)
		if err != nil {
			return 0, fmt.Errorf("dynamodb: marshal id: %w", err)
		}
		deleted, err := s.deleteByKey(ctx, table, keyAV)
		if err != nil || !deleted {
			return 0, err
		}
		return 1, nil
	}

	items, err := s.scanAll(ctx, table, where)
	if err != nil {
		return 0, err
	}

	var n int64
	for _, item := range items {
		keyAV, ok := item[partitionKey]
		if !ok {
			continue
		}
		deleted, err := s.deleteByKey(ctx, table, keyAV)
		if err != nil {
			return n, err
		}
		if deleted {
			n++
		}
	}
	return n, nil
}

func (s *Store) deleteByKey(ctx context.Context, table string, keyAV types.AttributeValue) (bool, error) {
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName:                aws.String(table),
		Key:                      map[string]types.AttributeValue{partitionKey: keyAV},
		ConditionExpression:      aws.String("attribute_exists(#pk)"),
		ExpressionAttributeNames: map[string]string{"#pk": partitionKey},
	})
	if err != nil {
		var cond *types.ConditionalCheckFailedException
		if errors.As(err, &cond) {
			return false, nil
		}
		return false, fmt.Errorf("dynamodb: delete item from %q: %w", table, err)
	}
	return true, nil
}

// getByID fetches one item directly by its partition key.
func (s *Store) getByID(ctx context.Context, table string, id any) (map[string]any, error) {
	key, err := attributevalue.Marshal(id)
	if err != nil {
		return nil, fmt.Errorf("dynamodb: marshal id: %w", err)
	}
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(table),
		Key:       map[string]types.AttributeValue{partitionKey: key},
	})
	if err != nil {
		return nil, fmt.Errorf("dynamodb: get item from %q: %w", table, err)
	}
	if out.Item == nil {
		return nil, dbswitch.ErrNotFound
	}
	return unmarshalItem(out.Item)
}

// scanAll pages through a full table Scan, applying an optional equality
// filter, and collects every matching item. Used by Find, List, Count, and
// by Update/Delete when the condition isn't a direct primary-key lookup.
//
// This is DynamoDB's fundamental difference from Postgres/Mongo: without a
// secondary index, a filter on a non-key attribute has to walk the entire
// table. dbswitch's contract (arbitrary equality filters) still works — it
// just isn't free at scale. See the README.
func (s *Store) scanAll(ctx context.Context, table string, where map[string]any) ([]map[string]types.AttributeValue, error) {
	expr, names, values, err := buildFilterExpression(where)
	if err != nil {
		return nil, err
	}

	var items []map[string]types.AttributeValue
	var startKey map[string]types.AttributeValue
	for {
		input := &dynamodb.ScanInput{TableName: aws.String(table), ExclusiveStartKey: startKey}
		if expr != "" {
			input.FilterExpression = aws.String(expr)
			input.ExpressionAttributeNames = names
			input.ExpressionAttributeValues = values
		}

		out, err := s.client.Scan(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("dynamodb: scan %q: %w", table, err)
		}
		items = append(items, out.Items...)

		if out.LastEvaluatedKey == nil {
			break
		}
		startKey = out.LastEvaluatedKey
	}
	return items, nil
}

// onlyIDCondition reports whether where is exactly the single condition
// {"id": v} — the fast path that maps directly to a DynamoDB key, instead of
// requiring a Scan.
func onlyIDCondition(where map[string]any) (any, bool) {
	if len(where) != 1 {
		return nil, false
	}
	v, ok := where[partitionKey]
	return v, ok
}

// buildFilterExpression turns an equality-AND-ed condition map into a Dynamo
// FilterExpression, with placeholder names and values so reserved words and
// arbitrary value types are always safe. Keys are sorted first so the
// generated expression — and therefore behavior — is deterministic.
func buildFilterExpression(where map[string]any) (expr string, names map[string]string, values map[string]types.AttributeValue, err error) {
	if len(where) == 0 {
		return "", nil, nil, nil
	}

	keys := make([]string, 0, len(where))
	for k := range where {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	names = make(map[string]string, len(keys))
	values = make(map[string]types.AttributeValue, len(keys))
	parts := make([]string, 0, len(keys))
	for i, k := range keys {
		nameKey := fmt.Sprintf("#f%d", i)
		valueKey := fmt.Sprintf(":v%d", i)
		av, err := attributevalue.Marshal(where[k])
		if err != nil {
			return "", nil, nil, fmt.Errorf("dynamodb: marshal condition %q: %w", k, err)
		}
		names[nameKey] = k
		values[valueKey] = av
		parts = append(parts, fmt.Sprintf("%s = %s", nameKey, valueKey))
	}
	return strings.Join(parts, " AND "), names, values, nil
}

// buildUpdateExpression turns a SET-style column map into a Dynamo
// UpdateExpression ("SET #s0 = :s0, #s1 = :s1, ..."), with the same
// placeholder-everything approach as buildFilterExpression.
func buildUpdateExpression(set map[string]any) (string, map[string]string, map[string]types.AttributeValue, error) {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	names := make(map[string]string, len(keys))
	values := make(map[string]types.AttributeValue, len(keys))
	parts := make([]string, 0, len(keys))
	for i, k := range keys {
		nameKey := fmt.Sprintf("#s%d", i)
		valueKey := fmt.Sprintf(":s%d", i)
		av, err := attributevalue.Marshal(set[k])
		if err != nil {
			return "", nil, nil, fmt.Errorf("dynamodb: marshal set %q: %w", k, err)
		}
		names[nameKey] = k
		values[valueKey] = av
		parts = append(parts, fmt.Sprintf("%s = %s", nameKey, valueKey))
	}
	return "SET " + strings.Join(parts, ", "), names, values, nil
}

func unmarshalItem(item map[string]types.AttributeValue) (map[string]any, error) {
	var out map[string]any
	if err := attributevalue.UnmarshalMap(item, &out); err != nil {
		return nil, fmt.Errorf("dynamodb: unmarshal item: %w", err)
	}
	return out, nil
}

func unmarshalItems(items []map[string]types.AttributeValue) ([]map[string]any, error) {
	out := make([]map[string]any, len(items))
	for i, it := range items {
		m, err := unmarshalItem(it)
		if err != nil {
			return nil, err
		}
		out[i] = m
	}
	return out, nil
}

func sortRows(rows []map[string]any, field string, dir dbswitch.SortDirection) {
	sort.SliceStable(rows, func(i, j int) bool {
		c := compareValues(rows[i][field], rows[j][field])
		if dir == dbswitch.Descending {
			return c > 0
		}
		return c < 0
	})
}

func filterAfter(rows []map[string]any, field string, dir dbswitch.SortDirection, after any) []map[string]any {
	out := rows[:0:0] // fresh backing array
	for _, r := range rows {
		c := compareValues(r[field], after)
		if dir == dbswitch.Descending {
			if c < 0 {
				out = append(out, r)
			}
		} else if c > 0 {
			out = append(out, r)
		}
	}
	return out
}

// compareValues gives a best-effort ordering between two values coming back
// from DynamoDB's loosely-typed attributes (numbers decode to float64,
// strings to string). Numeric values compare numerically regardless of their
// exact Go type; everything else falls back to a string comparison of their
// fmt.Sprint representation, so List's SortBy never panics — it just may not
// order unlike-typed values meaningfully.
func compareValues(a, b any) int {
	if af, aok := toFloat64(a); aok {
		if bf, bok := toFloat64(b); bok {
			switch {
			case af < bf:
				return -1
			case af > bf:
				return 1
			default:
				return 0
			}
		}
	}
	return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}
