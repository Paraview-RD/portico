import { describe, expect, it } from "vitest";

import { daysUntil } from "./format";

// The rounding rule, which is the part of this somebody would "fix".
//
// Days left is read as how much time remains, not as which date it falls on.
// Rounding up would tell a person with one hour that they have a day, and
// nineteen hours away is today rather than tomorrow — so it rounds down.
//
// The second case below is why it is Math.floor and not Math.trunc, and it was
// found here rather than in review: trunc returns -0 for a deadline an hour
// past, `-0 < 0` is false, and the tenant console would have shown "in -0
// days" instead of "expired" for the whole first day after one lapsed.
describe("daysUntil", () => {
  const now = new Date("2026-08-17T12:00:00Z");

  it("counts whole days and rounds down", () => {
    expect(daysUntil("2026-08-31T12:00:00Z", now)).toBe(14);
    // Nineteen hours: today, not tomorrow.
    expect(daysUntil("2026-08-18T07:00:00Z", now)).toBe(0);
    // A minute short of two days is one day.
    expect(daysUntil("2026-08-19T11:59:00Z", now)).toBe(1);
  });

  it("goes negative once the date has passed", () => {
    expect(daysUntil("2026-08-16T12:00:00Z", now)).toBe(-1);
    // An hour ago. Math.trunc gives -0 here, and `-0 < 0` is false — so the
    // page would have called a tenant that expired this morning "in -0 days"
    // for its whole first day overdue. Rounding down is what makes the sign
    // usable.
    const anHourAgo = daysUntil("2026-08-17T11:00:00Z", now);
    expect(anHourAgo).toBe(-1);
    expect(anHourAgo < 0).toBe(true);
  });
});
