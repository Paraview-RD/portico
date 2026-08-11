package store_test

import (
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/testdb"
	"github.com/Paraview-RD/portico/migrations"
)

// The migrations run backwards as well as forwards.
//
// docs/backup.md tells an operator that rolling a release back is a matter of
// running the down migrations, and nothing had ever run them. They did not
// work: the first migration's down section dropped twenty of the
// twenty-two tables it creates — `sessions` and `password_history` were never
// listed — and since both hold a foreign key to `users`, PostgreSQL refuses
// to drop `users` while they exist. The rollback therefore stopped partway,
// leaving a database with some tables gone and the schema version still
// recorded as applied: worse than either finishing or refusing.
//
// Checked by running them, because reading a list of DROP statements is
// exactly the activity that missed two of them.
func TestTheMigrationsRunBackwards(t *testing.T) {
	dsn := testdb.DSN(t)

	// Forwards first, through the same path the server uses.
	st, err := store.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open a second connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}

	if err := goose.DownTo(db, ".", 0); err != nil {
		t.Fatalf("roll the migrations back: %v", err)
	}

	// And nothing of the schema is left. A down that ran without error but
	// left tables behind would be the same trap in a quieter form: the next
	// `up` fails on something that already exists.
	var remaining []string
	rows, err := db.Query(`
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name <> 'goose_db_version'
		ORDER BY table_name`)
	if err != nil {
		t.Fatalf("list the remaining tables: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		remaining = append(remaining, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the remaining tables: %v", err)
	}
	if len(remaining) > 0 {
		t.Errorf("after rolling everything back these tables are still "+
			"there: %v; the next migration up fails on whichever it tries "+
			"to create first", remaining)
	}

	// Forwards again, which is what an operator does after deciding the
	// rollback was the wrong call. A down that leaves the database in a
	// state `up` cannot start from has not rolled anything back.
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("apply the migrations again after rolling back: %v", err)
	}
}
