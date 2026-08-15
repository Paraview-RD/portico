import { expect, it } from "vitest";

/**
 * The rules in docs/design-principles.md, held to by reading the source.
 *
 * Every one of these was drifting before it was written down here. Six of
 * the thirteen row-action groups used `secondary` where the document says
 * `ghost`; eight screens each wrote their own enabled/disabled badge, and
 * one of them had already picked a different colour. None of it is the kind
 * of thing a rendering test notices — each screen looks fine on its own, and
 * the defect is only visible by comparison.
 *
 * This is the same shape as the Go tests that pin a document to the thing it
 * describes. It reads the source as text rather than rendering it,
 * deliberately: what is checked is what somebody typed, and a component that
 * renders correctly from the wrong variant is exactly the failure being
 * prevented.
 *
 * The sources arrive through `import.meta.glob` rather than `node:fs` so
 * that this needs no Node type definitions the app does not otherwise
 * depend on.
 */

const screens = Object.entries(
  import.meta.glob<string>("./pages/*.tsx", {
    query: "?raw",
    import: "default",
    eager: true,
  }),
)
  .filter(([path]) => !path.endsWith(".test.tsx"))
  .map(([path, text]) => ({ name: path.replace("./pages/", ""), text }));

it("found the screens it is supposed to be reading", () => {
  // Without this, a glob that stops matching turns every check below into a
  // loop over nothing that passes for the wrong reason.
  expect(screens.length).toBeGreaterThan(8);
});

/** Every `<Td>…</Td>` in a file, with the line it starts on. */
function cells(text: string): { line: number; body: string }[] {
  const found: { line: number; body: string }[] = [];
  for (const match of text.matchAll(/<Td\b[\s\S]*?<\/Td>/g)) {
    found.push({
      line: text.slice(0, match.index).split("\n").length,
      body: match[0],
    });
  }
  return found;
}

it("draws every row of buttons as ghosts, per design-principles.md", () => {
  // "use it for an action that sits in a group — a table's action column, a
  // toolbar, a row of peers". A bordered button among borderless ones is
  // what a reader sees as the important one, and in an action column none
  // of them is.
  const offenders: string[] = [];

  for (const { name, text } of screens) {
    for (const { line, body } of cells(text)) {
      if ((body.match(/<Button/g) ?? []).length < 2) continue;
      const variants = [...body.matchAll(/variant="([\w-]+)"/g)].map(
        (m) => m[1],
      );
      const wrong = variants.filter(
        (v) => v !== "ghost" && v !== "ghost-danger",
      );
      if (wrong.length > 0) {
        offenders.push(`${name}:${line} uses ${wrong.join(", ")}`);
      }
    }
  }

  expect(offenders).toEqual([]);
});

it("keeps a filled button out of the table, since primary is one per view", () => {
  // `danger` is filled, and filled is what `primary` is. One per row is one
  // per row, and the column stops reading as a set of peers.
  const offenders: string[] = [];

  for (const { name, text } of screens) {
    for (const { line, body } of cells(text)) {
      if (/variant="danger"/.test(body)) {
        offenders.push(`${name}:${line} — use ghost-danger inside a row`);
      }
    }
  }

  expect(offenders).toEqual([]);
});

it("asks StatusBadge for enabled-or-disabled rather than restating it", () => {
  // Eight copies of `status === "ACTIVE" ? "success" : "neutral"` is eight
  // chances to disagree, and they had: one screen drew a disabled account
  // in danger. The component is in components/ui.tsx.
  const offenders: string[] = [];

  for (const { name, text } of screens) {
    text.split("\n").forEach((body, index) => {
      if (/"ACTIVE" \? "success" : "neutral"/.test(body)) {
        offenders.push(`${name}:${index + 1}`);
      }
    });
  }

  expect(offenders).toEqual([]);
});

it("gives every screen with a manual chapter a link to it in the header", () => {
  // The panel that explains a screen can be turned off for the whole tenant,
  // so a link that lives only inside it disappears with it. The header entry
  // is the one that is always there; the panel keeps its own because a
  // reader who has just read the explanation is the likeliest to want more.
  const missing: string[] = [];

  for (const { name, text } of screens) {
    const panel = /docsPage="([^"]+)"/.exec(text);
    if (panel === null) continue;
    if (!text.includes("<DocsLink")) {
      missing.push(`${name} explains ${panel[1]} but its header links nowhere`);
    }
  }

  expect(missing).toEqual([]);
});

it("gives every control a name a screen reader can read", () => {
  // A placeholder is not a name — it disappears on the first keystroke, and
  // what gets announced afterwards is nothing. Neither is a label smuggled
  // into the first option of a select. Either the control is inside a
  // `Field`, which renders a real `<label>`, or it carries `aria-label`.
  const unnamed: string[] = [];

  for (const { name, text } of screens) {
    for (const match of text.matchAll(/<(Input|Select|Textarea)\b[^>]*/g)) {
      const tag = match[0];
      if (tag.includes("aria-label")) continue;

      // Inside a Field or a <label>, whichever encloses it: both give the
      // control a real label element. Looked for by scanning backwards to
      // whichever opener comes last before this tag.
      const before = text.slice(0, match.index);
      const enclosing = Math.max(
        before.lastIndexOf("<Field"),
        before.lastIndexOf("<label"),
      );
      const closed = Math.max(
        before.lastIndexOf("</Field>"),
        before.lastIndexOf("</label>"),
      );
      if (enclosing > closed) continue;

      unnamed.push(`${name}:${before.split("\n").length} <${match[1]}>`);
    }
  }

  expect(unnamed).toEqual([]);
});

it("takes radii from tokens, not from Tailwind's own scale", () => {
  // "Every color, spacing, radius, and shadow used in a component comes from
  // a CSS custom property defined in theme.css" — the first rule in the
  // document, and the one most easily broken by typing `rounded` out of
  // habit. Five class lists had, all of them next to a border that did use
  // its token, so the mismatch was invisible.
  const literals: string[] = [];

  for (const { name, text } of screens) {
    text.split("\n").forEach((line, index) => {
      if (!line.includes("className")) return;
      // `rounded` alone, not `rounded-[var(…)]` and not `rounded-full`,
      // which is a shape rather than a size and has no token to take.
      if (/\brounded\b(?!-)/.test(line)) {
        literals.push(`${name}:${index + 1}`);
      }
    });
  }

  expect(literals).toEqual([]);
});

/**
 * Every design token a screen names has to exist.
 *
 * A `var(--font-size-2xl)` that was never defined does not fail: CSS falls
 * back to the inherited value, so the element renders at whatever size its
 * parent had. The landing page shipped with exactly that — a page heading
 * silently smaller than the paragraph beneath it, from one token that had
 * never been in theme.css.
 *
 * That is the failure this catches and a rendering test cannot: the page is
 * valid, the class is applied, and the only symptom is that it looks wrong to
 * somebody who happens to look.
 */
const theme = Object.entries(
  import.meta.glob<string>("./styles/*.css", {
    query: "?raw",
    import: "default",
    eager: true,
  }),
)
  .map(([, text]) => text)
  .join("\n");

const components = Object.entries(
  import.meta.glob<string>("./components/*.tsx", {
    query: "?raw",
    import: "default",
    eager: true,
  }),
)
  .filter(([path]) => !path.endsWith(".test.tsx"))
  .map(([path, text]) => ({ name: path.replace("./components/", ""), text }));

it("found the theme it is supposed to be reading", () => {
  expect(theme).toBeTruthy();
  expect(theme).toContain("--font-size-sm:");
});

it("names only design tokens that exist", () => {
  const defined = new Set(
    Array.from(theme.matchAll(/^\s*(--[a-z0-9-]+)\s*:/gm), (m) => m[1]),
  );
  expect(defined.size).toBeGreaterThan(20);

  const missing: string[] = [];
  for (const { name, text } of [...screens, ...components]) {
    text.split("\n").forEach((line, index) => {
      for (const match of line.matchAll(/var\((--[a-z0-9-]+)\)/g)) {
        if (!defined.has(match[1])) {
          missing.push(`${name}:${index + 1} ${match[1]}`);
        }
      }
    });
  }

  expect(
    missing,
    "these name a design token theme.css does not define. CSS falls back to " +
      "the inherited value rather than failing, so the element renders at " +
      "whatever the parent had and nothing reports it:\n" +
      missing.join("\n"),
  ).toEqual([]);
});
