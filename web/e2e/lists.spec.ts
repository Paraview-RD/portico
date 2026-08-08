import { expect, test } from "./fixtures";

/**
 * Every list screen says it is loading, rather than saying nothing.
 *
 * Three of these had no loading state at all. Their rows started as `null`,
 * so `rows?.length === 0` was false and `rows?.map` was undefined, and the
 * body rendered as nothing — pixel-identical to "there is nothing here". A
 * reader cannot tell a slow query from an empty tenant, and the two call for
 * opposite reactions: wait, or go and create something.
 *
 * It is invisible in development, where the answer arrives in ten
 * milliseconds, and it is exactly what somebody on a slow link sees.
 *
 * Two things here are not incidental, and the first version of this file got
 * both wrong and passed anyway:
 *
 *  - The assertion is scoped to the table. The application shell renders its
 *    own "Loading…" while it fetches the session, so an unscoped check
 *    matches that instead and passes on every screen whether or not the
 *    screen has a loading state. Both mutations survived until this was
 *    scoped.
 *  - The screen is reached by clicking, not by page.goto. A full navigation
 *    remounts the shell, which puts its loading state back on the page and
 *    reintroduces exactly the confusion above.
 */

const screens = [
  { label: "Users", api: "**/api/v1/users?*" },
  { label: "Organizations", api: "**/api/v1/organizations*" },
  { label: "Groups", api: "**/api/v1/groups" },
  { label: "Applications", api: "**/api/v1/applications/oauth-clients" },
  { label: "Provisioning", api: "**/api/v1/scim-credentials" },
  { label: "Webhooks", api: "**/api/v1/webhooks" },
  { label: "Audit logs", api: "**/api/v1/audit-logs*" },
];

for (const screen of screens) {
  test(`${screen.label} says it is loading while it is`, async ({
    page,
    signIn,
  }) => {
    await signIn();

    // Signing in lands on Users, so for that screen clicking its own entry
    // is a no-op — no request, no loading state, and a failure that looks
    // like a missing feature. Start somewhere without a table instead.
    await page
      .getByRole("navigation")
      .getByRole("button", { name: "Settings", exact: true })
      .click();
    await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();

    let release: (() => void) | null = null;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });

    await page.route(screen.api, async (route) => {
      await held;
      await route.continue();
    });

    await page
      .getByRole("navigation")
      .getByRole("button", { name: screen.label, exact: true })
      .click();

    const table = page.getByRole("table").first();
    await expect(
      table.getByText("Loading…"),
      "the table renders as empty while it is still fetching, which reads as " +
        "an empty tenant rather than as a slow request",
    ).toBeVisible({ timeout: 10_000 });

    release!();

    // And it goes away again, so nobody is left watching a message that
    // outlived its request.
    await expect(table.getByText("Loading…")).toHaveCount(0, {
      timeout: 10_000,
    });
  });
}
