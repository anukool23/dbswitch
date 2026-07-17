package dbswitch

import (
	"fmt"
	"reflect"
	"testing"
)

// fakeDialect is a minimal, predictable Dialect used to test the builders in
// isolation. Because the builders only need the interface, we don't need a
// real database (or even the postgres package) to verify how SQL is assembled.
type fakeDialect struct{}

func (fakeDialect) Placeholder(n int) string           { return fmt.Sprintf("$%d", n) }
func (fakeDialect) QuoteIdentifier(name string) string { return `"` + name + `"` }
func (fakeDialect) ColumnTypeSQL(t ColumnType) string  { return "TYPE" }
func (fakeDialect) DefaultSQL(d DefaultValue) string {
	if d == DefaultNone {
		return ""
	}
	return "DEFAULT_EXPR"
}

func TestBuildInsert(t *testing.T) {
	sql, args := BuildInsert(fakeDialect{}, "users", map[string]any{
		"email": "a@b.com",
		"name":  "A",
	})

	wantSQL := `INSERT INTO "users" ("email", "name") VALUES ($1, $2)`
	if sql != wantSQL {
		t.Errorf("sql\n got: %q\nwant: %q", sql, wantSQL)
	}
	wantArgs := []any{"a@b.com", "A"} // sorted key order: email, name
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestBuildSelect(t *testing.T) {
	sql, args := BuildSelect(fakeDialect{}, "users", map[string]any{"email": "a@b.com"})
	if want := `SELECT * FROM "users" WHERE "email" = $1`; sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{"a@b.com"}) {
		t.Errorf("args = %v", args)
	}

	// empty where -> no WHERE clause, no args
	sql, args = BuildSelect(fakeDialect{}, "users", nil)
	if want := `SELECT * FROM "users"`; sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got %v", args)
	}
}

func TestBuildUpdate_placeholderThreading(t *testing.T) {
	sql, args, err := BuildUpdate(fakeDialect{}, "users",
		map[string]any{"name": "New", "role": "ADMIN"}, // SET: $1, $2 (sorted: name, role)
		map[string]any{"id": "123"},                    // WHERE: continues at $3
	)
	if err != nil {
		t.Fatal(err)
	}
	wantSQL := `UPDATE "users" SET "name" = $1, "role" = $2 WHERE "id" = $3`
	if sql != wantSQL {
		t.Errorf("sql\n got: %q\nwant: %q", sql, wantSQL)
	}
	if !reflect.DeepEqual(args, []any{"New", "ADMIN", "123"}) {
		t.Errorf("args = %v", args)
	}
}

func TestBuildUpdate_and_Delete_refuseEmptyWhere(t *testing.T) {
	if _, _, err := BuildUpdate(fakeDialect{}, "users", map[string]any{"x": 1}, nil); err == nil {
		t.Error("BuildUpdate: expected error for empty where, got nil")
	}
	if _, _, err := BuildDelete(fakeDialect{}, "users", nil); err == nil {
		t.Error("BuildDelete: expected error for empty where, got nil")
	}
}

func TestBuildList(t *testing.T) {
	sql, args := BuildList(fakeDialect{}, "posts", ListOptions{
		Filter:  map[string]any{"status": "PUBLISHED"},
		SortBy:  "publishedAt",
		SortDir: Descending,
		Limit:   20,
		After:   "2026-07-01",
	})

	want := `SELECT * FROM "posts" WHERE "status" = $1 AND "publishedAt" < $2 ORDER BY "publishedAt" DESC LIMIT 20`
	if sql != want {
		t.Errorf("sql\n got: %q\nwant: %q", sql, want)
	}
	if !reflect.DeepEqual(args, []any{"PUBLISHED", "2026-07-01"}) {
		t.Errorf("args = %v", args)
	}
}

func TestBuildList_empty(t *testing.T) {
	sql, args := BuildList(fakeDialect{}, "posts", ListOptions{})
	if want := `SELECT * FROM "posts"`; sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got %v", args)
	}
}
func TestBuildList_offset(t *testing.T) {
	sql, _ := BuildList(fakeDialect{}, "notifications", ListOptions{
		Filter: map[string]any{"userId": "u1"},
		SortBy: "createdAt", SortDir: Descending,
		Limit: 20, Offset: 40, // page 3
	})
	want := `SELECT * FROM "notifications" WHERE "userId" = $1 ORDER BY "createdAt" DESC LIMIT 20 OFFSET 40`
	if sql != want {
		t.Errorf("sql\n got: %q\nwant: %q", sql, want)
	}
}

func TestBuildCount(t *testing.T) {
	sql, args := BuildCount(fakeDialect{}, "notifications", map[string]any{"userId": "u1", "read": false})
	want := `SELECT COUNT(*) FROM "notifications" WHERE "read" = $1 AND "userId" = $2`
	if sql != want {
		t.Errorf("sql\n got: %q\nwant: %q", sql, want)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, want 2", args)
	}
}

func TestBuildCount_noFilter(t *testing.T) {
	sql, args := BuildCount(fakeDialect{}, "notifications", nil)
	if want := `SELECT COUNT(*) FROM "notifications"`; sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got %v", args)
	}
}
