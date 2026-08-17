/**
 * Formatting values that belong to the reader rather than to the server.
 *
 * i18n-conventions.md, "Formatting values": timestamps arrive as ISO 8601
 * UTC and are formatted in the browser, because a preformatted date from the
 * server commits every reader to one locale and one time zone.
 *
 * Twelve call sites each wrote `new Date(x).toLocaleString()` and agreed by
 * luck. This is the one place to change when they should stop agreeing —
 * a relative "3 minutes ago", a fixed zone for an operator comparing against
 * server logs — and the one place a test can point at.
 */
export function formatInstant(
  iso: string,
  precision: "instant" | "date" = "instant",
): string {
  const at = new Date(iso);
  return precision === "date" ? at.toLocaleDateString() : at.toLocaleString();
}

/**
 * Whole days from now until an instant, negative once it has passed.
 *
 * Rounded down on purpose: a deadline nineteen hours away is "today", not
 * "tomorrow", because the number is read as how much time is left rather than
 * as which date it falls on — rounding up would tell somebody with an hour
 * that they have a day.
 *
 * Down rather than towards zero, and the difference is not academic.
 * Math.trunc on a deadline an hour in the past gives -0, and `-0 < 0` is false
 * in JavaScript — so a caller asking "has this passed?" is told no for the
 * whole first day after it did, which is exactly the day it matters. Found by
 * the test beside this, not by reading it.
 *
 * Here rather than in the one page that needs it, for the reason above this
 * file: it is computed from the reader's clock, and the next place that wants
 * it must not compute it slightly differently.
 */
export function daysUntil(iso: string, now: Date = new Date()): number {
  const millis = new Date(iso).getTime() - now.getTime();
  return Math.floor(millis / 86_400_000);
}
