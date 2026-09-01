import { describe, expect, it } from "vitest";

import {
  brandingStyle,
  noBranding,
  parseTextBlocks,
  tokenizeInline,
} from "./branding";

/**
 * brandingStyle's font handling has a failure mode CSS itself hides: only
 * the --font-family custom property looked correct in a naive check,
 * because it is genuinely present in the style object — but theme.css
 * resolves `font-family: var(--font-family)` exactly once, on <html>, so
 * a descendant subtree overriding just the custom property changes
 * nothing anyone sees. This pins the fix: the real `fontFamily` property
 * must also be set, since that is what CSS inheritance actually carries
 * down to elements that do not set their own.
 */
describe("brandingStyle", () => {
  it("sets both the custom property and the real font-family when a font is chosen", () => {
    const style = brandingStyle({
      ...noBranding,
      fontFamily: "Georgia, serif",
    });
    expect(style.fontFamily).toBe("Georgia, serif");
    expect((style as Record<string, string>)["--font-family"]).toBe(
      "Georgia, serif",
    );
  });

  it("sets nothing font-related when no font is chosen", () => {
    const style = brandingStyle(noBranding);
    expect(style.fontFamily).toBeUndefined();
    expect((style as Record<string, string>)["--font-family"]).toBeUndefined();
  });

  it("derives the hover/active/soft primary shades from the chosen colour", () => {
    const style = brandingStyle({
      ...noBranding,
      colorPrimary: "#e11d48",
    }) as Record<string, string>;
    expect(style["--color-primary"]).toBe("#e11d48");
    expect(style["--color-primary-hover"]).toContain("#e11d48");
  });
});

/**
 * The footer text fields render on unauthenticated screens, so the parser
 * has to stop at exactly bold/italic/lists and nothing that could carry an
 * executable action — see the doc comment on renderBrandingText in
 * branding.tsx. These tests pin the parsed *data*, not the rendered JSX:
 * tokenizeInline/parseTextBlocks never touch the DOM, matching this file's
 * existing pure-function style rather than adding a rendering dependency.
 */
describe("tokenizeInline", () => {
  it("splits bold, italic, and plain runs", () => {
    expect(tokenizeInline("plain **bold** plain *italic* plain")).toEqual([
      { type: "text", value: "plain " },
      { type: "bold", value: "bold" },
      { type: "text", value: " plain " },
      { type: "italic", value: "italic" },
      { type: "text", value: " plain" },
    ]);
  });

  it("leaves an unmatched delimiter as literal text", () => {
    expect(tokenizeInline("a * b")).toEqual([{ type: "text", value: "a * b" }]);
  });

  it("never recognises link syntax — a pasted link stays plain text", () => {
    const text =
      "See [our site](https://evil.example.test) or javascript:alert(1)";
    expect(tokenizeInline(text)).toEqual([{ type: "text", value: text }]);
  });

  it("renders a literal <script> tag as an inert text run, not markup", () => {
    const text = "<script>alert(1)</script>";
    expect(tokenizeInline(text)).toEqual([{ type: "text", value: text }]);
  });
});

describe("parseTextBlocks", () => {
  it("splits paragraphs on blank lines", () => {
    expect(parseTextBlocks("first\n\nsecond")).toEqual([
      { type: "paragraph", text: "first" },
      { type: "paragraph", text: "second" },
    ]);
  });

  it("renders a block as a list only when every line starts with - or *", () => {
    expect(parseTextBlocks("- one\n- two\n* three")).toEqual([
      { type: "list", items: ["one", "two", "three"] },
    ]);
  });

  it("keeps a block a paragraph when only some lines look like list items", () => {
    expect(parseTextBlocks("- one\nnot a list item")).toEqual([
      { type: "paragraph", text: "- one\nnot a list item" },
    ]);
  });

  it("ignores blank input", () => {
    expect(parseTextBlocks("")).toEqual([]);
    expect(parseTextBlocks("   \n\n  ")).toEqual([]);
  });
});
