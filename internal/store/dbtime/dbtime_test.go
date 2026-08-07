package dbtime_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/paraview/portico/internal/store/dbtime"
)

func TestNewNormalizesToUTCSeconds(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	input := time.Date(2026, 8, 6, 22, 30, 45, 123456789, loc)

	got := dbtime.New(input)

	if got.Location() != time.UTC {
		t.Errorf("location = %v, want UTC", got.Location())
	}
	if got.Nanosecond() != 0 {
		t.Errorf("nanosecond = %d, want 0 (should be truncated)", got.Nanosecond())
	}
	if !got.Equal(input.Truncate(time.Second)) {
		t.Errorf("time = %v, want the same instant as %v", got, input)
	}
}

func TestValueRoundTripsThroughScan(t *testing.T) {
	original := dbtime.New(time.Date(2026, 8, 6, 14, 25, 33, 0, time.UTC))

	stored, err := original.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}

	var got dbtime.Time
	if err := got.Scan(stored); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !got.Equal(original.Time) {
		t.Errorf("round trip = %v, want %v", got, original)
	}
}

func TestScanAcceptsDriverRepresentations(t *testing.T) {
	want := time.Date(2026, 8, 6, 14, 25, 33, 0, time.UTC)

	tests := []struct {
		name string
		src  any
	}{
		{"rfc3339 string", "2026-08-06T14:25:33Z"},
		{"rfc3339 bytes", []byte("2026-08-06T14:25:33Z")},
		{"time.Time", want},
		{"unix seconds", want.Unix()},
		// The format SQLite's own datetime() emits, in case a row was
		// inserted by hand from the CLI.
		{"sqlite datetime", "2026-08-06 14:25:33"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got dbtime.Time
			if err := got.Scan(tt.src); err != nil {
				t.Fatalf("Scan(%v): %v", tt.src, err)
			}
			if !got.Equal(want) {
				t.Errorf("scanned %v, want %v", got, want)
			}
		})
	}
}

func TestScanNullYieldsZeroTime(t *testing.T) {
	var got dbtime.Time
	if err := got.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if !got.IsZero() {
		t.Errorf("time = %v, want the zero time", got)
	}
}

func TestScanRejectsGarbage(t *testing.T) {
	tests := []any{"not a timestamp", 3.14, struct{}{}}

	for _, src := range tests {
		var got dbtime.Time
		if err := got.Scan(src); err == nil {
			t.Errorf("Scan(%v) succeeded, want an error", src)
		}
	}
}

// The zero time must store as NULL rather than as year 1, so an unset
// timestamp is distinguishable in the database.
func TestZeroTimeStoresAsNull(t *testing.T) {
	var zero dbtime.Time

	got, err := zero.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if got != nil {
		t.Errorf("Value = %v, want nil", got)
	}
}

func TestJSONUsesRFC3339(t *testing.T) {
	original := dbtime.New(time.Date(2026, 8, 6, 14, 25, 33, 0, time.UTC))

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(encoded) != `"2026-08-06T14:25:33Z"` {
		t.Errorf("JSON = %s, want an RFC 3339 string", encoded)
	}

	var decoded dbtime.Time
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !decoded.Equal(original.Time) {
		t.Errorf("decoded = %v, want %v", decoded, original)
	}
}
