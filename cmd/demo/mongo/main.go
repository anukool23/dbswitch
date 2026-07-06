package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/anukool23/dbswitch"
	"github.com/anukool23/dbswitch/mongo"
)

func main() {
	ctx := context.Background()

	store, err := mongo.Open(ctx, os.Getenv("DBSWITCH_TEST_MONGO_URI"), "dbswitch_test")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	users := dbswitch.Table{
		Name: "users",
		Columns: []dbswitch.Column{
			{Name: "id", Type: dbswitch.TypeUUID, PrimaryKey: true},
			{Name: "email", Type: dbswitch.TypeText, Unique: true, NotNull: true},
		},
	}
	if err := store.CreateTable(ctx, users); err != nil {
		log.Fatal("createtable:", err)
	}

	id := fmt.Sprintf("user-%d", time.Now().UnixNano())
	email := fmt.Sprintf("u%d@lumea.ink", time.Now().UnixNano())

	if err := store.Insert(ctx, "users", map[string]any{"id": id, "email": email}); err != nil {
		log.Fatal("insert:", err)
	}
	fmt.Println("inserted:", id)

	// duplicate email -> same dbswitch.DuplicateError as Postgres
	err = store.Insert(ctx, "users", map[string]any{"id": "x", "email": email})
	fmt.Println("dup is ErrDuplicate?", errors.Is(err, dbswitch.ErrDuplicate))
	var dup *dbswitch.DuplicateError
	if errors.As(err, &dup) {
		fmt.Println("constraint:", dup.Constraint) // e.g. email_1
	}

	found, err := store.FindOne(ctx, "users", map[string]any{"email": email})
	fmt.Printf("found: %v err: %v\n", found, err) // note: id is a plain string, no [16]byte

	_, err = store.FindOne(ctx, "users", map[string]any{"email": "nobody@lumea.ink"})
	fmt.Println("missing is ErrNotFound?", errors.Is(err, dbswitch.ErrNotFound))

	n, err := store.Update(ctx, "users",
		map[string]any{"email": "renamed-" + id + "@lumea.ink"},
		map[string]any{"id": id})
	fmt.Println("updated:", n, err)

	n, err = store.Delete(ctx, "users", map[string]any{"id": id})
	fmt.Println("deleted:", n, err)

	_, err = store.Delete(ctx, "users", map[string]any{})
	fmt.Println("empty-where delete refused:", err)
}