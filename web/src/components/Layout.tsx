import type { ReactNode } from "react";

import { Button } from "./ui";
import { useLanguage, useT, languageNames } from "../i18n";
import type { Language } from "../i18n";
import type { Route } from "../router";
import { useRouter } from "../router";
import { useSession } from "../session";

interface NavItem {
  route: Route;
  labelKey: Parameters<ReturnType<typeof useT>>[0];
  /** Administrator-only entries are hidden from normal users (§3.5). */
  adminOnly: boolean;
}

const navItems: NavItem[] = [
  { route: "/users", labelKey: "nav.users", adminOnly: true },
  { route: "/organizations", labelKey: "nav.organizations", adminOnly: true },
  { route: "/audit-logs", labelKey: "nav.auditLogs", adminOnly: true },
  { route: "/settings", labelKey: "nav.settings", adminOnly: true },
  { route: "/profile", labelKey: "nav.profile", adminOnly: false },
];

export function Layout({ children }: { children: ReactNode }) {
  const t = useT();
  const { language, setLanguage } = useLanguage();
  const { user, signOut } = useSession();
  const { route, navigate } = useRouter();

  // The menu follows the caller's role. This is presentation only — the
  // server enforces the same rule, so hiding a link is a convenience, not
  // the security boundary.
  const visibleItems = navItems.filter(
    (item) => !item.adminOnly || user?.role === "SUPER_ADMIN",
  );

  return (
    <div className="flex min-h-dvh bg-[var(--color-bg-soft)]">
      <nav className="flex w-56 flex-col border-r border-[var(--color-border)] bg-[var(--color-bg)] p-3">
        <div className="px-2 py-3 text-[length:var(--font-size-lg)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
          Portico
        </div>

        <ul className="flex flex-1 flex-col gap-0.5">
          {visibleItems.map((item) => {
            const active = route === item.route;
            return (
              <li key={item.route}>
                <button
                  type="button"
                  onClick={() => navigate(item.route)}
                  aria-current={active ? "page" : undefined}
                  className={[
                    "w-full rounded-[var(--radius-sm)] px-3 py-2 text-left transition-colors",
                    active
                      ? "bg-[var(--color-primary)] text-white"
                      : "text-[var(--color-fg-muted)] hover:bg-[var(--color-bg-soft)] hover:text-[var(--color-fg)]",
                  ].join(" ")}
                >
                  {t(item.labelKey)}
                </button>
              </li>
            );
          })}
        </ul>

        <div className="flex flex-col gap-2 border-t border-[var(--color-border)] pt-3">
          <select
            aria-label="Language"
            value={language}
            onChange={(e) => setLanguage(e.target.value as Language)}
            className="h-8 rounded-[var(--radius-sm)] border border-[var(--color-border)] bg-[var(--color-bg)] px-2 text-[length:var(--font-size-sm)] text-[var(--color-fg)]"
          >
            {Object.entries(languageNames).map(([code, name]) => (
              <option key={code} value={code}>
                {name}
              </option>
            ))}
          </select>

          <div className="px-1 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {user?.displayName}
          </div>

          <Button variant="secondary" size="sm" onClick={() => void signOut()}>
            {t("nav.signOut")}
          </Button>
        </div>
      </nav>

      <main className="flex-1 overflow-x-hidden p-6">{children}</main>
    </div>
  );
}
