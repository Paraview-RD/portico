import { expect, test } from "./fixtures";

/**
 * The two screens somebody reaches before they have an account anywhere.
 *
 * These are here because of what they are: addresses opened from an email by
 * a person who cannot sign in. Everything else in this suite is visited by
 * somebody who already has, and the guard that sends a signed-out visitor to
 * the sign-in screen is exactly right for those — and exactly wrong for
 * these. A missing entry in that list is invisible from the API, which
 * answers correctly either way, and invisible from the component tests, which
 * do not run the guard.
 *
 * The failure it produced was total and quiet: the confirmation link mailed to
 * every trial applicant landed on the sign-in screen, so the tenant was never
 * created and the visitor was asked to sign in to an account that does not
 * exist yet.
 *
 * Both run against a server with trials switched off, which is the harder
 * case and the one every ordinary deployment is in. The screens are meant to
 * be reachable regardless — the form refuses on submit, from the server,
 * rather than on mount from a screen that flickers into an error.
 */

test("the trial form is reachable without signing in", async ({ page }) => {
  await page.goto("/trial");

  await expect(page).toHaveURL(/\/trial$/);
  await expect(
    page.getByRole("heading", { name: "Try Portico" }),
  ).toBeVisible();
  // The industry picker is the part that has to be populated from the server.
  // Even with trials off, the status endpoint answers and the list has at
  // least the generic world in it.
  await expect(page.getByLabel("Seeded data")).toBeVisible();
});

test("a confirmation link with no token explains itself rather than asking for a password", async ({
  page,
}) => {
  // No token, because a real one cannot be had here: making one would need
  // trials switched on and a mailbox to read. What this pins is the routing —
  // that the address reaches its own screen — and the screen's own answer to
  // being opened wrongly.
  await page.goto("/trial/confirm");

  await expect(page).toHaveURL(/\/trial\/confirm$/);
  await expect(page.getByText(/link/i).first()).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Sign in" }),
  ).not.toBeVisible();
});
