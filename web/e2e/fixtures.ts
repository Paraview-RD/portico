import { test as base, expect, type Page } from "@playwright/test";

import { adminPassword } from "../playwright.config";

/**
 * Shared fixtures.
 *
 * The important one is not the sign-in helper — it is that every test in this
 * suite fails if the browser reported a Content-Security-Policy violation or
 * an uncaught error, whether or not the test thought to look. A CSP violation
 * does not fail an assertion on its own: the blocked script simply does not
 * run, and what the test sees is a page that renders but does nothing, which
 * reads as a missing `await`. That is precisely how the SAML POST-binding bug
 * survived eleven passing Go tests.
 */

declare global {
  interface Window {
    __cspViolations?: string[];
  }
}

type Fixtures = {
  page: Page;
  signIn: (username?: string, password?: string) => Promise<void>;
};

export const test = base.extend<Fixtures>({
  page: async ({ page }, use, testInfo) => {
    // Collected in the page so that violations on documents the server
    // renders itself — the SAML POST binding form, which is not part of the
    // SPA — are caught too.
    await page.addInitScript(() => {
      window.__cspViolations = [];
      document.addEventListener("securitypolicyviolation", (event) => {
        window.__cspViolations?.push(
          `${event.violatedDirective} blocked ${event.blockedURI || "inline"}`,
        );
      });
    });

    const consoleErrors: string[] = [];
    const pageErrors: string[] = [];

    page.on("console", (message) => {
      if (message.type() !== "error") return;
      const text = message.text();
      // A 401 from /users/me before signing in is how the app discovers it
      // has no session. It is expected, and failing on it would make every
      // test start by asserting the absence of normal behaviour.
      if (text.includes("401")) return;
      consoleErrors.push(text);
    });
    page.on("pageerror", (error) => pageErrors.push(error.message));

    await use(page);

    // Only on a passing test: a failing one already has its own diagnosis,
    // and adding a second failure on top buries it.
    if (testInfo.status !== testInfo.expectedStatus) return;

    const violations = await page
      .evaluate(() => window.__cspViolations ?? [])
      .catch(() => [] as string[]);

    expect(
      violations,
      "Content-Security-Policy violations. The blocked resource silently did " +
        "not run; the page may still look correct.",
    ).toEqual([]);
    expect(pageErrors, "uncaught exceptions in the page").toEqual([]);
    expect(consoleErrors, "errors logged to the browser console").toEqual([]);
  },

  signIn: async ({ page }, use) => {
    await use(async (username = "admin", password = adminPassword) => {
      await page.goto("/login");
      // Not `exact`: the label carries a required marker, and matching the
      // whole rendered string would tie the suite to that decoration. A
      // password input has no ARIA role, so getByRole is not available here
      // the way it is for the other controls.
      await page.getByLabel("Username, email, or phone").fill(username);
      await page.getByLabel("Password").fill(password);
      await page.getByRole("button", { name: "Sign in" }).click();
      // Signing in is complete when the console is reachable, not when the
      // button was clicked. The navigation landmark only exists once
      // authenticated, which makes it the right thing to wait for.
      await expect(page.getByRole("navigation")).toBeVisible({
        timeout: 15_000,
      });
    });
  },
});

export { expect };
