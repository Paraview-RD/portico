import { expect, test } from "./fixtures";

/**
 * The home screen, and the one thing about it that must not drift.
 *
 * A portal listing applications invites a reader to conclude that these are
 * *their* applications — that somebody assigned them, and that an application
 * missing from a colleague's list is one that colleague was not granted. None
 * of that is true here: this version has two fixed roles and no notion of who
 * may use what, so every authenticated person may sign in to everything
 * registered, and the list is identical for everybody.
 *
 * So the screen says so, and this asserts that it still does. It is the
 * cheapest possible check on the most expensive possible misunderstanding.
 */

const ordinaryUser = "e2e-portal-user";
const ordinaryPassword = "e2e-Portal-Password-1";

async function adminToken(page: {
  evaluate: (fn: () => string | null) => Promise<string | null>;
}): Promise<string> {
  const token = await page.evaluate(() =>
    localStorage.getItem("portico.token"),
  );
  expect(token, "no session token after signing in").toBeTruthy();
  return token as string;
}

test("signing in lands on the home screen", async ({ page, signIn }) => {
  await signIn();
  await expect(page).toHaveURL(/\/$/);
  await expect(
    page.getByRole("heading", { level: 1, name: /Hello/ }),
  ).toBeVisible();
});

test("the applications list does not claim to be personal", async ({
  page,
  signIn,
}) => {
  await signIn();

  // Not a paraphrase: the sentence that prevents the misreading is the thing
  // under test, and a test that accepted any wording would pass on a screen
  // that had quietly dropped it.
  await expect(
    page.getByText(/no per-person assignment/i),
    "the portal no longer says these applications are the tenant's rather " +
      "than the reader's, which invites exactly the wrong conclusion",
  ).toBeVisible();
});

test("an application with a launch address is offered, one without is not", async ({
  page,
  signIn,
}) => {
  await signIn();
  const auth = { Authorization: `Bearer ${await adminToken(page)}` };

  const openable = {
    clientId: "e2e-openable",
    name: "E2E Openable",
    public: true,
    applicationType: "USER_AGENT",
    redirectUris: ["https://openable.example.com/callback"],
    postLogoutRedirectUris: [],
    scopes: ["openid"],
    launchUrl: "https://openable.example.com/",
  };
  const headless = {
    clientId: "e2e-no-launch",
    name: "E2E Without Launch",
    public: true,
    applicationType: "USER_AGENT",
    redirectUris: ["https://nolaunch.example.com/callback"],
    postLogoutRedirectUris: [],
    scopes: ["openid"],
  };

  for (const application of [openable, headless]) {
    const created = await page.request.post(
      "/api/v1/applications/oauth-clients",
      { headers: auth, data: application },
    );
    expect(
      created.ok() || created.status() === 409,
      `registering ${application.clientId} failed: ${await created.text()}`,
    ).toBe(true);
  }

  // Reloaded, not navigated to. Signing in already lands on the home screen,
  // so clicking its own entry is a no-op — the list was fetched before these
  // two applications existed, and the assertion below would be about a stale
  // render. It passed that way when a previous run had left the applications
  // behind, which is the third time this suite has been fooled by its own
  // leftovers.
  await page.reload();

  const link = page.getByRole("link", { name: /E2E Openable/ });
  await expect(link).toBeVisible();
  await expect(link).toHaveAttribute("href", "https://openable.example.com/");
  // Opening in a new tab is only safe with these: without noopener the opened
  // page can reach back through window.opener and navigate this one.
  await expect(link).toHaveAttribute("rel", /noopener/);

  // A registration with no launch address is not a tile. It still signs
  // people in; there is simply nowhere for a portal to send them.
  await expect(
    page.getByText("E2E Without Launch"),
    "an application with no launch address is being offered as something to open",
  ).toHaveCount(0);
});

test("an ordinary user gets the portal, not an administrative screen", async ({
  page,
  signIn,
}) => {
  await signIn();
  const auth = { Authorization: `Bearer ${await adminToken(page)}` };

  const created = await page.request.post("/api/v1/users", {
    headers: auth,
    data: {
      username: ordinaryUser,
      displayName: "Portal Person",
      password: ordinaryPassword,
      role: "USER",
    },
  });
  expect(
    created.ok() || created.status() === 409,
    `creating the ordinary user failed: ${await created.text()}`,
  ).toBe(true);

  await page.evaluate(() => localStorage.clear());
  await signIn(ordinaryUser, ordinaryPassword);

  await expect(
    page.getByRole("heading", { level: 1, name: /Hello/ }),
  ).toBeVisible();

  // And typing an administrative URL lands here too, rather than on a screen
  // that would answer 403 to everything it asked for.
  await page.goto("/users");
  await expect(
    page.getByRole("heading", { level: 1, name: /Hello/ }),
  ).toBeVisible();
});
