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
