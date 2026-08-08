package store_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Tenant isolation is the one property in this system where a single
// oversight is a breach rather than a bug: a query missing its tenant_id
// predicate returns another tenant's rows and looks, in a diff, exactly like
// a query that is merely less specific. Reviewers do not reliably catch
// that. These tests do.
//
// They are deliberately crude — they read SQL as text rather than parsing it
// — because a guard that is hard to understand is a guard people delete. The
// behavioural proof that isolation actually holds is in
// internal/server/tenancy_test.go; these only ensure that a new query
// cannot quietly opt out of it.

// unscopedQueries are the queries allowed to touch a tenant-scoped table
// without filtering by tenant.
//
// There are two, and the test below asserts that count. The point is not
// that two is a magic number — it is that adding another has to be a
// deliberate act that edits this list, rather than an omission that nothing
// notices.
//
// Both are the same shape, and it is the only shape that qualifies: a query
// that *determines* the tenant cannot filter by it, because filtering would
// mean already knowing the answer. Everything downstream of them is scoped
// to whatever they returned. Any other candidate is a query that could take
// a tenant and did not.
var unscopedQueries = map[string]string{
	"GetUserForAuthentication": "runs before the tenant is known; it is what establishes it",
	"GetSCIMCredentialByTokenHash": "a SCIM client presents a bearer token and " +
		"nothing else; the credential row is what says which tenant it acts in",
}

func TestOnlyTheTenantResolvingQueriesAreUnscoped(t *testing.T) {
	if len(unscopedQueries) != 2 {
		t.Fatalf("the unscoped-query allowlist has %d entries, want 2.\n"+
			"Adding one is a decision about the isolation boundary, not a "+
			"detail: say here why the new query cannot be scoped, and make "+
			"sure something else checks the tenant it ends up acting on.",
			len(unscopedQueries))
	}
}

func TestTenantScopedQueriesFilterByTenant(t *testing.T) {
	scoped := scopedTables(t)
	if len(scoped) == 0 {
		t.Fatal("found no tenant-scoped tables in the schema; the guard would pass vacuously")
	}

	for _, path := range globOrFail(t, filepath.Join("queries", "*.sql")) {
		source := readFile(t, path)

		for _, stmt := range splitNamedQueries(source) {
			if _, allowed := unscopedQueries[stmt.name]; allowed {
				continue
			}

			table, touched := referencedScopedTable(stmt.body, scoped)
			if !touched {
				continue
			}
			if !strings.Contains(flatten(stmt.body), "tenant_id") {
				t.Errorf("%s: query %s reads or writes %s without filtering on tenant_id.\n"+
					"Every statement on a tenant-scoped table must constrain "+
					"tenant_id, or it will act on other tenants' rows.",
					filepath.Base(path), stmt.name, table)
			}
		}
	}
}

// Not every query can be generated: a list endpoint whose WHERE clause
// depends on which filters the caller supplied has to be assembled at
// runtime. Those live in Go string literals, where the guard above cannot
// see them, so they are checked here instead.
func TestHandWrittenSQLFiltersByTenant(t *testing.T) {
	scoped := scopedTables(t)

	dirs := []string{".", filepath.Join("..", "service"), filepath.Join("..", "handler")}
	checked := 0

	fset := token.NewFileSet()
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}

		for _, entry := range entries {
			name := entry.Name()
			// Generated code is covered by the query guard above, and test
			// files legitimately write unscoped SQL to set up fixtures.
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}

			path := filepath.Join(dir, name)
			syntax, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}

			ast.Inspect(syntax, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				table, touched := referencedScopedTable(value, scoped)
				if !touched || !looksLikeSQL(value) {
					return true
				}
				checked++
				if !strings.Contains(flatten(value), "tenant_id") {
					t.Errorf("%s: hand-written SQL touches %s without a tenant_id predicate:\n\t%s",
						name, table, strings.TrimSpace(value))
				}
				return true
			})
		}
	}

	// A guard that silently stops finding anything to check is worse than no
	// guard, because it still reports success.
	if checked == 0 {
		t.Fatal("found no hand-written SQL to check; either it moved or this test stopped working")
	}
}

type namedQuery struct {
	name string
	body string
}

// splitNamedQueries breaks a sqlc query file into its "-- name: X :kind"
// sections.
func splitNamedQueries(source string) []namedQuery {
	nameLine := regexp.MustCompile(`(?m)^--\s*name:\s*(\w+)\s`)
	matches := nameLine.FindAllStringSubmatchIndex(source, -1)

	queries := make([]namedQuery, 0, len(matches))
	for i, m := range matches {
		end := len(source)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		queries = append(queries, namedQuery{
			name: source[m[2]:m[3]],
			body: source[m[1]:end],
		})
	}
	return queries
}

// scopedTables reads the schema and returns the tables that carry a
// tenant_id column. Deriving this from the migration rather than listing it
// here means a new scoped table is covered the moment it is created.
func scopedTables(t *testing.T) []string {
	t.Helper()

	var schema strings.Builder
	for _, path := range globOrFail(t, filepath.Join("..", "..", "migrations", "*.sql")) {
		schema.WriteString(readFile(t, path))
	}

	createTable := regexp.MustCompile(`(?is)CREATE TABLE (\w+) \((.*?)\n\);`)
	var tables []string
	for _, m := range createTable.FindAllStringSubmatch(schema.String(), -1) {
		if strings.Contains(m[2], "tenant_id") {
			tables = append(tables, m[1])
		}
	}
	return tables
}

// flatten collapses every run of whitespace to a single space.
//
// Without this the guard is defeated by formatting alone: a query written
// as "FROM\n    users" does not contain "from users", so it matches nothing
// and is skipped in silence. That is the worst way for a guard to fail,
// because the suite still passes and nobody is told the query went
// unchecked.
func flatten(s string) string { return strings.Join(strings.Fields(s), " ") }

// referencedScopedTable reports which scoped table a statement touches, if
// any. The keyword prefix keeps "users" from matching inside
// "system_settings" or in prose about users; the schema qualifier is
// accepted because "FROM public.users" is the same table.
func referencedScopedTable(sql string, scoped []string) (string, bool) {
	lower := strings.ToLower(flatten(sql))
	for _, table := range scoped {
		for _, keyword := range []string{"from ", "join ", "into ", "update "} {
			if strings.Contains(lower, keyword+table) ||
				strings.Contains(lower, keyword+"public."+table) {
				return table, true
			}
		}
	}
	return "", false
}

// looksLikeSQL filters out prose that happens to mention a table name, such
// as an error message or a doc comment moved into a constant.
func looksLikeSQL(s string) bool {
	upper := strings.ToUpper(flatten(s))
	return strings.Contains(upper, "SELECT ") ||
		strings.Contains(upper, "INSERT ") ||
		strings.Contains(upper, "UPDATE ") ||
		strings.Contains(upper, "DELETE ")
}

func globOrFail(t *testing.T, pattern string) []string {
	t.Helper()
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no files matched %s", pattern)
	}
	return paths
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// A hand-written SELECT that names its columns has to name all of them.
//
// The user listing cannot be generated — its WHERE clause depends on which
// filters the caller supplied — so its column list is written out by hand
// and has to be kept in step with the table. It was not: adding the lockout
// columns left the listing scanning the old set, and the symptom was an
// administrator seeing no lock on an account that was locked. Nothing else
// noticed, because every other read of a user goes through a generated
// query.
func TestHandWrittenUserSelectNamesEveryColumn(t *testing.T) {
	columns := tableColumns(t, "users")
	if len(columns) == 0 {
		t.Fatal("found no columns for users; the guard would pass vacuously")
	}

	source := readFile(t, filepath.Join("..", "service", "user.go"))
	flat := flatten(source)

	// The listing is the only hand-written SELECT over users. Locating it by
	// the FROM clause rather than by line number so that moving it does not
	// silently stop this checking anything.
	if !strings.Contains(flat, "FROM users WHERE tenant_id = $1") {
		t.Fatal("could not find the hand-written user listing; either it moved " +
			"or this test stopped working")
	}

	for _, column := range columns {
		// password_hash is selected but never returned to a caller; every
		// column has to be *scannable*, which is what this is about.
		if !strings.Contains(flat, column) {
			t.Errorf("the hand-written user listing does not select %q.\n"+
				"Every column of users must appear, or a row scanned from it "+
				"carries a zero value that looks like real data — an unset "+
				"lock, an empty organization — with nothing to say it was "+
				"never read.", column)
		}
	}
}

// tableColumns reads a table's columns out of the migration.
func tableColumns(t *testing.T, table string) []string {
	t.Helper()

	var schema strings.Builder
	for _, path := range globOrFail(t, filepath.Join("..", "..", "migrations", "*.sql")) {
		schema.WriteString(readFile(t, path))
	}

	createTable := regexp.MustCompile(`(?is)CREATE TABLE ` + table + ` \((.*?)\n\);`)
	m := createTable.FindStringSubmatch(schema.String())
	if m == nil {
		return nil
	}

	// Column definitions are the lines that start with an identifier; the
	// rest are constraints and comments.
	column := regexp.MustCompile(`(?m)^\s{4}([a-z_]+)\s`)
	var columns []string
	for _, found := range column.FindAllStringSubmatch(m[1], -1) {
		switch found[1] {
		case "constraint", "foreign", "primary", "unique", "check":
			continue
		}
		columns = append(columns, found[1])
	}
	return columns
}
