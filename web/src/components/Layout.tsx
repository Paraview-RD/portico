import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";

import { BrandLockup } from "./brand";
import {
  ApplicationsIcon,
  AttributesIcon,
  AuditIcon,
  ChevronDownIcon,
  GlobeIcon,
  GroupsIcon,
  HomeIcon,
  IdentityProvidersIcon,
  OrganizationsIcon,
  ProfileIcon,
  ProvisioningIcon,
  SettingsIcon,
  SignOutIcon,
  UsersIcon,
  TenantIcon,
  WebhooksIcon,
} from "./icons";
import { request } from "../api/client";
import { useLanguage, useT, languageNames } from "../i18n";
import type { Language } from "../i18n";
import type { Route } from "../router";
import { useRouter } from "../router";
import { useSession } from "../session";

type LabelKey = Parameters<ReturnType<typeof useT>>[0];

interface NavItem {
  route: Route;
  labelKey: LabelKey;
  icon: (props: { size?: number }) => ReactNode;
  /** Administrator-only entries are hidden from normal users (§3.5). */
  adminOnly: boolean;
  /**
   * Entries that also need a capability the server grants, rather than only a
   * role this console can read off the session.
   *
   * There is one: the operator console exists only where the deployment asked
   * for it and only for the default tenant's administrator, and neither of
   * those is a fact the browser holds. So the server answers it on
   * /users/me and this is where the answer is spent.
   */
  needs?: "mayManageTenants";
}

interface NavGroup {
  /** Omitted for the first group, which needs no heading above it. */
  labelKey?: LabelKey;
  items: NavItem[];
}

// Each group answers one question, and that is what decides what goes in it:
// who is in the system, who connects to it, what has happened, and how it is
// configured. A group defined that way stays right as the menu grows,
// because a new screen answers one of those questions or reveals a fifth.
//
// The earlier arrangement had two groups, and the second was "operations" —
// which is not a question, it is where things go when the first group will
// not have them. Application registration ended up there beside the audit
// log and the password rules, though it is neither a record of the past nor
// a preference: it is the list of systems that trust this one to say who
// somebody is, which is close to the point of the product.
const navGroups: NavGroup[] = [
  {
    // No heading. The home screen is not a category, and giving it one
    // would put a label above a single item at the top of every sidebar.
    items: [
      {
        route: "/",
        labelKey: "nav.home",
        icon: HomeIcon,
        // The one entry everybody sees, which is the point of it.
        adminOnly: false,
      },
    ],
  },
  {
    labelKey: "nav.group.directory",
    items: [
      {
        route: "/users",
        labelKey: "nav.users",
        icon: UsersIcon,
        adminOnly: true,
      },
      {
        route: "/organizations",
        labelKey: "nav.organizations",
        icon: OrganizationsIcon,
        adminOnly: true,
      },
      {
        // Beside organizations rather than under them: they are siblings,
        // not a hierarchy. One is where somebody sits, the other is a set
        // they belong to.
        route: "/groups",
        labelKey: "nav.groups",
        icon: GroupsIcon,
        adminOnly: true,
      },
    ],
  },
  {
    // Everything that connects another system to this one, in the four
    // directions it can happen: applications ask Portico who somebody is, a
    // directory pushes accounts in, webhooks push events out, and an
    // identity provider is asked who somebody is. Three of them were
    // reachable before — two buried as sections of the settings page, which
    // is where a capability goes to be undiscovered.
    labelKey: "nav.group.integration",
    items: [
      {
        route: "/applications",
        labelKey: "nav.applications",
        icon: ApplicationsIcon,
        adminOnly: true,
      },
      {
        route: "/provisioning",
        labelKey: "nav.provisioning",
        icon: ProvisioningIcon,
        adminOnly: true,
      },
      {
        route: "/webhooks",
        labelKey: "nav.webhooks",
        icon: WebhooksIcon,
        adminOnly: true,
      },
      {
        route: "/identity-providers",
        labelKey: "nav.identityProviders",
        icon: IdentityProvidersIcon,
        adminOnly: true,
      },
    ],
  },
  {
    // On its own rather than beside settings. Reading the audit log is a job
    // somebody comes here to do, and often the only one they came for;
    // filing it under configuration would make the record of what happened
    // look like a knob.
    labelKey: "nav.group.audit",
    items: [
      {
        route: "/audit-logs",
        labelKey: "nav.auditLogs",
        icon: AuditIcon,
        adminOnly: true,
      },
    ],
  },
  {
    labelKey: "nav.group.system",
    items: [
      {
        route: "/settings",
        labelKey: "nav.settings",
        icon: SettingsIcon,
        adminOnly: true,
      },
      {
        // Configuration rather than directory. What is defined here is which
        // questions an account form asks — the answers belong to the people
        // on the user screen, but the list of questions is a knob, and it is
        // the one somebody reaches for after reading about field mappings.
        route: "/user-attributes",
        labelKey: "nav.userAttributes",
        icon: AttributesIcon,
        adminOnly: true,
      },
      {
        // Last in the group that holds the knobs, and absent on nearly every
        // deployment. It is the only entry here that leads outside the
        // reader's own tenant.
        route: "/tenants",
        labelKey: "nav.tenants",
        icon: TenantIcon,
        adminOnly: true,
        needs: "mayManageTenants",
      },
    ],
  },
  {
    // The only group an ordinary user sees, which is why it survives as its
    // own rather than being folded into the account menu in the top bar:
    // doing that would leave them looking at an empty sidebar.
    labelKey: "nav.group.account",
    items: [
      {
        route: "/profile",
        labelKey: "nav.profile",
        icon: ProfileIcon,
        adminOnly: false,
      },
    ],
  },
];

export function Layout({ children }: { children: ReactNode }) {
  const { user } = useSession();
  const { route } = useRouter();

  // The menu follows the caller's role. This is presentation only — the
  // server enforces the same rule, so hiding a link is a convenience, not
  // the security boundary.
  const isAdmin = user?.role === "SUPER_ADMIN";
  const groups = navGroups
    .map((group) => ({
      ...group,
      items: group.items.filter(
        (item) =>
          (!item.adminOnly || isAdmin) &&
          // A capability the server decides. Absent from the session — an
          // older token, or a deployment without the feature — means no,
          // which is the safe direction: the route refuses anyway, and an
          // entry that leads to a refusal is worse than no entry.
          (!item.needs || user?.[item.needs] === true),
      ),
    }))
    .filter((group) => group.items.length > 0);

  const current = groups
    .flatMap((g) => g.items)
    .find((item) => item.route === route);

  return (
    <div className="flex min-h-dvh bg-[var(--color-bg-soft)]">
      <Sidebar groups={groups} />

      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar locationKey={current?.labelKey} />
        <main className="min-w-0 flex-1 overflow-x-hidden p-6">
          {/* One column, the same on every screen. Without it each page
              decided its own width against the whole viewport, so a table
              ran to the far edge of a wide display while a form stopped
              short — and the difference read as a mistake rather than as
              two kinds of content. */}
          <div className="mx-auto w-full max-w-[var(--content-width)]">
            {children}
          </div>
        </main>
      </div>
    </div>
  );
}

function Sidebar({ groups }: { groups: NavGroup[] }) {
  const t = useT();
  const { route, navigate } = useRouter();
  const version = useVersion();

  return (
    <nav
      className="flex shrink-0 flex-col border-r border-[var(--color-border)] bg-[var(--color-bg)]"
      style={{ width: "var(--sidebar-width)" }}
    >
      <div
        className="flex items-center border-b border-[var(--color-border)] px-4"
        style={{ height: "var(--topbar-height)" }}
      >
        <BrandLockup
          name="Portico"
          descriptor={t("brand.descriptor")}
          size={30}
        />
      </div>

      <div className="flex-1 overflow-y-auto px-3 py-4">
        {groups.map((group, index) => (
          <div
            key={group.labelKey ?? index}
            className={index > 0 ? "mt-5" : undefined}
          >
            {group.labelKey && (
              <div className="mb-1.5 px-3 text-[length:var(--font-size-xs)] font-[weight:var(--font-weight-medium)] tracking-[0.06em] text-[var(--color-fg-subtle)] uppercase">
                {t(group.labelKey)}
              </div>
            )}
            <ul className="flex flex-col gap-0.5">
              {group.items.map((item) => {
                const Icon = item.icon;
                const active = route === item.route;
                return (
                  <li key={item.route}>
                    <button
                      type="button"
                      onClick={() => navigate(item.route)}
                      aria-current={active ? "page" : undefined}
                      className={[
                        "flex w-full items-center gap-2.5 rounded-[var(--radius-sm)] px-3 py-2 text-left transition-colors",
                        active
                          ? "bg-[var(--color-primary-soft)] font-[weight:var(--font-weight-medium)] text-[var(--color-primary)]"
                          : "text-[var(--color-fg-muted)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-fg)]",
                      ].join(" ")}
                    >
                      <Icon size={18} />
                      <span className="truncate">{t(item.labelKey)}</span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </div>
        ))}
      </div>

      <div className="border-t border-[var(--color-border)] px-4 py-3 text-[length:var(--font-size-xs)] text-[var(--color-fg-subtle)]">
        {/* A development build reports "dev", which must not become "vdev". */}
        {version ? (/^\d/.test(version) ? `v${version}` : version) : null}
      </div>
    </nav>
  );
}

/**
 * useVersion reads the running build from the health endpoint.
 *
 * It is the one thing an operator looking at a screenshot needs and cannot
 * get any other way, which is why it sits in the corner of every page rather
 * than on a settings screen nobody opens.
 */
function useVersion(): string {
  const [version, setVersion] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    request<{ version: string }>("/health", {
      anonymous: true,
      signal: controller.signal,
    })
      .then((health) => setVersion(health.version))
      .catch(() => {
        // Not knowing the version is not worth showing an error for.
      });
    return () => controller.abort();
  }, []);

  return version;
}

/**
 * The top bar carries where you are on the left and who you are on the
 * right.
 *
 * Session controls live here rather than at the foot of the menu because
 * that is where people look for them: the account you are signed in as is a
 * property of the window, not an item in the navigation, and putting it at
 * the bottom of a list of pages invites reading it as one.
 */
function TopBar({ locationKey }: { locationKey?: LabelKey }) {
  const t = useT();

  return (
    <header
      className="sticky top-0 z-20 flex shrink-0 items-center justify-between gap-4 border-b border-[var(--color-border)] bg-[var(--color-bg)] px-6"
      style={{ height: "var(--topbar-height)" }}
    >
      <div className="min-w-0 truncate text-[var(--color-fg-muted)]">
        {locationKey ? t(locationKey) : null}
      </div>

      <div className="flex shrink-0 items-center gap-1">
        <LanguageMenu />
        <div className="mx-1 h-5 w-px bg-[var(--color-border)]" />
        <AccountMenu />
      </div>
    </header>
  );
}

function LanguageMenu() {
  const { language, setLanguage } = useLanguage();
  const t = useT();
  const { open, setOpen, ref } = useDismissable();

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={() => setOpen(!open)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t("nav.language")}
        className="flex items-center gap-1.5 rounded-[var(--radius-sm)] px-2.5 py-1.5 text-[var(--color-fg-muted)] transition-colors hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-fg)]"
      >
        <GlobeIcon size={16} />
        <span className="hidden sm:inline">{languageNames[language]}</span>
        <ChevronDownIcon size={14} />
      </button>

      {open && (
        <Menu>
          {Object.entries(languageNames).map(([code, name]) => (
            <MenuItem
              key={code}
              selected={code === language}
              onClick={() => {
                setLanguage(code as Language);
                setOpen(false);
              }}
            >
              {name}
            </MenuItem>
          ))}
        </Menu>
      )}
    </div>
  );
}

function AccountMenu() {
  const t = useT();
  const { user, signOut } = useSession();
  const { open, setOpen, ref } = useDismissable();

  if (!user) return null;

  // The first character of the display name, which for a Chinese name is the
  // surname and for a Latin one the initial — both of which are what someone
  // expects to see in an avatar.
  const initial = [...user.displayName][0] ?? "?";

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={() => setOpen(!open)}
        aria-haspopup="menu"
        aria-expanded={open}
        className="flex items-center gap-2 rounded-[var(--radius-sm)] px-2 py-1.5 transition-colors hover:bg-[var(--color-bg-hover)]"
      >
        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-[var(--color-primary)] text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-medium)] text-[var(--color-fg-on-primary)]">
          {initial}
        </span>
        <span className="hidden max-w-32 truncate text-[var(--color-fg)] sm:inline">
          {user.displayName}
        </span>
        <ChevronDownIcon size={14} className="text-[var(--color-fg-muted)]" />
      </button>

      {open && (
        <Menu>
          <div className="border-b border-[var(--color-border)] px-3 py-2">
            <div className="truncate text-[var(--color-fg)]">
              {user.displayName}
            </div>
            <div className="truncate text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
              {user.username}
            </div>
          </div>
          <MenuItem onClick={() => void signOut()}>
            <span className="flex items-center gap-2">
              <SignOutIcon size={16} />
              {t("nav.signOut")}
            </span>
          </MenuItem>
        </Menu>
      )}
    </div>
  );
}

function Menu({ children }: { children: ReactNode }) {
  return (
    <div
      role="menu"
      className="absolute right-0 z-30 mt-1 min-w-44 overflow-hidden rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-bg)] py-1 shadow-[var(--shadow-md)]"
    >
      {children}
    </div>
  );
}

function MenuItem({
  children,
  selected,
  onClick,
}: {
  children: ReactNode;
  selected?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onClick}
      className={[
        "block w-full px-3 py-2 text-left transition-colors",
        selected
          ? "bg-[var(--color-primary-soft)] text-[var(--color-primary)]"
          : "text-[var(--color-fg)] hover:bg-[var(--color-bg-hover)]",
      ].join(" ")}
    >
      {children}
    </button>
  );
}

/**
 * useDismissable closes a popover on an outside click or Escape.
 *
 * Both, not one: a mouse user expects clicking away to work and a keyboard
 * user expects Escape to, and a menu that only honours one of them is a menu
 * somebody gets stuck in.
 */
function useDismissable() {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;

    const onPointerDown = (event: MouseEvent) => {
      if (!ref.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };

    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  return { open, setOpen, ref };
}
