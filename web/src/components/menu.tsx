import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";

import { ChevronDownIcon, GlobeIcon } from "./icons";
import { useLanguage, useT, languageNames } from "../i18n";
import type { Language } from "../i18n";

/**
 * The popover menu, and the one of them that belongs to no shell.
 *
 * All of this lived inside Layout.tsx, which is the signed-in frame — so the
 * language menu was reachable only after signing in. Everything before that
 * point took whatever the browser's Accept-Language happened to say, with no
 * way to disagree: a visitor whose browser is set to English, reading a
 * Chinese page, or the reverse, had to go and change a browser setting to read
 * the page that is asking them to sign up.
 *
 * Moved here rather than copied, and that is the point. A second language
 * menu built for the signed-out screens is a second set of paddings, a second
 * dismiss behaviour, and eventually a second answer to what the languages are
 * called.
 */

export function Menu({ children }: { children: ReactNode }) {
  return (
    <div
      role="menu"
      className="absolute right-0 z-30 mt-1 min-w-44 overflow-hidden rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-bg)] py-1 shadow-[var(--shadow-md)]"
    >
      {children}
    </div>
  );
}

export function MenuItem({
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
export function useDismissable() {
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

/** Picks the language, wherever somebody is when they want to. */
export function LanguageMenu() {
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
