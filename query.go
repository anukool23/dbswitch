package dbswitch

import (
	"errors"
	"sort"
	"strconv"
	"strings"
)

// sortedKeys returns a map's keys in a stable, sorted order. This matters a
// lot: Go randomizes map iteration order on purpose, so if we ranged over a
// map directly, the generated SQL column order — and therefore which value
// lands on which placeholder — would change run to run. Sorting makes the
// output deterministic and keeps args aligned with their placeholders.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// BuildCreateTable renders a Table definition into a CREATE TABLE statement
// for the given dialect. DDL has no bind parameters, so it returns only SQL.
func BuildCreateTable(d Dialect, t Table) string {
	var b strings.Builder
	b.WriteString("CREATE TABLE IF NOT EXISTS ")
	b.WriteString(d.QuoteIdentifier(t.Name))
	b.WriteString(" (\n")

	for i, col := range t.Columns {
		b.WriteString("  ")
		b.WriteString(d.QuoteIdentifier(col.Name))
		b.WriteString(" ")
		b.WriteString(d.ColumnTypeSQL(col.Type))

		if col.PrimaryKey {
			b.WriteString(" PRIMARY KEY")
		}
		if col.Unique {
			b.WriteString(" UNIQUE")
		}
		if col.NotNull {
			b.WriteString(" NOT NULL")
		}
		if def := d.DefaultSQL(col.Default); def != "" {
			b.WriteString(" DEFAULT ")
			b.WriteString(def)
		}

		if i < len(t.Columns)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}

	b.WriteString(")")
	return b.String()
}

// BuildInsert renders an INSERT for one row. data maps column names to values.
// It returns the SQL plus the args slice, in matching order, for the driver to
// bind — values are never concatenated into the SQL string.
func BuildInsert(d Dialect, table string, data map[string]any) (string, []any) {
	cols := sortedKeys(data)

	quotedCols := make([]string, len(cols))
	placeholders := make([]string, len(cols))
	args := make([]any, len(cols))

	for i, c := range cols {
		quotedCols[i] = d.QuoteIdentifier(c)
		placeholders[i] = d.Placeholder(i + 1) // 1-based
		args[i] = data[c]
	}

	sql := "INSERT INTO " + d.QuoteIdentifier(table) +
		" (" + strings.Join(quotedCols, ", ") + ")" +
		" VALUES (" + strings.Join(placeholders, ", ") + ")"

	return sql, args
}

// buildWhere renders an equality WHERE clause from a map (all conditions
// ANDed together). startN is the placeholder number to begin at — this lets
// UPDATE number its SET placeholders first, then continue the WHERE from
// where they left off. Unexported: it's an internal helper, not public API.
func buildWhere(d Dialect, where map[string]any, startN int) (string, []any) {
	if len(where) == 0 {
		return "", nil
	}
	cols := sortedKeys(where)
	conds := make([]string, len(cols))
	args := make([]any, len(cols))
	for i, c := range cols {
		conds[i] = d.QuoteIdentifier(c) + " = " + d.Placeholder(startN+i)
		args[i] = where[c]
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// BuildSelect renders SELECT * FROM table with an optional WHERE. An empty
// where is allowed and means "select all rows".
func BuildSelect(d Dialect, table string, where map[string]any) (string, []any) {
	clause, args := buildWhere(d, where, 1)
	return "SELECT * FROM " + d.QuoteIdentifier(table) + clause, args
}

// BuildUpdate renders UPDATE table SET ... WHERE ... . It refuses an empty
// where to avoid accidentally rewriting every row.
func BuildUpdate(d Dialect, table string, set, where map[string]any) (string, []any, error) {
	if len(set) == 0 {
		return "", nil, errors.New("dbswitch: update requires at least one column to set")
	}
	if len(where) == 0 {
		return "", nil, errors.New("dbswitch: update requires a WHERE condition (refusing to update all rows)")
	}

	setCols := sortedKeys(set)
	assignments := make([]string, len(setCols))
	args := make([]any, 0, len(setCols)+len(where))

	for i, c := range setCols {
		assignments[i] = d.QuoteIdentifier(c) + " = " + d.Placeholder(i+1)
		args = append(args, set[c])
	}

	clause, whereArgs := buildWhere(d, where, len(setCols)+1)
	args = append(args, whereArgs...)

	sql := "UPDATE " + d.QuoteIdentifier(table) +
		" SET " + strings.Join(assignments, ", ") + clause
	return sql, args, nil
}

// BuildDelete renders DELETE FROM table WHERE ... . It refuses an empty where
// to avoid accidentally deleting every row.
func BuildDelete(d Dialect, table string, where map[string]any) (string, []any, error) {
	if len(where) == 0 {
		return "", nil, errors.New("dbswitch: delete requires a WHERE condition (refusing to delete all rows)")
	}
	clause, args := buildWhere(d, where, 1)
	return "DELETE FROM " + d.QuoteIdentifier(table) + clause, args, nil
}

// BuildList renders a SELECT with optional equality filters, a cursor range
// condition, ORDER BY, and LIMIT — from ListOptions.
func BuildList(d Dialect, table string, opts ListOptions) (string, []any) {
	var b strings.Builder
	b.WriteString("SELECT * FROM ")
	b.WriteString(d.QuoteIdentifier(table))

	conds := make([]string, 0)
	args := make([]any, 0)
	n := 1

	for _, k := range sortedKeys(opts.Filter) {
		conds = append(conds, d.QuoteIdentifier(k)+" = "+d.Placeholder(n))
		args = append(args, opts.Filter[k])
		n++
	}

	// Cursor: SortBy < After (desc) or SortBy > After (asc).
	if opts.After != nil && opts.SortBy != "" {
		op := ">"
		if opts.SortDir == Descending {
			op = "<"
		}
		conds = append(conds, d.QuoteIdentifier(opts.SortBy)+" "+op+" "+d.Placeholder(n))
		args = append(args, opts.After)
		n++
	}

	if len(conds) > 0 {
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(conds, " AND "))
	}

	if opts.SortBy != "" {
		b.WriteString(" ORDER BY ")
		b.WriteString(d.QuoteIdentifier(opts.SortBy))
		if opts.SortDir == Descending {
			b.WriteString(" DESC")
		} else {
			b.WriteString(" ASC")
		}
	}

	if opts.Limit > 0 {
		b.WriteString(" LIMIT ")
		b.WriteString(strconv.Itoa(opts.Limit))
	}

	return b.String(), args
}