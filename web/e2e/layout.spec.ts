import { expect, test } from "./fixtures";

/**
 * Every administrative screen is built the same way.
 *
 * Not a screenshot baseline. A baseline fails on any change and says only
 * "different", which is why nobody keeps one for long. These are the two
 * properties that were actually violated, stated so they can be checked:
 *
 *  1. One content column, the same width on every screen. Each page used to
 *     size itself against the whole viewport, so a table ran to the far edge
 *     of a wide display while a form stopped at 448px.
 *  2. The primary content sits in the same chrome. Tables have carried a
 *     border and a background since they were written; the settings form did
 *     not, so identical fields sat on the page background on one screen and
 *     in a card on another — the two most similar screens looking least
 *     alike.
 *
 * Both are about the frame rather than the pixels, which is the part that
 * has to agree for a console to feel like one product.
 */

const screens = [
  { label: "Home", heading: /Hello/ },
  { label: "Users", heading: "Users" },
  { label: "Organizations", heading: "Organizations" },
  { label: "Groups", heading: "Groups" },
  { label: "Applications", heading: "Applications" },
  { label: "Directory integration", heading: /directory integration/i },
  { label: "Webhooks", heading: /event subscriptions/i },
  { label: "Audit logs", heading: "Audit logs" },
  { label: "Settings", heading: "Settings" },
  { label: "My profile", heading: /profile/i },
];

/** The box of the column every screen's content is laid out in. */
async function contentColumn(page: import("@playwright/test").Page) {
  const box = await page
    .getByRole("main")
    .locator("> div")
    .first()
    .boundingBox();
  expect(box, "no content column inside main").not.toBeNull();
  return box!;
}

test("every screen is laid out in the same column", async ({
  page,
  signIn,
}) => {
  await signIn();
  // Wide enough that an unbounded page would visibly differ from a bounded
  // one. At the default viewport both fit, and the check would pass on a
  // layout that has no column at all.
  await page.setViewportSize({ width: 1900, height: 900 });

  const boxes: Record<string, { x: number; width: number }> = {};
  for (const screen of screens) {
    await page
      .getByRole("navigation")
      .getByRole("button", { name: screen.label, exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: screen.heading, level: 1 }).first(),
    ).toBeVisible();

    const box = await contentColumn(page);
    boxes[screen.label] = {
      x: Math.round(box.x),
      width: Math.round(box.width),
    };
  }

  const widths = new Set(Object.values(boxes).map((b) => b.width));
  const lefts = new Set(Object.values(boxes).map((b) => b.x));
  expect(
    [...widths],
    `screens disagree about how wide the content column is: ${JSON.stringify(boxes)}`,
  ).toHaveLength(1);
  expect(
    [...lefts],
    `screens disagree about where the content column starts: ${JSON.stringify(boxes)}`,
  ).toHaveLength(1);

  // And it is bounded at the width the token declares. "Less than the
  // viewport" is not enough: every screen stretching equally to the edge of
  // a very wide display also satisfies that, and satisfied it here until a
  // mutation removing the cap survived.
  const declared = await page.evaluate(() =>
    parseFloat(
      getComputedStyle(document.documentElement).getPropertyValue(
        "--content-width",
      ),
    ),
  );
  expect(declared, "--content-width is not declared").toBeGreaterThan(0);
  expect(
    [...widths][0],
    `the content column is ${[...widths][0]}px on a ${1900}px display but ` +
      `--content-width says ${declared}px, so nothing is capping it`,
  ).toBe(declared);
});

test("every row of content spans the same column", async ({ page, signIn }) => {
  await signIn();
  await page.setViewportSize({ width: 1900, height: 900 });

  const ragged: string[] = [];
  for (const screen of screens) {
    await page
      .getByRole("navigation")
      .getByRole("button", { name: screen.label, exact: true })
      .click();
    // The home screen is a column of cards with no table and no form, so
    // wait for a card instead. Waiting for something that never appears
    // would fail the test for a reason that has nothing to do with width.
    await expect(
      page.getByRole("main").locator("table, form, section").first(),
    ).toBeVisible();

    // And then wait for the screen to stop moving, which is a different
    // thing from waiting for it to appear.
    //
    // This measures a settled layout, and a screen mid-fetch is not one. The
    // profile screen found it: while the device list is loading, the card
    // holding it is a single line — too short to reach the card below it on
    // the left, so the left column stops chaining into the same row and
    // reports 346–826 against the full-width section beneath it. The layout
    // is fine; the measurement happened in the moment before the list
    // arrived. One run in fifteen, which is often enough to erode trust in
    // the suite and rare enough to be dismissed as noise.
    //
    // Waiting on the placeholder rather than on the network. `networkidle`
    // was tried first and made it worse — five in thirty — because there is
    // no navigation on a client-side route change, so it has nothing to wait
    // for and returns at once. Every screen here says "Loading…" while it
    // fetches, so the absence of that is the screen saying it is done.
    await expect(page.getByRole("main").getByText("Loading…")).toHaveCount(0);

    // The property is about rows, not about blocks.
    //
    // The first version of this test required every surface on a screen to
    // be the same width, which was right about the bug and wrong about the
    // rule. The bug was the profile screen: three cards at 30rem followed by
    // a fourth running to the far edge, which reads as an accident. But two
    // cards deliberately placed side by side are also two different widths,
    // and so is a full-width block above a pair of half-width ones — both
    // are decisions, and a check that forbade them would forbid every layout
    // except a single stack.
    //
    // What separates the two is the row. Things side by side belong to one
    // row and may divide it however they like; what must agree is where each
    // row starts and where it ends. A lone wide card under three narrow ones
    // is two rows disagreeing about the column, which is exactly the thing
    // that looked broken.
    const rows = await page.evaluate(() => {
      // The surfaces content sits on: a card is a section, a table is
      // wrapped in the element carrying its border. Deliberately not "any
      // bordered element" — a search box has a border too, and counting one
      // reported the organizations screen as ragged because its filter
      // input is 288px wide.
      const surfaces = [
        ...document.querySelectorAll<HTMLElement>("main section"),
        ...[...document.querySelectorAll("main table")].map(
          (table) => table.parentElement as HTMLElement,
        ),
      ].filter((element) => element && element.offsetHeight > 0);

      // Only the outermost ones. A card may contain a table, and measuring
      // both would compare a surface with something inside it.
      const outermost = surfaces.filter(
        (element) =>
          !surfaces.some(
            (other) => other.contains(element) && other !== element,
          ),
      );

      const boxes = outermost
        .map((element) => element.getBoundingClientRect())
        .sort((a, b) => a.top - b.top);

      // Group into rows: a surface joins the row above it if their vertical
      // ranges overlap, which is what "side by side" means on screen.
      const grouped: { left: number; right: number }[] = [];
      let current: { top: number; bottom: number } | null = null;
      for (const box of boxes) {
        if (current && box.top < current.bottom) {
          const row = grouped[grouped.length - 1];
          row.left = Math.min(row.left, box.left);
          row.right = Math.max(row.right, box.right);
          current.bottom = Math.max(current.bottom, box.bottom);
        } else {
          grouped.push({ left: box.left, right: box.right });
          current = { top: box.top, bottom: box.bottom };
        }
      }
      return grouped.map((row) => ({
        left: Math.round(row.left),
        right: Math.round(row.right),
      }));
    });

    // A screen with nothing measurable would satisfy the check below by
    // having no rows to disagree, so say so rather than pass quietly.
    expect(
      rows.length,
      `${screen.label} has no measurable content surface`,
    ).toBeGreaterThan(0);

    const spans = new Set(rows.map((row) => `${row.left}–${row.right}`));
    if (spans.size > 1) {
      ragged.push(`${screen.label}: ${[...spans].join(" / ")}`);
    }
  }

  expect(
    ragged,
    "on these screens one row of content stops short of where another ends, " +
      "which reads as a mistake rather than as a layout",
  ).toEqual([]);
});

test("every screen puts its content in the same chrome", async ({
  page,
  signIn,
}) => {
  await signIn();

  const bare: string[] = [];
  for (const screen of screens) {
    await page
      .getByRole("navigation")
      .getByRole("button", { name: screen.label, exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: screen.heading, level: 1 }).first(),
    ).toBeVisible();

    // Wait for the content itself rather than for the heading: a screen
    // still fetching shows its header over a loading row, and asking about
    // the chrome then measures the wrong thing.
    //
    // The same wait as the test above, and here it does something slightly
    // different: a list screen whose table has not arrived yet has neither a
    // table nor a form, so the skip below would pass over it in silence.
    // That is worse than a failure — the screen reports as checked and was
    // not.
    await expect(page.getByRole("main").getByText("Loading…")).toHaveCount(0);

    // The home screen has neither a table nor a form — it is a column of
    // cards — so there is nothing here for this property to be about. It is
    // covered by the two tests above.
    const hasContent =
      (await page.getByRole("main").locator("table, form").count()) > 0;
    if (!hasContent) continue;

    const framed = await page.evaluate(() => {
      // The primary content is the first table on a list screen and the
      // first field on a settings-like one. Either way it must sit inside a
      // bordered surface — that is the whole property: the reader sees the
      // same frame around the same kind of thing on every screen.
      //
      // A field rather than the form around it, because the property is
      // about what the reader sees around the controls. A screen may lay
      // several bordered groups out inside one form — the settings screen
      // does, in two columns — and asking whether the form itself has a
      // border would call that bare while every field on it sits in a card.
      const content =
        document.querySelector("main table") ??
        document.querySelector("main form :is(input, select, textarea)") ??
        document.querySelector("main form");
      if (!content) return false;

      // From the parent, never from the content itself. An <input> has a
      // border of its own, so starting at one would report every form as
      // framed — including a form sitting bare on the page background, which
      // is the single thing this test exists to catch. (It did, for one
      // revision of this file, until removing every card from the settings
      // screen failed to fail it.)
      for (
        let node: Element | null = content.parentElement;
        node && node.tagName !== "MAIN";
        node = node.parentElement
      ) {
        if (parseFloat(getComputedStyle(node).borderTopWidth) > 0) return true;
      }
      return false;
    });

    if (!framed) bare.push(screen.label);
  }

  expect(
    bare,
    "these screens put their content straight onto the page background " +
      "while the others put it in a bordered surface",
  ).toEqual([]);
});
