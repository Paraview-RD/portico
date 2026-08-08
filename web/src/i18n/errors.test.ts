import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { createElement } from "react";
import { describe, expect, it } from "vitest";

import { ApiError } from "../api/client";
import { LanguageProvider, useErrorMessage } from "./index";
import { errorsEnUS } from "./errors-en-US";
import { errorsZhCN } from "./errors-zh-CN";

/**
 * The server answers in English because its messages also go to logs and to
 * API clients. The console has the code, so it says the same thing in the
 * reader's language. These hold the three fallbacks in order — each says
 * more than the next, and getting the order wrong turns a specific error
 * into a generic one.
 */

function wrapper(language: "en-US" | "zh-CN") {
  localStorage.setItem("portico.language", language);
  return ({ children }: { children: ReactNode }) =>
    createElement(LanguageProvider, null, children);
}

function describeIn(language: "en-US" | "zh-CN", error: unknown): string {
  const { result } = renderHook(() => useErrorMessage(), {
    wrapper: wrapper(language),
  });
  return result.current(error);
}

describe("useErrorMessage", () => {
  it("renders a known code in the reader's language", () => {
    const err = new ApiError(
      "CAS_SERVICE_WILDCARD",
      "Wildcards are not accepted. Register the URL prefix itself; anything beginning with it matches.",
      400,
    );

    expect(describeIn("zh-CN", err)).toBe(errorsZhCN.CAS_SERVICE_WILDCARD);
    expect(describeIn("en-US", err)).toBe(errorsEnUS.CAS_SERVICE_WILDCARD);
  });

  it("falls back to the server's message for a code it does not know", () => {
    // The failure that matters: a code added on the server and not yet
    // translated must degrade to English, not to a blank or an identifier.
    const err = new ApiError(
      "SOME_CODE_ADDED_LATER",
      "Something specific the server wanted to say.",
      400,
    );

    expect(describeIn("zh-CN", err)).toBe(
      "Something specific the server wanted to say.",
    );
  });

  it("keeps the server's own text for codes that carry a value", () => {
    // Translating this one and dropping the rest would be a downgrade: the
    // sentence would be tidier and would no longer say which entity id was
    // the problem.
    const err = new ApiError(
      "METADATA_ENTITY_ID_MISMATCH",
      "That metadata declares entity id https://other.example/sp, but this registration is for https://sp.example/meta.",
      400,
    );

    const message = describeIn("zh-CN", err);
    expect(message).toContain(errorsZhCN.METADATA_ENTITY_ID_MISMATCH);
    expect(message).toContain("https://other.example/sp");
  });

  it("has something to say about a failure that never reached the API", () => {
    expect(describeIn("zh-CN", new TypeError("Failed to fetch"))).toBe(
      "Failed to fetch",
    );
    expect(describeIn("zh-CN", "not an error at all")).not.toBe("");
  });
});

describe("error bundles", () => {
  // The type system already requires zh-CN to cover every en-US key. This
  // catches the other half: an entry left as the English string, which
  // compiles and renders English into a Chinese screen.
  it("translates every code rather than copying the English", () => {
    const untranslated = Object.keys(errorsEnUS).filter(
      (code) =>
        errorsZhCN[code as keyof typeof errorsEnUS] ===
        errorsEnUS[code as keyof typeof errorsEnUS],
    );

    expect(untranslated).toEqual([]);
  });

  it("ends every message as a sentence", () => {
    // Mixed punctuation across a table this size reads as carelessness in
    // exactly the place a reader is already annoyed.
    const wrong = Object.entries(errorsZhCN).filter(
      ([, message]) => !message.endsWith("。"),
    );

    expect(wrong).toEqual([]);
  });
});
