package mongo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/anukool23/dbswitch"
)

var _ dbswitch.Store = (*Store)(nil)

// Store is a MongoDB-backed dbswitch.Store. It holds the client and a handle
// to one database; each generic "table" maps to a collection in that database.
type Store struct {
	client *mongo.Client
	db     *mongo.Database
}

// Open connects to MongoDB and verifies the connection with a ping. Unlike
// postgres.Open (which takes just a DSN), Mongo needs the database name
// separately, since a collection is reached as db(name).Collection(table).
func Open(ctx context.Context, uri, dbName string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo: connect: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx) // don't leak the client if the ping fails
		return nil, fmt.Errorf("mongo: ping: %w", err)
	}

	return &Store{client: client, db: client.Database(dbName)}, nil
}

// Close disconnects the client. Disconnect returns an error, but the Store
// interface's Close() is fire-and-forget, so we discard it (best effort).
func (s *Store) Close() {
	_ = s.client.Disconnect(context.Background())
}

// CreateTable has no DDL equivalent in Mongo (collections are schemaless and
// created implicitly on first write). What it CAN do is enforce the Table's
// uniqueness intent: for every Unique column it creates a unique index. The
// primary-key column maps to Mongo's _id, which is already uniquely indexed.
func (s *Store) CreateTable(ctx context.Context, t dbswitch.Table) error {
	coll := s.db.Collection(t.Name)

	for _, col := range t.Columns {
		// The primary key becomes _id (unique by default) — no index to create.
		if col.PrimaryKey {
			continue
		}
		if !col.Unique {
			continue
		}

		_, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys:    bson.D{{Key: col.Name, Value: 1}},
			Options: options.Index().SetUnique(true),
		})
		if err != nil {
			return fmt.Errorf("mongo: create unique index on %q.%q: %w", t.Name, col.Name, err)
		}
	}
	return nil
}

// Insert stores one document. The generic "id" field is mapped to Mongo's _id
// so the caller's id becomes the primary key. A duplicate unique-index
// violation is translated to *dbswitch.DuplicateError — the same error type
// Postgres returns — so callers' errors.Is(err, dbswitch.ErrDuplicate) works
// identically no matter the backend.
func (s *Store) Insert(ctx context.Context, table string, data map[string]any) error {
	_, err := s.db.Collection(table).InsertOne(ctx, toMongoDoc(data))
	if err != nil {
		return mapMongoError(err)
	}
	return nil
}

// toMongoDoc copies the data map, renaming "id" -> "_id" so the caller's id is
// stored as Mongo's primary key (completing the mapping CreateTable set up).
func toMongoDoc(data map[string]any) map[string]any {
	doc := make(map[string]any, len(data))
	for k, v := range data {
		if k == "id" {
			doc["_id"] = v
			continue
		}
		doc[k] = v
	}
	return doc
}

// mapMongoError translates Mongo driver errors into dbswitch's generic errors.
func mapMongoError(err error) error {
	if mongo.IsDuplicateKeyError(err) {
		return &dbswitch.DuplicateError{Constraint: duplicateConstraint(err)}
	}
	return err
}

// duplicateConstraint pulls the violated index name (e.g. "email_1") out of a
// Mongo duplicate-key message. Mongo, like MySQL, has no clean constraint-name
// field the way Postgres's pgconn.PgError.ConstraintName does — so we parse it.
func duplicateConstraint(err error) string {
	msg := err.Error()
	const marker = "index: "
	i := strings.Index(msg, marker)
	if i == -1 {
		return msg
	}
	rest := msg[i+len(marker):]
	if j := strings.Index(rest, " "); j != -1 {
		return rest[:j]
	}
	return rest
}

// FindOne returns the first matching document, with _id mapped back to "id".
// No match -> dbswitch.ErrNotFound (mirrors Postgres's pgx.ErrNoRows mapping).
func (s *Store) FindOne(ctx context.Context, table string, where map[string]any) (map[string]any, error) {
	var doc bson.M
	err := s.db.Collection(table).FindOne(ctx, toMongoDoc(where)).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, dbswitch.ErrNotFound
		}
		return nil, err
	}
	return fromMongoDoc(doc), nil
}

// Find returns all matching documents (empty where = all). No matches is an
// empty slice, not an error — same convention as the Postgres backend.
func (s *Store) Find(ctx context.Context, table string, where map[string]any) ([]map[string]any, error) {
	cursor, err := s.db.Collection(table).Find(ctx, toMongoDoc(where))
	if err != nil {
		return nil, err
	}

	var docs []bson.M
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	results := make([]map[string]any, len(docs))
	for i, d := range docs {
		results[i] = fromMongoDoc(d)
	}
	return results, nil
}

// fromMongoDoc reverses toMongoDoc: renames _id -> id so callers always see
// the generic "id" field regardless of backend.
func fromMongoDoc(doc bson.M) map[string]any {
	out := make(map[string]any, len(doc))
	for k, v := range doc {
		if k == "_id" {
			out["id"] = v
			continue
		}
		out[k] = v
	}
	return out
}
// Update sets fields on all matching documents, returning how many matched.
// Refuses an empty where (an empty Mongo filter matches EVERY document).
func (s *Store) Update(ctx context.Context, table string, set, where map[string]any) (int64, error) {
	if len(set) == 0 {
		return 0, errors.New("dbswitch: update requires at least one column to set")
	}
	if len(where) == 0 {
		return 0, errors.New("dbswitch: update requires a WHERE condition (refusing to update all rows)")
	}

	res, err := s.db.Collection(table).UpdateMany(ctx, toMongoDoc(where), bson.M{"$set": toMongoDoc(set)})
	if err != nil {
		return 0, mapMongoError(err)
	}
	return res.MatchedCount, nil
}

// Delete removes all matching documents, returning how many were deleted.
// Refuses an empty where.
func (s *Store) Delete(ctx context.Context, table string, where map[string]any) (int64, error) {
	if len(where) == 0 {
		return 0, errors.New("dbswitch: delete requires a WHERE condition (refusing to delete all rows)")
	}

	res, err := s.db.Collection(table).DeleteMany(ctx, toMongoDoc(where))
	if err != nil {
		return 0, mapMongoError(err)
	}
	return res.DeletedCount, nil
}