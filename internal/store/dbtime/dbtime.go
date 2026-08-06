// Package dbtime bridges Go time values and the ISO 8601 text that SQLite
// stores.
//
// The sqlite driver hands back a string for a TEXT column and will not scan
// it into a time.Time, so every timestamp column is generated as this type
// instead (see the overrides in sqlc.yaml). Storing text rather than an
// integer keeps the database readable from the sqlite3 CLI and sorts
// correctly, and this type keeps that choice from leaking into the rest of
// the code.
package dbtime

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// layout is the on-disk format: RFC 3339 in UTC, with second precision.
const layout = time.RFC3339

// Time wraps time.Time with database serialization. The embedded value is
// always UTC.
type Time struct {
	time.Time
}

// New returns a Time holding t, normalized to UTC and truncated to seconds
// so that what is written matches what is read back.
func New(t time.Time) Time {
	return Time{Time: t.UTC().Truncate(time.Second)}
}

// Now returns the current time, ready to be stored.
func Now() Time { return New(time.Now()) }

// Scan implements sql.Scanner, accepting the text the driver returns as well
// as the time.Time and integer forms other drivers may produce.
func (t *Time) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		t.Time = time.Time{}
		return nil
	case time.Time:
		t.Time = v.UTC()
		return nil
	case string:
		return t.parse(v)
	case []byte:
		return t.parse(string(v))
	case int64:
		// Unix seconds, for a database written by a different tool.
		t.Time = time.Unix(v, 0).UTC()
		return nil
	default:
		return fmt.Errorf("dbtime: cannot scan %T into Time", src)
	}
}

func (t *Time) parse(s string) error {
	if s == "" {
		t.Time = time.Time{}
		return nil
	}
	// Accept the formats SQLite's own date functions emit, so a row inserted
	// by hand from the CLI still reads back correctly.
	for _, l := range []string{layout, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(l, s); err == nil {
			t.Time = parsed.UTC()
			return nil
		}
	}
	return fmt.Errorf("dbtime: cannot parse %q as a timestamp", s)
}

// Value implements driver.Valuer.
func (t Time) Value() (driver.Value, error) {
	if t.Time.IsZero() {
		return nil, nil
	}
	return t.Time.UTC().Format(layout), nil
}

// MarshalJSON emits RFC 3339, matching the API convention that timestamps
// are ISO 8601 with an explicit offset.
func (t Time) MarshalJSON() ([]byte, error) {
	return t.Time.UTC().MarshalJSON()
}

// UnmarshalJSON parses RFC 3339.
func (t *Time) UnmarshalJSON(b []byte) error {
	if err := t.Time.UnmarshalJSON(b); err != nil {
		return err
	}
	t.Time = t.Time.UTC()
	return nil
}
