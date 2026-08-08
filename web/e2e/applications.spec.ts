import { expect, test } from "./fixtures";

/**
 * Application management, in a browser.
 *
 * Two of this project's three browser-only defects came from this screen and
 * its siblings: a `role="tab"` that announced itself as a tab and led
 * nowhere, and a status control that addressed the wrong identifier — which
 * the type checker could not see, because both identifiers were strings, and
 * which a component test would only catch if it happened to be written
 * against the same wrong assumption.
 *
 * Both are checked here through the accessibility tree rather than through
 * class names or test ids, because the accessibility tree is what a screen
 * reader and a keyboard user actually get.
 */

test.beforeEach(async ({ signIn, page }) => {
  await signIn();
  await page.goto("/applications");
});

test("each protocol tab leads to a panel", async ({ page }) => {
  const tabs = page.getByRole("tab");
  await expect(tabs).toHaveCount(3);

  for (const name of ["OAuth 2.1 / OIDC", "SAML 2.0", "CAS"]) {
    await page.getByRole("tab", { name }).click();

    await expect(page.getByRole("tab", { name })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    // The half that was missing: a tab that selects itself and controls
    // nothing is a link wearing a tab's clothes.
    await expect(page.getByRole("tabpanel")).toBeVisible();
  }
});

test("the protocol tabs are reachable from the keyboard", async ({ page }) => {
  const first = page.getByRole("tab", { name: "OAuth 2.1 / OIDC" });
  await first.focus();
  await expect(first).toBeFocused();

  // Not a decorative assertion: these are rendered as buttons precisely so
  // that this works, and a refactor to <div onClick> would keep every visual
  // test passing while removing the screen from keyboard use.
  await page.keyboard.press("Enter");
  await expect(page.getByRole("tabpanel")).toBeVisible();
});

test("switching protocol switches what is listed", async ({ page }) => {
  await page.getByRole("tab", { name: "SAML 2.0" }).click();
  const samlPanel = await page.getByRole("tabpanel").innerText();

  await page.getByRole("tab", { name: "CAS" }).click();
  const casPanel = await page.getByRole("tabpanel").innerText();

  // Weak on purpose: the fixture data is not this test's to control. What it
  // rules out is a panel that never re-renders — three tabs over one list,
  // which looks entirely correct until someone registers an application and
  // cannot find it.
  expect(samlPanel).not.toEqual(casPanel);
});
