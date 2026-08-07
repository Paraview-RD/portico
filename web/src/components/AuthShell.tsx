import type { ReactNode } from "react";

import { BrandTile } from "./brand";

/**
 * The frame around every signed-out screen: sign-in, registration, and the
 * two password-recovery steps.
 *
 * It exists so those four cannot drift apart. They were four independent
 * copies of the same centred card, which is exactly the arrangement where
 * one of them quietly ends up with different padding.
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
  return (
    <div className="flex min-h-dvh flex-col items-center justify-center bg-[var(--color-bg-soft)] p-4">
      <div className="w-full max-w-sm">
        <div className="mb-5 flex items-center gap-3">
          <BrandTile size={40} />
          <div className="leading-tight">
            <div className="text-[length:var(--font-size-xl)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
              Portico
            </div>
            <div className="text-[length:var(--font-size-xs)] tracking-[0.08em] text-[var(--color-fg-subtle)] uppercase">
              Identity Platform
            </div>
          </div>
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
