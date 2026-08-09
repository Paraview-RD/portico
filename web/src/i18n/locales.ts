import { enUS } from "./en-US";
import type { TranslationKey } from "./en-US";
import { errorsEnUS } from "./errors-en-US";
import { errorsZhCN } from "./errors-zh-CN";
import { zhCN } from "./zh-CN";

/**
 * A language the console is available in.
 *
 * One entry per language, and this is the only list. Adding a third means
 * writing its two bundles and adding a line here — no component, no switch,
 * and no `language === "zh-CN"` anywhere else in the tree.
 */
export interface Locale {
  /**
   * A BCP 47 tag, the same vocabulary the server speaks. It has to match,
   * because this is what an account's `preferredLanguage` is compared
   * against and what decides the language of the mail it receives.
   */
  code: string;
  /** Shown in the menu, in its own language rather than in the reader's. */
  name: string;
  /**
   * The manual's own prefix for this language, which is deliberately a
   * different vocabulary — a URL prefix is short because a person sees it.
   * Empty for the language the manual is written in.
   */
  docsPrefix: string;
  messages: Record<TranslationKey, string>;
  /**
   * Console text for the server's error codes. Typed against the English
   * bundle over in errors-zh-CN.ts, so omitting a code does not compile.
   */
  errors: Record<string, string>;
}

export const locales: readonly Locale[] = [
  {
    code: "en-US",
    name: "English",
    docsPrefix: "",
    messages: enUS,
    errors: errorsEnUS,
  },
  {
    code: "zh-CN",
    name: "简体中文",
    docsPrefix: "zh/",
    messages: zhCN,
    errors: errorsZhCN,
  },
];

/** The language everything falls back to, and the one bundles are typed against. */
export const defaultLanguage = "en-US";

export type Language = string;

/**
 * Resolves a tag onto a language the console has.
 *
 * Exact match first, then the language subtag alone, so "zh", "zh-Hans", and
 * a browser sending "zh-TW" all land on 简体中文. The same rule the server
 * applies to the same values — an account whose preference works for its
 * mail and not for its console would be a difference nobody could explain.
 *
 * Returns undefined rather than the default, so a caller can tell "they asked
 * for something we do not have" from "they asked for English".
 */
export function matchLanguage(tag: string | undefined): Language | undefined {
  if (!tag) return undefined;
  const wanted = tag.trim().toLowerCase();
  if (!wanted) return undefined;

  const exact = locales.find((locale) => locale.code.toLowerCase() === wanted);
  if (exact) return exact.code;

  const language = wanted.split("-")[0];
  const prefix = locales.find(
    (locale) => locale.code.toLowerCase().split("-")[0] === language,
  );
  return prefix?.code;
}

export function localeFor(language: Language): Locale {
  return (
    locales.find((locale) => locale.code === language) ??
    locales.find((locale) => locale.code === defaultLanguage)!
  );
}

/**
 * Where the manual lives for a language.
 *
 * The manual is served by this same process under /docs, so a reader never
 * leaves the deployment and never reads a page for a version they are not
 * running.
 */
export function docsUrl(language: Language, page = ""): string {
  return `/docs/${localeFor(language).docsPrefix}${page}`;
}
