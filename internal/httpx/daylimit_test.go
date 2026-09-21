package httpx

import (
	"testing"
	"time"
)

func TestDayLimiterAllowsUpToLimit(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	l := NewDayLimiter(3, 24*time.Hour)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !l.Allow("phone-1") {
			t.Fatalf("attempt %d: want allowed", i)
		}
	}
	if l.Allow("phone-1") {
		t.Fatal("4th attempt within the window: want refused")
	}
	// A different key has its own allowance.
	if !l.Allow("phone-2") {
		t.Fatal("a different key should not share phone-1's count")
	}
}

func TestDayLimiterResetsAfterTheWindow(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	l := NewDayLimiter(1, 24*time.Hour)
	l.now = func() time.Time { return now }

	if !l.Allow("phone-1") {
		t.Fatal("first attempt: want allowed")
	}
	if l.Allow("phone-1") {
		t.Fatal("second attempt inside the window: want refused")
	}

	// Nine at 09:00 the next morning does not carry the earlier count: a
	// token bucket would have refilled continuously by then, but this is a
	// day window, so 09:00 the next day is a fresh window regardless of
	// how much of the previous day's allowance was used and when.
	now = now.Add(24*time.Hour + time.Minute)
	if !l.Allow("phone-1") {
		t.Fatal("attempt a day later: want allowed, window reset")
	}
}
