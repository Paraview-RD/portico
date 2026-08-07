# Design Principles

Rules for how the frontend is styled, not what it should look like.
Portico's actual color palette, type choices, and spacing values live in
[`web/src/styles/theme.css`](../web/src/styles/theme.css) — this document
never repeats a concrete value, so it can't drift out of sync with the
code. If you're deciding what a color should *be*, edit that file. If
you're deciding how colors should be *organized*, this is the file to
read (and update, if the organization itself needs to change).

## Tokens, not literals

- Every color, spacing, radius, and shadow used in a component comes from
  a CSS custom property defined in `theme.css`. No component may hardcode
  a hex value, an `rgb()`, or a raw pixel size for these — that's what
  makes a future re-theme (including dark mode) a one-file change instead
  of a grep-and-replace across the codebase.
- Tokens are named by **role**, not by appearance: `--color-danger`, not
  `--color-red`. A role can be re-pointed to a different hue without the
  name becoming a lie.

## Color roles

A minimal palette covers most UI needs without the maintenance cost of a
large, rarely-differentiated one:

| Role | Meaning |
|---|---|
| `primary` | The main interactive/brand color — primary buttons, active nav state |
| `bg` / `bg-soft` | Page background / a slightly offset surface (cards, table stripes) |
| `fg` / `fg-muted` | Primary text / secondary text (captions, placeholders) |
| `border` | Dividers, input borders, card outlines |
| `success` / `warning` / `danger` | Status semantics — never used for anything decorative |

Resist adding a new role until an existing one genuinely doesn't fit —
role sprawl is how token systems stop being a single source of truth.

## Typography

- One font stack (`--font-family`), system-first — no webfont dependency
  for an admin-tool UI.
- A small size scale (`sm` / `base` / `lg`) rather than a continuous
  range. If a screen needs a size that isn't in the scale, that's a sign
  the layout — not the scale — needs rethinking.
- Weight is used for hierarchy (`normal` / `medium` / `bold`), not size
  alone. Don't bump font-size to imply "this is more important" when a
  weight change reads more cleanly at the same size.

## Spacing, radius, shadow

- Spacing is a fixed scale (`--space-1` … `--space-8`), not arbitrary
  pixel values chosen per-component. Two components using "the same"
  gap should reference the same token, not two numbers that happen to be
  equal today.
- Radius has three steps (`sm`/`md`/`lg`) mapped to component size, not
  chosen per-designer-taste per component.
- Shadow is reserved for elevation (something floating above the page —
  dropdowns, modals) — not used as a decorative border substitute.

## Component states

Every interactive component defines, at minimum: default, hover, active,
disabled, and — for anything that triggers a request — loading. States
are expressed as token combinations (e.g. `disabled` = `fg-muted` text +
reduced opacity), not as one-off colors invented per component.

## Dark mode

Dark mode is a second set of token *values* under `[data-theme="dark"]`
in the same `theme.css` — components never branch on theme directly.
If a component needs different behavior (not just different colors) in
dark mode, that's usually a sign the component depends on a literal
color somewhere upstream; fix that instead of adding a conditional.

## Accessibility baseline

- Text-on-background token pairs (e.g. `fg` on `bg`) must meet WCAG AA
  contrast (4.5:1 for body text, 3:1 for large text) — check this when a
  token *value* changes, not just when a component is first built.
- Every focusable element has a visible focus state; don't remove the
  default outline without replacing it with an equally visible one.
- Status semantics (`success`/`warning`/`danger`) are never the only
  signal — pair color with an icon or text label so the UI still works
  for color-blind users and in any future high-contrast theme.
