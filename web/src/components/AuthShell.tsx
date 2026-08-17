import type { ReactNode } from "react";

import { BrandLockup } from "./brand";
import { LanguageMenu } from "./menu";
import { useT } from "../i18n";

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

  return (
    <div className="flex min-h-dvh flex-col bg-[var(--color-bg-soft)] p-4">
      {/* Same reasoning as the landing page: everybody on these four screens
          is signed out, so the language they get is whatever their browser
          asked for, and the menu that changes it used to be behind the sign-in
          they are standing in front of. */}
      <header className="flex shrink-0 items-start justify-between gap-4">
        <BrandLockup
          name="Portico"
          descriptor={t("brand.descriptor")}
          size={40}
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
