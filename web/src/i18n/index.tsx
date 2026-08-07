import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
} from "react";
import type { ReactNode } from "react";

import { enUS } from "./en-US";
import type { TranslationKey } from "./en-US";
import { zhCN } from "./zh-CN";

export type Language = "en-US" | "zh-CN";

const bundles: Record<Language, Record<TranslationKey, string>> = {
  "en-US": enUS,
  "zh-CN": zhCN,
};

export const languageNames: Record<Language, string> = {
  "en-US": "English",
  "zh-CN": "简体中文",
};

const STORAGE_KEY = "portico.language";

/** Picks the initial language from storage, then the browser, then English. */
function detectLanguage(): Language {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === "en-US" || stored === "zh-CN") {
    return stored;
  }
  return navigator.language.toLowerCase().startsWith("zh") ? "zh-CN" : "en-US";
}

/** Substitutes {0}, {1}, … with the given arguments. */
function interpolate(template: string, args: unknown[]): string {
  if (args.length === 0) return template;
  return template.replace(/\{(\d+)\}/g, (match, index) => {
    const value = args[Number(index)];
    return value === undefined ? match : String(value);
  });
}

export type Translate = (key: TranslationKey, ...args: unknown[]) => string;

interface LanguageContextValue {
  language: Language;
  setLanguage: (language: Language) => void;
  t: Translate;
}

const LanguageContext = createContext<LanguageContextValue | null>(null);

export function LanguageProvider({ children }: { children: ReactNode }) {
  const [language, setLanguageState] = useState<Language>(detectLanguage);

  const setLanguage = useCallback((next: Language) => {
    localStorage.setItem(STORAGE_KEY, next);
    setLanguageState(next);
    document.documentElement.lang = next;
  }, []);

  const t = useCallback<Translate>(
    (key, ...args) => interpolate(bundles[language][key] ?? key, args),
    [language],
  );

  const value = useMemo(
    () => ({ language, setLanguage, t }),
    [language, setLanguage, t],
  );

  return (
    <LanguageContext.Provider value={value}>
      {children}
    </LanguageContext.Provider>
  );
}

export function useLanguage(): LanguageContextValue {
  const context = useContext(LanguageContext);
  if (!context) {
    throw new Error("useLanguage must be used inside a LanguageProvider");
  }
  return context;
}

/** Convenience hook for components that only need the translate function. */
export function useT(): Translate {
  return useLanguage().t;
}
