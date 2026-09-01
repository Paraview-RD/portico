import { describe, expect, it } from "vitest";

import { brandingStyle, noBranding } from "./branding";

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
