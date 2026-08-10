import { expect, test } from "./fixtures";

/**
 * That a directory-managed account says so, end to end.
 *
 * This is not a rendering detail. An administrator who edits one of these is
 * editing something the next synchronization will overwrite, and nothing
 * else on the screen distinguishes it from an account somebody created here
 * five minutes ago. The whole chain has to hold for the warning to appear —
 * the provisioning service writing `SCIM` as the source, the list endpoint
 * carrying it, and the row rendering it — and only a browser against the
 * real binary exercises all three.
 *
 * The type checker cannot help: `source` was typed as three values when the
 * server had four, so a SCIM account was a thing TypeScript believed could
 * not arrive.
 */

const credentialName = "e2e directory";
const provisionedUser = "e2e-directory-user";

/** Unwraps the response envelope, failing loudly rather than returning undefined. */
async function data<T>(response: {
  ok: () => boolean;
  status: () => number;
  json: () => Promise<{ code: string; message: string; data: T }>;
  text: () => Promise<string>;
}): Promise<T> {
  if (!response.ok()) {
    throw new Error(`${response.status()}: ${await response.text()}`);
  }
  const body = await response.json();
  if (body.code !== "SUCCESS") {
    throw new Error(`${body.code}: ${body.message}`);
  }
  return body.data;
}

/**
 * Issues a provisioning credential, discarding any left by a previous run.
 *
 * Rather than making the name unique: a run that leaves a uniquely-named
 * credential behind on every attempt is a slow leak, and the name is how an
 * operator recognizes it. Reaching a known state first is also what stops
 * this test passing on data an earlier run created — which is a real
 * failure mode here, because the account it provisions survives too.
 */
async function issueCredential(
  request: { get: Function; post: Function; delete: Function },
  session: string | null,
): Promise<string> {
  const auth = { Authorization: `Bearer ${session}` };

  const existing = await data<{ id: string; name: string }[]>(
    await request.get("/api/v1/scim-credentials", { headers: auth }),
  );
  for (const credential of existing.filter((c) => c.name === credentialName)) {
    const removed = await request.delete(
      `/api/v1/scim-credentials/${credential.id}`,
      { headers: auth },
    );
    expect(removed.ok(), await removed.text()).toBe(true);
  }

  const issued = await data<{ token: string }>(
    await request.post("/api/v1/scim-credentials", {
      headers: auth,
      data: { name: credentialName },
    }),
  );
  return issued.token;
}

test("an account a directory provisioned is marked as theirs, in the list and in the form", async ({
  page,
  signIn,
}) => {
  await signIn();

  // The session the browser is holding, rather than a second sign-in: this
  // test is about what an administrator sees, so it should act as the one
  // that is signed in.
  const session = await page.evaluate(() =>
    localStorage.getItem("portico.token"),
  );
  expect(session, "no session token after signing in").toBeTruthy();

  const token = await issueCredential(page.request, session);

  // Provisioned the way a directory does it, over SCIM with the credential
  // that was just issued — not by calling the console's own create endpoint
  // and setting a field. Going through the real path is what makes this a
  // test of the source being recorded rather than of it being echoed back.
  //
  // The display name deliberately avoids the word the badge uses, so that
  // finding "Directory" in the row means the badge and cannot mean the name.
  const provisioned = await page.request.post("/scim/v2/Users", {
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/scim+json",
    },
    data: {
      schemas: ["urn:ietf:params:scim:schemas:core:2.0:User"],
      userName: provisionedUser,
      externalId: "e2e-ext-directory-user",
      name: { formatted: "Provisioned Person" },
      active: true,
    },
  });
  expect(
    provisioned.ok(),
    `provisioning failed: ${await provisioned.text()}`,
  ).toBe(true);

  await page.goto("/users");

  const provisionedRow = page.getByRole("row", { name: provisionedUser });
  await expect(provisionedRow).toBeVisible();
  await expect(
    provisionedRow.getByText("Directory", { exact: true }),
    "the provisioned account carries no directory mark",
  ).toBeVisible();

  // The other half, and the one that fails if the mark is unconditional: an
  // account created here is not a directory's, and saying it is would be
  // worse than saying nothing.
  await expect(
    page.getByRole("row", { name: "admin" }).getByText("Directory", {
      exact: true,
    }),
  ).toHaveCount(0);

  // The badge warns before the click; the form warns while the edit is being
  // typed, which is the last moment it is still free to abandon.
  await provisionedRow.getByRole("button", { name: "Edit" }).click();
  await expect(page.getByRole("alert")).toContainText(
    "maintained by a directory",
  );
});

/**
 * The two capabilities that existed everywhere except the screen.
 *
 * Both were found the same way: a translation key with no reader. Filtering
 * users by organization had a label, and the list endpoint had taken an
 * organizationId all along; showing which groups an account is in had two
 * labels, a client method, and a whole `GET /users/{id}/groups` endpoint —
 * and no caller. A key nothing renders is usually surplus. Sometimes it is
 * the last trace of something that was never finished, and the difference
 * only shows when you go and look.
 */

/** A signed-in administrator's own token, for the setup these tests need. */
async function adminToken(page: {
  evaluate: (fn: () => string | null) => Promise<string | null>;
}): Promise<string> {
  const token = await page.evaluate(() =>
    localStorage.getItem("portico.token"),
  );
  expect(token, "no session token after signing in").toBeTruthy();
  return token as string;
}

test("picking an organization filters to its whole branch", async ({
  page,
  signIn,
}) => {
  await signIn();
  const auth = { Authorization: `Bearer ${await adminToken(page)}` };

  /** Gets an organization by code, creating it under `parentId` if absent. */
  async function organization(name: string, code: string, parentId: string) {
    const existing = await data<{ id: string; code: string }[]>(
      await page.request.get("/api/v1/organizations", { headers: auth }),
    );
    const found = existing.find((o) => o.code === code);
    if (found) return found.id;

    const created = await data<{ id: string }>(
      await page.request.post("/api/v1/organizations", {
        headers: auth,
        data: { name, code, parentId },
      }),
    );
    return created.id;
  }

  async function member(username: string, organizationId: string) {
    const created = await page.request.post("/api/v1/users", {
      headers: auth,
      data: {
        username,
        displayName: username,
        password: "e2e-Subtree-Member-1",
        role: "USER",
        organizationId,
      },
    });
    expect(
      created.ok() || created.status() === 409,
      `creating ${username} failed: ${await created.text()}`,
    ).toBe(true);
  }

  // A branch two deep and a sibling root. The sibling is what stops a filter
  // that returns everybody from passing, and the child is the whole point:
  // against a flat chart, an exact match and a subtree are indistinguishable.
  const parent = await organization("E2E Subtree Parent", "e2e-subtree-p", "");
  const child = await organization(
    "E2E Subtree Child",
    "e2e-subtree-c",
    parent,
  );
  const other = await organization("E2E Subtree Other", "e2e-subtree-o", "");

  await member("e2e-subtree-in-parent", parent);
  await member("e2e-subtree-in-child", child);
  await member("e2e-subtree-in-other", other);

  await page.goto("/users");
  const chart = page.getByRole("navigation", { name: "Organization" });
  await expect(
    chart.getByRole("button", { name: "E2E Subtree Parent" }),
  ).toBeVisible();

  await chart.getByRole("button", { name: "E2E Subtree Parent" }).click();

  await expect(
    page.getByRole("row", { name: "e2e-subtree-in-parent" }),
    "the chosen organization's own member is missing",
  ).toBeVisible();
  await expect(
    page.getByRole("row", { name: "e2e-subtree-in-child" }),
    "somebody in a department below the chosen one was left out, which is " +
      "the whole reason this filter is a chart rather than a list",
  ).toBeVisible();
  await expect(
    page.getByRole("row", { name: "e2e-subtree-in-other" }),
    "a member of another branch survived the filter",
  ).toHaveCount(0);
  // And the administrator, who belongs to no organization at all.
  await expect(page.getByRole("row", { name: "admin" })).toHaveCount(0);

  // The other question the control has to be able to ask, and the one a
  // dropdown could not: who is in no organization at all.
  await chart.getByRole("button", { name: "Not in one" }).click();

  await expect(
    page.getByRole("row", { name: "admin" }),
    "the unfiled administrator is missing from the unfiled",
  ).toBeVisible();
  await expect(
    page.getByRole("row", { name: "e2e-subtree-in-child" }),
    "somebody with an organization appeared among those without one",
  ).toHaveCount(0);
});

test("an account says which groups it is in", async ({ page, signIn }) => {
  await signIn();
  const auth = { Authorization: `Bearer ${await adminToken(page)}` };

  const username = "e2e-grouped-member";
  const created = await page.request.post("/api/v1/users", {
    headers: auth,
    data: {
      username,
      displayName: "Grouped Member",
      password: "e2e-Grouped-Member-1",
      role: "USER",
    },
  });
  expect(
    created.ok() || created.status() === 409,
    `creating ${username} failed: ${await created.text()}`,
  ).toBe(true);

  const everyone = await data<{ id: string; username: string }[]>(
    await page.request.get("/api/v1/users?pageSize=200", { headers: auth }),
  ).then(
    (page_) =>
      (page_ as unknown as { items: { id: string; username: string }[] }).items,
  );
  const member = everyone.find((u) => u.username === username);
  expect(member, "the account just created is not in the list").toBeTruthy();

  const groupName = "E2E Membership Group";
  const groups = await data<{ id: string; displayName: string }[]>(
    await page.request.get("/api/v1/groups", { headers: auth }),
  );
  const group =
    groups.find((g) => g.displayName === groupName) ??
    (await data<{ id: string }>(
      await page.request.post("/api/v1/groups", {
        headers: auth,
        data: { displayName: groupName, description: "" },
      }),
    ));

  const added = await page.request.post(`/api/v1/groups/${group.id}/members`, {
    headers: auth,
    data: { userIds: [member!.id] },
  });
  expect(added.ok(), `adding the member failed: ${await added.text()}`).toBe(
    true,
  );

  await page.goto("/users");
  await page
    .getByRole("row", { name: username })
    .getByRole("button", { name: "Edit" })
    .click();

  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("Groups");
  await expect(
    dialog.getByText(groupName),
    "the account's group membership is not shown",
  ).toBeVisible();

  // The other half: an account in no group says so rather than showing an
  // empty space that reads as "still loading".
  await page.getByRole("button", { name: "Cancel" }).click();
  await page
    .getByRole("row", { name: "admin" })
    .getByRole("button", { name: "Edit" })
    .click();
  await expect(page.getByRole("dialog").getByText("None")).toBeVisible();
});
