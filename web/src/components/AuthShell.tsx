import type { ReactNode } from "react";

import { BrandLockup } from "./brand";
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
 */
export function AuthShell({
  title,
  subtitle,
  children,
  footer,
}: {
  title: string;
  subtitle?: string;
  children: ReactNode;
  footer?: ReactNode;
}) {
  const t = useT();

  return (
    <div className="flex min-h-dvh flex-col items-center justify-center bg-[var(--color-bg-soft)] p-4">
      <div className="w-full max-w-sm">
        <div className="mb-5">
          <BrandLockup
            name="Portico"
            descriptor={t("brand.descriptor")}
            size={40}
          />
        </div>

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
      </div>
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
