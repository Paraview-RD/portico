package provision

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Looking after the tenants a public demonstration handed out.
//
// These are here rather than in the API for the reason the whole package is:
// there is no account that can act outside its own tenant, so there is nobody
// the API could authorize to list or delete one. A cross-tenant console would
// need a role that does not exist, and inventing one to tidy up a
// demonstration is the expensive answer to a cheap question.
//
// Everything here is bounded to tenants a trial created. A tenant that does
// not appear in trial_requests was provisioned by hand, and no amount of
// convenience justifies a command that could delete it by a typo.

// Trial is one confirmed trial and the tenant it produced.
type Trial struct {
	TenantCode  string
	TenantName  string
	Status      string
	Email       string
	Industry    string
	ConfirmedAt time.Time
	RequestIP   string
}

// ErrNotATrialTenant is a tenant that exists and was not created by a trial.
var ErrNotATrialTenant = errors.New(
	"no trial created that tenant; this command only removes tenants a trial handed out")

// ListTrials returns the tenants self-service trials have created, newest
// first.
func (p *Provisioner) ListTrials(ctx context.Context) ([]Trial, error) {
	rows, err := p.store.Queries.ListConfirmedTrials(ctx)
	if err != nil {
		return nil, fmt.Errorf("list trials: %w", err)
	}

	trials := make([]Trial, 0, len(rows))
	for _, r := range rows {
		t := Trial{
			TenantCode: r.TenantCode,
			TenantName: r.TenantName,
			Status:     r.TenantStatus,
			Email:      r.Email,
			Industry:   r.Industry,
			RequestIP:  r.RequestIp,
		}
		if r.ConfirmedAt != nil {
			t.ConfirmedAt = *r.ConfirmedAt
		}
		trials = append(trials, t)
	}
	return trials, nil
}

// PruneRequests deletes trial requests whose links expired without being
// confirmed, which is what returns their reserved tenant codes to
// circulation.
//
// The running server does this hourly. This exists for the case where there
// is no running server, or where somebody wants the codes back now rather
// than within the hour.
func (p *Provisioner) PruneRequests(ctx context.Context) (int64, error) {
	return p.store.Queries.DeleteExpiredTrialRequests(ctx)
}

// DeleteTrialTenant removes a trial tenant and everything inside it.
//
// Irreversible, and the only operation in this package that destroys data
// somebody could be using. Two things bound it. It refuses any tenant that no
// trial created, so the default tenant and anything provisioned by hand are
// out of reach. And it runs in one transaction, so a failure part-way leaves
// the tenant whole rather than half-emptied.
//
// The order of deletion is derived from the database rather than written
// down. Thirty-four tables carry a tenant_id and none of them cascade — a
// deliberate schema decision, since a cascade makes "delete this tenant" a
// single statement somebody can run by accident. A hand-maintained list would
// be wrong the first time somebody adds a table and forgets this file; asking
// the foreign keys which order they permit cannot go stale.
func (p *Provisioner) DeleteTrialTenant(ctx context.Context, code string) (DeletedTenant, error) {
	row, err := p.store.Queries.GetConfirmedTrialByTenantCode(ctx, code)
	if errors.Is(err, sql.ErrNoRows) {
		return DeletedTenant{}, ErrNotATrialTenant
	} else if err != nil {
		return DeletedTenant{}, fmt.Errorf("look up trial for tenant %s: %w", code, err)
	}
	if row.TenantID == nil {
		return DeletedTenant{}, ErrNotATrialTenant
	}
	tenantID := *row.TenantID

	loosen, err := nullableCrossReferences(ctx, p.store.DB())
	if err != nil {
		return DeletedTenant{}, err
	}
	order, err := tenantTableOrder(ctx, p.store.DB())
	if err != nil {
		return DeletedTenant{}, err
	}

	deleted := DeletedTenant{Code: code, Email: row.Email}

	// Its own transaction rather than store.WithTx, which hands out a
	// *sqlcgen.Queries — these statements name their table at runtime, so
	// there is nothing generated to call.
	err = inTx(ctx, p.store.DB(), func(tx *sql.Tx) error {
		// Break the cycles first.
		//
		// An organization names a manager and an account names an
		// organization, so those two tables reference each other and no order
		// empties both. Both columns are nullable, which is what makes it
		// solvable: cleared, the edge is gone and what remains is a tree.
		//
		// Every nullable cross-table reference is cleared rather than only the
		// ones in a cycle. Finding the cycles would be more code to decide
		// something that does not matter — these rows are about to be deleted,
		// and a column set to null on the way out costs one statement.
		for _, ref := range loosen {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(
				"UPDATE %s SET %s = NULL WHERE tenant_id = $1",
				quoteIdent(ref.table), quoteIdent(ref.column)), tenantID); err != nil {
				return fmt.Errorf("clear %s.%s: %w", ref.table, ref.column, err)
			}
		}

		for _, table := range order {
			// The identifier is not a parameter — it cannot be — so it comes
			// from the catalogue and nowhere else. tenantTableOrder reads
			// information_schema, so every name here is a table that exists in
			// this database; nothing a caller supplies reaches this string.
			res, err := tx.ExecContext(ctx,
				fmt.Sprintf("DELETE FROM %s WHERE tenant_id = $1", quoteIdent(table)), tenantID)
			if err != nil {
				return fmt.Errorf("clear %s: %w", table, err)
			}
			if n, err := res.RowsAffected(); err == nil {
				deleted.Rows += n
			}
		}

		// The request row last but one: it is what points at the tenant.
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM trial_requests WHERE tenant_id = $1", tenantID); err != nil {
			return fmt.Errorf("clear the trial request: %w", err)
		}

		res, err := tx.ExecContext(ctx, "DELETE FROM tenants WHERE id = $1", tenantID)
		if err != nil {
			return fmt.Errorf("delete tenant %s: %w", code, err)
		}
		if n, err := res.RowsAffected(); err == nil && n == 0 {
			return fmt.Errorf("tenant %s vanished while it was being deleted", code)
		}
		return nil
	})
	if err != nil {
		return DeletedTenant{}, err
	}
	return deleted, nil
}

// DeletedTenant is what a delete removed, for the caller to report.
type DeletedTenant struct {
	Code  string
	Email string
	// Rows is everything removed from the tenant-scoped tables, not counting
	// the tenant row itself. A number rather than a breakdown: what it answers
	// is "was that as big as I thought", and a per-table listing of thirty-four
	// mostly-zero counts answers it worse.
	Rows int64
}

// tenantTableOrder returns the tenant-scoped tables in an order that satisfies
// their foreign keys: a table that references another comes first.
//
// Self-references are ignored. An organization's parent is another
// organization, and deleting a whole tenant's worth in one statement is fine
// — PostgreSQL checks a non-deferrable foreign key at the end of the
// statement, by which time both the parent and the child are gone.
func tenantTableOrder(ctx context.Context, db *sql.DB) ([]string, error) {
	tables, err := tenantScopedTables(ctx, db)
	if err != nil {
		return nil, err
	}

	// references[child] is every tenant-scoped table the child points at.
	references := map[string]map[string]bool{}
	for t := range tables {
		references[t] = map[string]bool{}
	}

	constraints, err := foreignKeys(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, fk := range constraints {
		// A constraint with a nullable column is not an edge: it is cleared
		// before anything is deleted, and counting it would report a cycle
		// that has already been broken.
		if fk.breakableBy != "" {
			continue
		}
		if fk.child == fk.parent || !tables[fk.child] || !tables[fk.parent] {
			continue
		}
		references[fk.child][fk.parent] = true
	}

	// Built parents-first, then reversed.
	//
	// A row may only go once nothing still points at it, so a table has to be
	// emptied before any table it references. That is the reverse of what this
	// loop produces: it takes tables whose own references are already
	// accounted for — the ones nothing in this set depends on reaching —
	// which is the deletion order read backwards.
	//
	// Sorted within each layer so two runs against the same schema produce the
	// same order. A failure that happens in a different place each time is one
	// nobody can reproduce.
	var order []string
	settled := map[string]bool{}
	for len(settled) < len(tables) {
		var layer []string
		for t := range tables {
			if settled[t] {
				continue
			}
			ready := true
			for parent := range references[t] {
				if !settled[parent] {
					ready = false
					break
				}
			}
			if ready {
				layer = append(layer, t)
			}
		}
		if len(layer) == 0 {
			return nil, errors.New(
				"the tenant-scoped tables reference each other in a cycle; " +
					"deleting a tenant needs an order and there is not one")
		}
		sortStrings(layer)
		order = append(order, layer...)
		for _, t := range layer {
			settled[t] = true
		}
	}

	reverse(order)
	return order, nil
}

// crossReference is one nullable foreign-key column pointing at another
// tenant-scoped table.
type crossReference struct{ table, column string }

// foreignKey is one constraint between two tables, and whether it can be
// stood down.
type foreignKey struct {
	child, parent string
	// breakableBy is a nullable column of this constraint, or empty when every
	// column is NOT NULL.
	//
	// One column is enough. PostgreSQL's default MATCH SIMPLE does not check a
	// foreign key at all once any of its columns is null — so clearing one
	// column of a composite key stands the whole constraint down, and asking
	// per column would call a key unbreakable because its tenant_id half is
	// NOT NULL.
	breakableBy string
}

// foreignKeys reads every foreign key between tables in the public schema,
// grouped by constraint rather than by column.
func foreignKeys(ctx context.Context, db *sql.DB) ([]foreignKey, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT tc.table_name,
		       max(ccu.table_name) AS parent,
		       coalesce(min(kcu.column_name) FILTER (
		           WHERE c.is_nullable = 'YES' AND kcu.column_name <> 'tenant_id'
		       ), '') AS breakable_by
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_name = tc.constraint_name
		 AND kcu.table_schema = tc.table_schema
		JOIN information_schema.constraint_column_usage ccu
		  ON ccu.constraint_name = tc.constraint_name
		 AND ccu.table_schema = tc.table_schema
		JOIN information_schema.columns c
		  ON c.table_name = tc.table_name
		 AND c.column_name = kcu.column_name
		 AND c.table_schema = tc.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_schema = 'public'
		GROUP BY tc.constraint_name, tc.table_name
		ORDER BY tc.table_name, tc.constraint_name`)
	if err != nil {
		return nil, fmt.Errorf("read foreign keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var keys []foreignKey
	for rows.Next() {
		var fk foreignKey
		if err := rows.Scan(&fk.child, &fk.parent, &fk.breakableBy); err != nil {
			return nil, fmt.Errorf("read a foreign key: %w", err)
		}
		keys = append(keys, fk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read foreign keys: %w", err)
	}
	return keys, nil
}

// nullableCrossReferences returns the columns to clear before deleting, which
// is what breaks the cycles between tenant-scoped tables.
//
// tenant_id is never one of them: it is what every row is being selected by,
// and clearing it would orphan the row instead of deleting it.
func nullableCrossReferences(ctx context.Context, db *sql.DB) ([]crossReference, error) {
	tables, err := tenantScopedTables(ctx, db)
	if err != nil {
		return nil, err
	}
	constraints, err := foreignKeys(ctx, db)
	if err != nil {
		return nil, err
	}

	var refs []crossReference
	seen := map[crossReference]bool{}
	for _, fk := range constraints {
		if fk.breakableBy == "" || !tables[fk.child] || !tables[fk.parent] {
			continue
		}
		ref := crossReference{table: fk.child, column: fk.breakableBy}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	return refs, nil
}

// tenantScopedTables is every base table carrying a tenant_id, except
// trial_requests — which is handled separately because its tenant_id is an
// outcome rather than an owner, and because it is what points at the tenant
// row being removed.
func tenantScopedTables(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.table_name
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_name = c.table_name AND t.table_schema = c.table_schema
		WHERE c.table_schema = 'public'
		  AND c.column_name = 'tenant_id'
		  AND t.table_type = 'BASE TABLE'
		  AND c.table_name <> 'trial_requests'`)
	if err != nil {
		return nil, fmt.Errorf("read tenant-scoped tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tables := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read a table name: %w", err)
		}
		tables[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read tenant-scoped tables: %w", err)
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("no tenant-scoped tables found; is this a Portico database?")
	}
	return tables, nil
}

// inTx runs fn in a transaction, rolling back on error or panic.
func inTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Rollback after a successful Commit is a no-op, so this is safe
	// unconditionally — the same arrangement as store.WithTx.
	defer func() { _ = tx.Rollback() }()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// quoteIdent makes a table name safe to interpolate. Every name reaching it
// came from information_schema, so this is belt to the catalogue's braces.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func reverse(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
