package dbswitch

// SortDirection controls list ordering.
type SortDirection int

const (
	Ascending SortDirection = iota // default
	Descending
)

// ListOptions describes a paginated, sorted query. Every field is optional;
// the zero value is "all rows, unordered, no limit" (same as Find).
type ListOptions struct {
	// Filter holds equality conditions, ANDed together (e.g. status=PUBLISHED).
	Filter map[string]any

	// SortBy is the field to order by. Empty means no explicit ordering.
	SortBy  string
	SortDir SortDirection

	// Limit caps the number of returned rows. 0 means no limit.
	Limit int

	// After enables cursor pagination: when set together with SortBy, only rows
	// *past* this value in the sort direction are returned — SortBy < After for
	// Descending, SortBy > After for Ascending. Pass the SortBy value of the
	// last row from the previous page.
	After any
}
