package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/anukool23/dbswitch"
	"github.com/anukool23/dbswitch/dynamodb"
)

func main() {
	ctx := context.Background()

	var opts []func(*awsdynamodb.Options)
	if endpoint := os.Getenv("DBSWITCH_TEST_DYNAMODB_ENDPOINT"); endpoint != "" {
		// e.g. http://localhost:8000 for DynamoDB Local. Region/credentials
		// still come from the normal AWS env vars (dummy values are fine
		// against DynamoDB Local: AWS_ACCESS_KEY_ID=test, AWS_SECRET_ACCESS_KEY=test,
		// AWS_REGION=us-east-1).
		opts = append(opts, func(o *awsdynamodb.Options) { o.BaseEndpoint = aws.String(endpoint) })
	}

	store, err := dynamodb.Open(ctx, opts...)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Unlike the Postgres/Mongo demos, "email" is NOT marked Unique here:
	// DynamoDB only enforces uniqueness on the primary key (see the
	// dynamodb package doc) — declaring a non-key column Unique makes
	// CreateTable return an error on this backend.
	users := dbswitch.Table{
		Name: "dbswitch_demo_users",
		Columns: []dbswitch.Column{
			{Name: "id", Type: dbswitch.TypeText, PrimaryKey: true},
			{Name: "email", Type: dbswitch.TypeText, NotNull: true},
		},
	}
	if err := store.CreateTable(ctx, users); err != nil {
		log.Fatal("createtable:", err)
	}

	id := fmt.Sprintf("user-%d", time.Now().UnixNano())
	email := fmt.Sprintf("u%d@lumea.ink", time.Now().UnixNano())

	if err := store.Insert(ctx, "dbswitch_demo_users", map[string]any{"id": id, "email": email}); err != nil {
		log.Fatal("insert:", err)
	}
	fmt.Println("inserted:", id)

	// duplicate id -> same dbswitch.DuplicateError as Postgres/Mongo
	err = store.Insert(ctx, "dbswitch_demo_users", map[string]any{"id": id, "email": "other@lumea.ink"})
	fmt.Println("dup is ErrDuplicate?", errors.Is(err, dbswitch.ErrDuplicate))
	var dup *dbswitch.DuplicateError
	if errors.As(err, &dup) {
		fmt.Println("constraint:", dup.Constraint) // e.g. dbswitch_demo_users.id
	}

	found, err := store.FindOne(ctx, "dbswitch_demo_users", map[string]any{"id": id})
	fmt.Printf("found by id (GetItem): %v err: %v\n", found, err)

	// email isn't the key, so this lookup falls back to a filtered Scan.
	byEmail, err := store.FindOne(ctx, "dbswitch_demo_users", map[string]any{"email": email})
	fmt.Printf("found by email (scan): %v err: %v\n", byEmail, err)

	_, err = store.FindOne(ctx, "dbswitch_demo_users", map[string]any{"id": "nobody"})
	fmt.Println("missing is ErrNotFound?", errors.Is(err, dbswitch.ErrNotFound))

	n, err := store.Update(ctx, "dbswitch_demo_users",
		map[string]any{"email": "renamed-" + id + "@lumea.ink"},
		map[string]any{"id": id})
	fmt.Println("updated:", n, err)

	n, err = store.Delete(ctx, "dbswitch_demo_users", map[string]any{"id": id})
	fmt.Println("deleted:", n, err)

	_, err = store.Delete(ctx, "dbswitch_demo_users", map[string]any{})
	fmt.Println("empty-where delete refused:", err)
}
