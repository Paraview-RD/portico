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
import { codesCarryingDetail, errorsEnUS } from "./errors-en-US";
import { errorsZhCN } from "./errors-zh-CN";
import { zhCN } from "./zh-CN";

export type Language = "en-US" | "zh-CN";

const bundles: Record<Language, Record<TranslationKey, string>> = {
  "en-US": enUS,
  "zh-CN": zhCN,
};

const errorBundles: Record<Language, Record<string, string>> = {
  "en-US": errorsEnUS,
  "zh-CN": errorsZhCN,
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

/**
 * Turns anything thrown by the API layer into a sentence in the reader's
 * language.
 *
 * The server answers in English by design — its messages also go to logs and
 * to API clients, and a second language there would mean an Accept-Language
 * round trip and a second place for the wording to live. But the response
 * carries a stable, specific code, so the console can say the same thing in
 * the reader's own language without the server knowing anything about it.
 *
 * Three fallbacks, in order, because each says more than the next:
 *
 *  1. A translation for the code.
 *  2. The server's own message. English, but correct and specific — which is
 *     what a code added on the server and not yet translated should produce,
 *     rather than a blank or a raw identifier.
 *  3. A generic apology, for a failure that never reached the API at all.
 */
export type DescribeError = (error: unknown) => string;

export function useErrorMessage(): DescribeError {
  const { language, t } = useLanguage();

  return useCallback(
    (error: unknown) => {
      if (!(error instanceof Error)) return t("common.unexpectedError");

      const code = (error as { code?: unknown }).code;
      if (typeof code !== "string") {
        return error.message || t("common.unexpectedError");
      }

      const translated = errorBundles[language][code];
      if (!translated) return error.message || t("common.unexpectedError");

      // Some messages carry a value the translation cannot — the URI that
      // was rejected, the entity id that did not match. Dropping it would
      // make the error tidier and less useful, so the server's own text
      // follows the translation for those.
      if (codesCarryingDetail.has(code) && error.message) {
        return `${translated}（${error.message}）`;
      }
      return translated;
    },
    [language, t],
  );
}
