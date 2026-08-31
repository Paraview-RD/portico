import { useEffect, useState, type CSSProperties, type ReactNode } from "react";

import { BrandLockup } from "./brand";
import { LanguageMenu } from "./menu";
import { authApi } from "../api/endpoints";
import type { Branding } from "../api/types";
import { useT } from "../i18n";

/** Nothing customized — every field empty, so the shell renders unchanged. */
const noBranding: Branding = {
  logoUrl: "",
  productName: "",
  colorPrimary: "",
  fontFamily: "",
  bgImageUrl: "",
  footerPrivacyUrl: "",
  footerTermsUrl: "",
  footerSupportUrl: "",
  loginHeading: "",
};

/**
 * Branding as CSS custom properties, scoped to whatever element receives
 * this as its `style` prop.
 *
 * A custom property cascades to descendants like any other inherited CSS
 * value, so setting it here — rather than on `document.documentElement` —
 * confines the override to this subtree with no cleanup needed on unmount:
 * the shell simply stops rendering, and the property goes with it. The
 * signed-in console, which never mounts this component, never sees it.
 */
function brandingStyle(branding: Branding): CSSProperties {
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
  if (branding.fontFamily) style["--font-family"] = branding.fontFamily;
  return style as CSSProperties;
}

/**
 * The frame around every signed-out screen: sign-in, registration, and the
 * two password-recovery steps.
 *
 * It exists so those four cannot drift apart. They were four independent
 * copies of the same centred card, which is exactly the arrangement where
 * one of them quietly ends up with different padding.
 *
 * The lockup at the top was itself a fifth copy — a hand-assembled tile and
 * wordmark rather than BrandLockup, with the descriptor written in as an
 * English string. So the signed-out screens said "IDENTITY PLATFORM" while
 * the sidebar three clicks later said 身份平台, and the component whose whole
 * purpose is to stop that kind of drift was where it had happened.
 *
 * The lockup sits in the page's top-left corner rather than above the card,
 * which is where the signed-in shell puts it too: a brand mark is a fixture of
 * the page, and one centred over a form reads as part of the form. Changed
 * here rather than on the one screen that prompted it, for the reason this
 * component exists — a per-screen logo position is precisely the drift.
 */
export function AuthShell({
  title,
  subtitle,
  children,
  footer,
  wide = false,
}: {
  title: string;
  subtitle?: string;
  children: ReactNode;
  footer?: ReactNode;
  /**
   * Room for more than a column of fields. Off by default: sign-in and the
   * password screens are two or three controls each, and a wide box around
   * them only lengthens the distance the eye travels between a label and what
   * it labels.
   */
  wide?: boolean;
}) {
  const t = useT();

  // Fetched here, once, rather than in each of the five screens that render
  // through this shell — the same reasoning the class comment above gives
  // for the lockup itself: a per-screen fetch is a per-screen chance to
  // drift, or to simply be forgotten on the next new screen.
  const [branding, setBranding] = useState<Branding>(noBranding);
  useEffect(() => {
    const controller = new AbortController();
    authApi
      .registrationStatus(controller.signal)
      .then((status) => setBranding(status.branding))
      .catch(() => setBranding(noBranding));
    return () => controller.abort();
  }, []);

  return (
    <div
      className="flex min-h-dvh flex-col bg-[var(--color-bg-soft)] p-4"
      style={{
        ...brandingStyle(branding),
        ...(branding.bgImageUrl
          ? {
              backgroundImage: `url(${branding.bgImageUrl})`,
              backgroundSize: "cover",
              backgroundPosition: "center",
            }
          : undefined),
      }}
    >
      {/* Same reasoning as the landing page: everybody on these four screens
          is signed out, so the language they get is whatever their browser
          asked for, and the menu that changes it used to be behind the sign-in
          they are standing in front of. */}
      <header className="flex shrink-0 items-start justify-between gap-4">
        <BrandLockup
          name={branding.productName || "Portico"}
          descriptor={t("brand.descriptor")}
          size={40}
          logoSrc={branding.logoUrl || undefined}
        />
        <LanguageMenu />
      </header>

      {/* Centred in what is left. min-h-dvh rather than h-dvh above, so the
          container grows with its content instead of centring something taller
          than the viewport and putting its top out of reach. */}
      <main
        className={`mx-auto flex w-full flex-1 flex-col justify-center py-8 ${
          wide ? "max-w-lg" : "max-w-sm"
        }`}
      >
        <div className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-bg)] p-6 shadow-[var(--shadow-sm)]">
          <h1 className="text-[length:var(--font-size-lg)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
            {title}
          </h1>
          {subtitle && (
            <p className="mt-1 mb-5 text-[var(--color-fg-muted)]">{subtitle}</p>
          )}
          <div className={subtitle ? undefined : "mt-5"}>{children}</div>
        </div>

        {footer && (
          <div className="mt-4 text-center text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {footer}
          </div>
        )}

        {(branding.footerPrivacyUrl ||
          branding.footerTermsUrl ||
          branding.footerSupportUrl) && (
          // Three named slots, not a list an administrator supplies text
          // for: each link's label comes from this catalogue, translated,
          // and only its address is theirs to set. See
          // docs/settings.md#branding for why.
          <div className="mt-4 flex justify-center gap-4 text-[length:var(--font-size-xs)] text-[var(--color-fg-subtle)]">
            {branding.footerPrivacyUrl && (
              <a
                href={branding.footerPrivacyUrl}
                className="hover:underline"
                target="_blank"
                rel="noreferrer"
              >
                {t("brand.footerPrivacy")}
              </a>
            )}
            {branding.footerTermsUrl && (
              <a
                href={branding.footerTermsUrl}
                className="hover:underline"
                target="_blank"
                rel="noreferrer"
              >
                {t("brand.footerTerms")}
              </a>
            )}
            {branding.footerSupportUrl && (
              <a href={branding.footerSupportUrl} className="hover:underline">
                {t("brand.footerSupport")}
              </a>
            )}
          </div>
        )}
      </main>
    </div>
  );
}

/** A text button for the links under an auth card. */
export function AuthLink({
  onClick,
  children,
}: {
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="text-[var(--color-primary)] underline-offset-2 hover:underline"
    >
      {children}
    </button>
  );
}
