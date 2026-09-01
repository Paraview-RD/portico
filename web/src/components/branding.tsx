/**
 * Rendering branding, shared between the real unauthenticated screens
 * (AuthShell) and the live preview on the Branding settings page.
 *
 * Split out of AuthShell rather than left inline, and not duplicated into
 * the preview either. A preview that renders branding even slightly
 * differently from the real screen is worse than no preview — it would
 * tell an administrator their setting looks right when what a visitor
 * actually sees is subtly different. This is the same failure AuthShell
 * itself already exists to prevent for the four screens it wraps ("a
 * per-screen logo position is precisely the drift").
 */

import { useState, type CSSProperties, type ReactNode } from "react";

import { Modal } from "./ui";
import type { Branding } from "../api/types";
import { useT } from "../i18n";

/** Nothing customized — every field empty, so a screen renders unchanged. */
export const noBranding: Branding = {
  logoUrl: "",
  productName: "",
  colorPrimary: "",
  fontFamily: "",
  bgImageUrl: "",
  footerPrivacyMode: "",
  footerPrivacyUrl: "",
  footerPrivacyText: "",
  footerTermsMode: "",
  footerTermsUrl: "",
  footerTermsText: "",
  footerSupportMode: "",
  footerSupportUrl: "",
  footerSupportText: "",
  loginHeading: "",
};

/**
 * Branding as CSS custom properties, scoped to whatever element receives
 * this as its `style` prop.
 *
 * A custom property cascades to descendants like any other inherited CSS
 * value, so setting it here — rather than on `document.documentElement` —
 * confines the override to this subtree with no cleanup needed on unmount:
 * the element simply stops rendering, and the property goes with it. The
 * signed-in console, which never renders branding, never sees it.
 */
export function brandingStyle(branding: Branding): CSSProperties {
  const style: Record<string, string> = {};
  if (branding.colorPrimary) {
    style["--color-primary"] = branding.colorPrimary;
    // theme.css defines hover/active/soft as their own hex values, not as a
    // function of --color-primary, so setting the base alone would leave a
    // button in the brand colour that goes theme-blue the moment somebody
    // hovers it. color-mix keeps the four in the same relationship
    // theme.css's own values are in, without this file knowing how to do
    // colour math.
    style["--color-primary-hover"] =
      `color-mix(in srgb, ${branding.colorPrimary} 85%, black)`;
    style["--color-primary-active"] =
      `color-mix(in srgb, ${branding.colorPrimary} 70%, black)`;
    style["--color-primary-soft"] =
      `color-mix(in srgb, ${branding.colorPrimary} 15%, white)`;
    style["--color-primary-soft-hover"] =
      `color-mix(in srgb, ${branding.colorPrimary} 25%, white)`;
  }
  if (branding.fontFamily) {
    // Setting only the custom property does nothing here: theme.css
    // declares `font-family: var(--font-family)` exactly once, on <html>,
    // far above this subtree — every element below inherits *that*
    // resolved value, not a live re-read of --font-family at this deeper
    // scope. (Colour works the opposite way: every component references
    // var(--color-primary) at its own call site, so overriding the
    // variable here is enough.) The real `font-family` property has to be
    // set directly so normal CSS inheritance carries the actual font down
    // to every descendant that does not specify its own.
    style["--font-family"] = branding.fontFamily;
    style.fontFamily = branding.fontFamily;
  }
  return style as CSSProperties;
}

/** Background-image styling, split out because AuthShell applies it to a
 * different element than the colour/font custom properties. */
export function brandingBackgroundStyle(branding: Branding): CSSProperties {
  if (!branding.bgImageUrl) return {};
  return {
    backgroundImage: `url(${branding.bgImageUrl})`,
    backgroundSize: "cover",
    backgroundPosition: "center",
  };
}

/**
 * A very small, safe markup subset for the three footer text fields —
 * bold, italic, and lists, and nothing else. See docs/settings.md#branding
 * for the reasoning; the summary: this text renders on the four
 * unauthenticated screens (sign-in, register, forgot-password, authorize),
 * so nothing an administrator pastes in may ever become an executable
 * action in a visitor's session. A link's href is the one part of
 * Markdown that can carry one — javascript: schemes, open redirects —
 * bold/italic/lists cannot, which is why those render and links do not.
 * Footer *links* already exist as their own mechanism (mode "link", a
 * validated address) — this does not duplicate or extend that.
 *
 * The other half of the safety story is that this never touches
 * dangerouslySetInnerHTML. Parsing stops at plain data — ParagraphBlock /
 * ListBlock / InlineToken below — and renderBrandingText turns that data
 * into real React elements. There is no HTML string at any point for
 * something pasted in to break out of, so there is nothing to sanitize:
 * the safety property holds by construction, not by escaping.
 */

export type InlineToken =
  { type: "text"; value: string } | { type: "bold" | "italic"; value: string };

/**
 * Tokenizes one line (or paragraph) of text into bold/italic/plain runs.
 *
 * Left to right, no nesting: the first `**…**` or `*…*` found closes at
 * the next matching delimiter, full stop. An admin writing
 * "*a **b** c*" gets a defensible-but-unspecified split rather than
 * recursion — deliberately, since nesting bold inside italic buys a footer
 * blurb nothing and the extra parser complexity is exactly where a subtle
 * bug would hide.
 */
export function tokenizeInline(text: string): InlineToken[] {
  const tokens: InlineToken[] = [];
  let buffer = "";
  const flush = () => {
    if (buffer) tokens.push({ type: "text", value: buffer });
    buffer = "";
  };

  let i = 0;
  while (i < text.length) {
    if (text.startsWith("**", i)) {
      const end = text.indexOf("**", i + 2);
      if (end > i + 2) {
        flush();
        tokens.push({ type: "bold", value: text.slice(i + 2, end) });
        i = end + 2;
        continue;
      }
    } else if (text[i] === "*") {
      const end = text.indexOf("*", i + 1);
      if (end > i + 1) {
        flush();
        tokens.push({ type: "italic", value: text.slice(i + 1, end) });
        i = end + 1;
        continue;
      }
    }
    buffer += text[i];
    i++;
  }
  flush();
  return tokens;
}

export type TextBlock =
  { type: "paragraph"; text: string } | { type: "list"; items: string[] };

/**
 * Splits text into paragraph and list blocks on blank-line boundaries. A
 * block renders as a list only when every one of its lines starts with
 * "- " or "* " — anything else, including a mix, stays one paragraph, so a
 * stray "-" at the start of a sentence never turns it into a list item.
 */
export function parseTextBlocks(text: string): TextBlock[] {
  return text
    .split(/\n\s*\n/)
    .map((block) => block.trim())
    .filter(Boolean)
    .map((block): TextBlock => {
      const lines = block
        .split("\n")
        .map((line) => line.trim())
        .filter(Boolean);
      const isList =
        lines.length > 0 &&
        lines.every((line) => line.startsWith("- ") || line.startsWith("* "));
      if (isList) {
        return { type: "list", items: lines.map((line) => line.slice(2)) };
      }
      return { type: "paragraph", text: block };
    });
}

function renderInline(text: string): ReactNode[] {
  return tokenizeInline(text).map((token, i) => {
    switch (token.type) {
      case "bold":
        return <strong key={i}>{token.value}</strong>;
      case "italic":
        return <em key={i}>{token.value}</em>;
      default:
        return token.value;
    }
  });
}

/** Turns footer text into the elements above, for the modal both the real
 * screens and the branding preview share. */
function renderBrandingText(text: string): ReactNode {
  return parseTextBlocks(text).map((block, i) => {
    if (block.type === "list") {
      return (
        <ul
          key={i}
          className="mb-3 list-disc pl-5 text-[length:var(--font-size-sm)] text-[var(--color-fg)] last:mb-0"
        >
          {block.items.map((item, j) => (
            <li key={j}>{renderInline(item)}</li>
          ))}
        </ul>
      );
    }
    return (
      <p
        key={i}
        className="mb-3 text-[length:var(--font-size-sm)] text-[var(--color-fg)] last:mb-0"
      >
        {renderInline(block.text)}
      </p>
    );
  });
}

type FooterSlot = "privacy" | "terms" | "support";

/**
 * The three named footer slots. Not a list an administrator supplies text
 * for — each slot's label comes from this catalogue, translated, and only
 * the address or the text behind it is theirs to set. See
 * docs/settings.md#branding.
 */
export function BrandingFooterLinks({ branding }: { branding: Branding }) {
  const t = useT();
  const [open, setOpen] = useState<FooterSlot | "">("");

  const slots: {
    slot: FooterSlot;
    label: string;
    mode: Branding["footerPrivacyMode"];
    url: string;
    text: string;
  }[] = [
    {
      slot: "privacy",
      label: t("brand.footerPrivacy"),
      mode: branding.footerPrivacyMode,
      url: branding.footerPrivacyUrl,
      text: branding.footerPrivacyText,
    },
    {
      slot: "terms",
      label: t("brand.footerTerms"),
      mode: branding.footerTermsMode,
      url: branding.footerTermsUrl,
      text: branding.footerTermsText,
    },
    {
      slot: "support",
      label: t("brand.footerSupport"),
      mode: branding.footerSupportMode,
      url: branding.footerSupportUrl,
      text: branding.footerSupportText,
    },
  ];

  const visible = slots.filter((s) => s.mode === "link" || s.mode === "text");
  if (visible.length === 0) return null;

  const openSlot = slots.find((s) => s.slot === open);

  return (
    <>
      <div className="mt-4 flex justify-center gap-4 text-[length:var(--font-size-xs)] text-[var(--color-fg-subtle)]">
        {visible.map((s) =>
          s.mode === "link" ? (
            <a
              key={s.slot}
              href={s.url}
              className="hover:underline"
              target="_blank"
              rel="noreferrer"
            >
              {s.label}
            </a>
          ) : (
            <button
              key={s.slot}
              type="button"
              onClick={() => setOpen(s.slot)}
              className="hover:underline"
            >
              {s.label}
            </button>
          ),
        )}
      </div>

      <Modal
        open={open !== ""}
        title={openSlot?.label ?? ""}
        onClose={() => setOpen("")}
      >
        {openSlot && renderBrandingText(openSlot.text)}
      </Modal>
    </>
  );
}
