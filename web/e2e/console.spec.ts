import { expect, test } from "./fixtures";

/**
 * Every screen loads, renders, and does so without the browser objecting.
 *
 * The assertions here look weak — a heading is visible, the page is not
 * blank — and the value is not in them. It is in the fixture, which fails
 * any test that produced a Content-Security-Policy violation, an uncaught
 * exception, or a console error. Those are the failures that do not announce
 * themselves: a blocked script leaves a page that renders and does nothing.
 *
 * So this file is a sweep. Its job is to visit everything, so that the
 * fixture gets to look at everything.
 */

const publicScreens = [
  { path: "/login", heading: "Sign in" },
  { path: "/forgot-password", heading: /password/i },
];

const authenticatedScreens = [
  { path: "/users", heading: "Users" },
  { path: "/organizations", heading: "Organizations" },
  { path: "/groups", heading: "Groups" },
  { path: "/applications", heading: "Applications" },
  { path: "/audit-logs", heading: "Audit logs" },
  { path: "/settings", heading: "Settings" },
  { path: "/profile", heading: /profile/i },
];

for (const screen of publicScreens) {
  test(`${screen.path} loads without the browser objecting`, async ({
    page,
  }) => {
    await page.goto(screen.path);
    await expect(
      page.getByRole("heading", { name: screen.heading }).first(),
    ).toBeVisible();
  });
}

test.describe("signed in", () => {
  for (const screen of authenticatedScreens) {
    test(`${screen.path} loads without the browser objecting`, async ({
      page,
      signIn,
    }) => {
      await signIn();
      await page.goto(screen.path);
      await expect(
        page.getByRole("heading", { name: screen.heading }).first(),
      ).toBeVisible();
      // A screen that rendered its frame and nothing else is the failure
      // mode a heading check misses.
      await expect(page.getByRole("main")).not.toBeEmpty();
    });
  }
});

test("the embedded frontend is the one being served", async ({ page }) => {
  // Guards against the whole suite silently running against `npm run dev`,
  // which would not send the Content-Security-Policy the fixture is here to
  // police. The dev server does not serve /api/v1/health.
  const response = await page.request.get("/api/v1/health");
  expect(response.ok()).toBe(true);

  const page_ = await page.goto("/login");
  const csp = page_?.headers()["content-security-policy"];
  expect(
    csp,
    "no Content-Security-Policy on the document; either the security " +
      "middleware regressed or these tests are pointed at a dev server",
  ).toBeTruthy();
  expect(csp).toContain("script-src 'self'");
});
