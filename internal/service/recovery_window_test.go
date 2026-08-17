package service

// Where the daily password-recovery count starts.
//
// One function, three cases, no database — which is the reason it is a
// function at all. The rule reads as "the later of a day ago and the last
// clear", and it is exactly the kind of comparison that is written backwards
// once and then only noticed when somebody's allowance never comes back.

import (
	"testing"
	"time"
)

func TestTheRecoveryWindowStartsADayAgoWhenNobodyHasCleared(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

	if got := recoveryWindowStart(now, nil); !got.Equal(now.Add(-24 * time.Hour)) {
		t.Errorf("window starts at %s, want a day before %s", got, now)
	}
}

// A clear inside the day moves the start forward, which is what makes the
// button do anything. Counting from a fixed day-ago would return the same
// number the instant after an administrator restored the allowance.
func TestAClearInsideTheDayMovesTheWindowForward(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	cleared := now.Add(-2 * time.Hour)

	if got := recoveryWindowStart(now, &cleared); !got.Equal(cleared) {
		t.Errorf("window starts at %s, want the clear at %s", got, cleared)
	}
}

// A clear from last week does not.
//
// This is the half that keeps the cap a cap. If the window began at whatever
// the last clear was, an account cleared once would be counted from that
// moment forever — one administrator being helpful in March would exempt an
// account from the limit for the rest of the year, and nothing would say so.
func TestAnOldClearDoesNotWidenTheWindow(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	cleared := now.Add(-7 * 24 * time.Hour)

	if got := recoveryWindowStart(now, &cleared); !got.Equal(now.Add(-24 * time.Hour)) {
		t.Errorf("window starts at %s, want a day before %s — an old clear must "+
			"not exempt an account permanently", got, now)
	}
}
