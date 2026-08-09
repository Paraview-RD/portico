import { expect, test } from "./fixtures";

/**
 * That every entry in the sidebar goes where it says.
 *
 * The rest of the suite visits screens by URL, which is the wrong instrument
 * for this: a menu entry pointing at a route the router does not know falls
 * through to the default screen, and a test that navigated by URL would
 * never see it. What the reader gets is a menu item that "does nothing", or
 * worse, one that silently lands them on the user list.
 *
 * So this clicks. It is the only test in the suite that drives the menu the
 * way a person does.
 */

// Home has no group heading: it is not a category, and a label above a
// single item at the top of every sidebar is noise. The group assertions
// below skip the ones with no heading rather than inventing one.
const entries = [
  { group: "", label: "Home", path: "/" },
  { group: "Directory", label: "Users", path: "/users" },
  { group: "Directory", label: "Organizations", path: "/organizations" },
  { group: "Directory", label: "Groups", path: "/groups" },
  { group: "Integration", label: "Applications", path: "/applications" },
  { group: "Integration", label: "Provisioning", path: "/provisioning" },
  { group: "Integration", label: "Webhooks", path: "/webhooks" },
  { group: "Audit", label: "Audit logs", path: "/audit-logs" },
  { group: "System", label: "Settings", path: "/settings" },
  { group: "Account", label: "My profile", path: "/profile" },
];

const ordinaryUser = "e2e-ordinary-user";
const ordinaryPassword = "e2e-Ordinary-Password-1";

test("every menu entry leads to its own screen", async ({ page, signIn }) => {
  await signIn();

  const nav = page.getByRole("navigation");

  for (const entry of entries) {
    await nav.getByRole("button", { name: entry.label, exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`${entry.path}$`));

    // Landing somewhere is not enough — falling through to the default
    // screen also lands somewhere. The entry has to become the current one.
    await expect(
      nav.getByRole("button", { name: entry.label, exact: true }),
      `${entry.label} did not become the current page`,
    ).toHaveAttribute("aria-current", "page");
  }
});

test("each screen sits in the group it belongs to", async ({
  page,
  signIn,
}) => {
  await signIn();

  const text = (await page.getByRole("navigation").textContent()) ?? "";

  // The positions below mean what they say only if each name occurs once.
  // "Audit" is a prefix of "Audit logs", and it works today only because the
  // heading happens to come first — which is luck, not design. Checked here
  // so that adding an entry whose label contains a group's name fails
  // outright rather than passing for the wrong reason.
  const occurrences = (needle: string) => text.split(needle).length - 1;
  const named = entries.filter((entry) => entry.group !== "");
  for (const name of [
    ...new Set([...named.map((e) => e.group), ...named.map((e) => e.label)]),
  ]) {
    // A group name inside a longer label is fine — "Audit" inside "Audit
    // logs" — as long as the heading itself is the earlier occurrence, which
    // the ordering assertions then confirm. Two independent occurrences of
    // the same name are not.
    const insideLabels = named.filter(
      (e) => e.label !== name && e.label.includes(name),
    ).length;
    expect(
      occurrences(name) - insideLabels,
      `"${name}" appears more than once in the menu; the position checks ` +
        `below would be comparing the wrong occurrence`,
    ).toBe(1);
  }

  // Checked by position rather than by presence. A screen filed under the
  // wrong heading is still present, and every per-item assertion passes
  // while the menu says something untrue — which is the defect this whole
  // change was made to remove.
  for (const entry of entries) {
    if (entry.group === "") continue;
    const groupAt = text.indexOf(entry.group);
    const itemAt = text.indexOf(entry.label);
    expect(groupAt, `no "${entry.group}" heading in the menu`).toBeGreaterThan(
      -1,
    );
    expect(itemAt, `no "${entry.label}" entry in the menu`).toBeGreaterThan(-1);
    expect(
      itemAt,
      `"${entry.label}" appears before its own "${entry.group}" heading`,
    ).toBeGreaterThan(groupAt);

    // And before the next heading, or "belongs to this group" would mean
    // no more than "somewhere below it".
    const laterGroups = entries
      .map((e) => e.group)
      .filter((g) => g !== "" && text.indexOf(g) > groupAt)
      .map((g) => text.indexOf(g));
    if (laterGroups.length > 0) {
      expect(
        itemAt,
        `"${entry.label}" is under a later heading than "${entry.group}"`,
      ).toBeLessThan(Math.min(...laterGroups));
    }
  }
});

test("an ordinary user is offered nothing they cannot use", async ({
  page,
  signIn,
}) => {
  await signIn();

  // Created through the API rather than the form: this test is about the
  // menu, and driving the create dialog would make it fail for reasons that
  // have nothing to do with navigation.
  const session = await page.evaluate(() =>
    localStorage.getItem("portico.token"),
  );
  const created = await page.request.post("/api/v1/users", {
    headers: { Authorization: `Bearer ${session}` },
    data: {
      username: ordinaryUser,
      displayName: "Ordinary Person",
      password: ordinaryPassword,
      role: "USER",
    },
  });
  // Already there from an earlier run is fine; anything else is not.
  expect(
    created.ok() || created.status() === 409,
    `creating the ordinary user failed: ${await created.text()}`,
  ).toBe(true);

  // Dropped rather than signed out through the account menu. Signing out is
  // setup here, not the subject, and driving a dropdown to reach it would
  // make this test fail for a reason that has nothing to do with the menu
  // it is checking.
  await page.evaluate(() => localStorage.clear());
  await signIn(ordinaryUser, ordinaryPassword);

  const nav = page.getByRole("navigation");
  await expect(nav.getByRole("button", { name: "My profile" })).toBeVisible();

  // Hiding these is presentation only — the server enforces the same rule —
  // but a menu offering screens that answer 403 is its own kind of broken,
  // and the administrative groups are now four rather than one, so there
  // are four chances to leak one.
  for (const label of [
    "Users",
    "Organizations",
    "Groups",
    "Applications",
    "Provisioning",
    "Webhooks",
    "Audit logs",
    "Settings",
  ]) {
    await expect(
      nav.getByRole("button", { name: label, exact: true }),
      `${label} is offered to a user who cannot open it`,
    ).toHaveCount(0);
  }

  // And the group headings go with them, rather than standing empty.
  for (const heading of ["Directory", "Integration", "Audit", "System"]) {
    await expect(nav).not.toContainText(heading);
  }
});
