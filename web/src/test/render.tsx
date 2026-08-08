/**
 * Rendering helpers for component tests.
 *
 * Every screen reads its text through the language context, so a bare
 * render() throws. Going through here also means a test states which
 * language it is asserting in, rather than inheriting whatever the browser
 * that ran it would have picked.
 */
import { render } from "@testing-library/react";
import type { ReactElement } from "react";

import { LanguageProvider } from "../i18n";
import type { Language } from "../i18n";

const STORAGE_KEY = "portico.language";

/** Renders inside the language provider, in the language given. */
export function renderWithLanguage(
  ui: ReactElement,
  language: Language = "en-US",
) {
  // The provider picks its initial language from storage, then the browser.
  // Setting storage is how a test chooses, and it has to happen before the
  // provider mounts.
  localStorage.setItem(STORAGE_KEY, language);
  return render(<LanguageProvider>{ui}</LanguageProvider>);
}
