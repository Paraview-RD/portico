package store

import (
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// SQLSTATE codes this package recognizes. The full list is in the PostgreSQL
// manual, appendix A.
const (
	// sqlStateUniqueViolation is raised when an INSERT or UPDATE conflicts
	// with a unique index.
	sqlStateUniqueViolation = "23505"
	// sqlStateForeignKeyViolation is raised when a referenced row is absent.
	sqlStateForeignKeyViolation = "23503"
)

// IsNoRows reports whether err means "no such row", which several callers
// treat as a normal absence rather than a failure.
func IsNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// IsUniqueViolation reports whether err is a unique-constraint failure.
//
// This is matched on the SQLSTATE rather than on the message text: the
// message is localized by the server's lc_messages and its wording is not
// part of any compatibility promise, so a text match is a rule that silently
// stops working. It has already done so once here — the previous version
// matched SQLite's wording and returned false for every PostgreSQL error,
// which turned a lost insert race into a 500 instead of a 409.
func IsUniqueViolation(err error) bool { return hasSQLState(err, sqlStateUniqueViolation) }

// IsForeignKeyViolation reports whether err is a foreign-key failure, which
// is how the database rejects a reference to a row that does not exist.
func IsForeignKeyViolation(err error) bool { return hasSQLState(err, sqlStateForeignKeyViolation) }

func hasSQLState(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.SQLState() == code
}
