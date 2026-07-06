package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/anukool23/dbswitch"
	"github.com/anukool23/dbswitch/postgres"
)

func main() {
	ctx := context.Background()

	db, err := postgres.Open(ctx, os.Getenv("DBSWITCH_TEST_POSTGRES_DSN"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	demo := dbswitch.Table{
		Name: "dbswitch_demo",
		Columns: []dbswitch.Column{
			{Name: "id", Type: dbswitch.TypeUUID, PrimaryKey: true, Default: dbswitch.DefaultGenerateUUID},
			{Name: "email", Type: dbswitch.TypeText, NotNull: true, Unique: true},
			{Name: "created_at", Type: dbswitch.TypeTimestamp, NotNull: true, Default: dbswitch.DefaultCurrentTime},
		},
	}

	if err := db.CreateTable(ctx, demo); err != nil {
		log.Fatal(err)
	}

	// after CreateTable(...)

	err = db.Insert(ctx, "dbswitch_demo", map[string]any{"email": "dup@demo.com"})
	fmt.Println("first insert:", err) // expect: <nil>

	err = db.Insert(ctx, "dbswitch_demo", map[string]any{"email": "dup@demo.com"})
	fmt.Println("second insert:", err) // expect: a duplicate error

	fmt.Println("is ErrDuplicate?", errors.Is(err, dbswitch.ErrDuplicate)) // true

	var dup *dbswitch.DuplicateError
	if errors.As(err, &dup) {
		fmt.Println("violated constraint:", dup.Constraint) // e.g. dbswitch_demo_email_key
	}

	// after the insert section

	found, err := db.FindOne(ctx, "dbswitch_demo", map[string]any{"email": "dup@demo.com"})
	fmt.Println("found:", found, "err:", err)

	_, err = db.FindOne(ctx, "dbswitch_demo", map[string]any{"email": "nobody@demo.com"})
	fmt.Println("missing lookup is ErrNotFound?", errors.Is(err, dbswitch.ErrNotFound)) // true
	fmt.Println("table created (or already existed)")

	n, err := db.Update(ctx, "dbswitch_demo",
		map[string]any{"email": "renamed@demo.com"},
		map[string]any{"email": "dup@demo.com"},
	)
	fmt.Println("updated rows:", n, "err:", err)

	n, err = db.Delete(ctx, "dbswitch_demo", map[string]any{"email": "renamed@demo.com"})
	fmt.Println("deleted rows:", n, "err:", err)

	_, err = db.Delete(ctx, "dbswitch_demo", map[string]any{})
	fmt.Println("empty-where delete refused:", err) // expect the guard error
}
