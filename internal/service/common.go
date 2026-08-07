package service

import (
	"fmt"
	"strings"
)

// Page is the limit/offset pair a list query runs with. Handlers translate
// the API's page/pageSize into this.
type Page struct {
	Limit  int
	Offset int
}

// escapeLike neutralizes the wildcards in a user-supplied search term so a
// keyword of "%" does not match every row.
//
// The queries that use this declare ESCAPE '\'.
func escapeLike(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	)
	return replacer.Replace(s)
}

// filters accumulates the optional WHERE conditions of a list query along
// with their arguments.
//
// PostgreSQL numbers its placeholders ($1, $2, …), so a query assembled from
// a varying set of filters cannot use a fixed template — the numbering
// depends on which filters were supplied. This keeps the numbering and the
// argument slice in step by construction, which is the part that is easy to
// get wrong by hand and produces either a runtime error or, worse, arguments
// silently bound to the wrong condition.
//
// Only fixed string literals are ever passed to Add. Values go through
// placeholders, never into the SQL text.
type filters struct {
	conditions []string
	args       []any
}

// tenantFilters returns a filter set whose first bound argument is the
// tenant, so $1 in the caller's base query is the tenant and the optional
// filters number from $2.
//
// The tenant predicate itself stays written out in the caller's SQL rather
// than being added here. That is deliberate: it means the tenant constraint
// is visible when reading the query, and it is what lets the guard test in
// internal/store check these hand-written statements the same way it checks
// the generated ones.
func tenantFilters(tenantID string) *filters {
	return &filters{args: []any{tenantID}}
}

// Add appends a condition. Use %s in the format for each placeholder; they
// are numbered automatically in the order the values are given.
//
//	f.Add("status = %s", status)
//	f.Add("(username LIKE %s ESCAPE '\\' OR display_name LIKE %s ESCAPE '\\')", p, p)
func (f *filters) Add(format string, values ...any) {
	placeholders := make([]any, len(values))
	for i, v := range values {
		f.args = append(f.args, v)
		placeholders[i] = fmt.Sprintf("$%d", len(f.args))
	}
	f.conditions = append(f.conditions, fmt.Sprintf(format, placeholders...))
}

// And returns the optional conditions as a continuation of a WHERE clause
// the caller has already opened with its tenant predicate, or an empty
// string when no filter was supplied.
func (f *filters) And() string {
	if len(f.conditions) == 0 {
		return ""
	}
	return " AND " + strings.Join(f.conditions, " AND ")
}

// Args returns the arguments bound so far, in placeholder order.
func (f *filters) Args() []any { return f.args }

// Paginate appends LIMIT and OFFSET and returns the full argument list. It
// is called last, after every filter, so the numbering follows on.
func (f *filters) Paginate(page Page) (clause string, args []any) {
	limit := fmt.Sprintf("$%d", len(f.args)+1)
	offset := fmt.Sprintf("$%d", len(f.args)+2)
	return fmt.Sprintf(" LIMIT %s OFFSET %s", limit, offset),
		append(append([]any{}, f.args...), page.Limit, page.Offset)
}
