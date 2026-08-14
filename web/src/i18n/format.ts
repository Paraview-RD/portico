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
