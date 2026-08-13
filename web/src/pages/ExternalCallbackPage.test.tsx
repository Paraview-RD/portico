import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "../api/client";
import { renderWithLanguage } from "../test/render";

/**
 * The screen a browser lands on coming back from somebody else's provider.
 *
 * Two properties, and both are about a value that works exactly once.
 *
 * The state is consumed by the statement that reads it, so a second exchange
 * of the same one fails — and the failure would report a sign-in that worked
 * as one that did not. React mounts effects twice in development on purpose,
 * and a person pressing reload does the same thing more slowly.
 *
 * The other is what a first-time arrival is told. Nothing here creates
 * accounts, so being refused is not an edge case — it is what happens to
 * everybody the first time, and the sentence has to say what to do next.
 */

const completeExternalSignIn = vi.fn();
const adoptIssuedSession = vi.fn();
const navigate = vi.fn();

vi.mock("../api/endpoints", () => ({
  authApi: {
    completeExternalSignIn: (state: string, code: string, tenant: string) =>
      completeExternalSignIn(state, code, tenant),
  },
}));

vi.mock("../session", () => ({
  useSession: () => ({ adoptIssuedSession }),
}));

vi.mock("../router", () => ({
  useRouter: () => ({ navigate, route: "/" }),
}));

const { ExternalCallbackPage, externalCallback } =
  await import("./ExternalCallbackPage");

beforeEach(() => {
  vi.clearAllMocks();
});

describe("reading the callback out of the address", () => {
  it("finds the tenant in the path, because nothing else carries it", () => {
    expect(
      externalCallback("/t/acme/external/callback", "?state=s&code=c"),
    ).toEqual({ tenant: "acme", state: "s", code: "c", error: "" });
  });

  it("treats the unprefixed address as the default tenant", () => {
    expect(externalCallback("/external/callback", "?state=s&code=c")).toEqual({
      tenant: "",
      state: "s",
      code: "c",
      error: "",
    });
  });

  it("is not a callback anywhere else", () => {
    expect(externalCallback("/login", "?state=s&code=c")).toBeNull();
    expect(externalCallback("/external/callbacks", "")).toBeNull();
    expect(externalCallback("/t/acme/users", "")).toBeNull();
  });

  it("carries a provider's refusal, which arrives instead of a code", () => {
    const callback = externalCallback(
      "/external/callback",
      "?error=access_denied",
    );
    expect(callback?.error).toBe("access_denied");
  });
});

describe("spending the state", () => {
  it("exchanges it once however many times the screen mounts", async () => {
    completeExternalSignIn.mockResolvedValue({
      token: "t",
      expiresAt: "",
      user: {},
    });
    const callback = {
      tenant: "",
      state: "state-mounted-twice",
      code: "c",
      error: "",
    };

    // Mounted, torn down, mounted again with the same state — which is what
    // development mode does on its own and what a reload does deliberately.
    // If the guard were inside the component this would exchange twice, and
    // the second attempt would report a sign-in that worked as a failure.
    const first = renderWithLanguage(
      <ExternalCallbackPage callback={callback} onDone={() => {}} />,
    );
    await vi.waitFor(() => expect(adoptIssuedSession).toHaveBeenCalled());
    first.unmount();

    renderWithLanguage(
      <ExternalCallbackPage callback={callback} onDone={() => {}} />,
    );
    await Promise.resolve();

    expect(completeExternalSignIn).toHaveBeenCalledTimes(1);
  });

  it("takes the session up and leaves for the console", async () => {
    completeExternalSignIn.mockResolvedValue({
      token: "issued-token",
      expiresAt: "",
      user: {},
    });

    renderWithLanguage(
      <ExternalCallbackPage
        callback={{ tenant: "acme", state: "s1", code: "c", error: "" }}
        onDone={() => {}}
      />,
    );

    await vi.waitFor(() =>
      expect(adoptIssuedSession).toHaveBeenCalledWith("issued-token", "acme"),
    );
    expect(navigate).toHaveBeenCalledWith("/");
    // The tenant travels with the call rather than through storage, so a
    // landing that fails leaves the browser pointed where it already was.
    expect(completeExternalSignIn).toHaveBeenCalledWith("s1", "c", "acme");
  });

  it("sends a completed binding to the profile, not to a sentence", async () => {
    // A binding answers with the link rather than a session. The screen must
    // tell them apart by what came back: the same address serves both, and
    // the difference was decided before the browser left.
    completeExternalSignIn.mockResolvedValue({
      id: "identity-1",
      providerId: "p1",
      providerName: "Company Google",
      subject: "sub-1",
      email: "someone@example.com",
      createdAt: "",
      lastUsedAt: null,
    });

    renderWithLanguage(
      <ExternalCallbackPage
        callback={{ tenant: "", state: "s2", code: "c", error: "" }}
        onDone={() => {}}
      />,
    );

    await vi.waitFor(() => expect(navigate).toHaveBeenCalledWith("/profile"));
    expect(adoptIssuedSession).not.toHaveBeenCalled();
  });
});

describe("what a first-time arrival is told", () => {
  it("names the way in, in the reader's language", async () => {
    completeExternalSignIn.mockRejectedValue(
      new ApiError(
        "EXTERNAL_IDENTITY_UNKNOWN",
        "That identity is not linked to any account here.",
        401,
      ),
    );

    renderWithLanguage(
      <ExternalCallbackPage
        callback={{ tenant: "", state: "s3", code: "c", error: "" }}
        onDone={() => {}}
      />,
      "zh-CN",
    );

    // Not an assertion on the wording, which will be edited. It is an
    // assertion that the refusal reaches the screen translated rather than
    // as the server's English or as a bare code.
    const message = await screen.findByText(/关联|绑定|个人中心/);
    expect(message).toBeTruthy();
  });

  it("says which provider refused, when the provider is the one refusing", async () => {
    renderWithLanguage(
      <ExternalCallbackPage
        callback={{ tenant: "", state: "", code: "", error: "access_denied" }}
        onDone={() => {}}
      />,
    );

    expect(await screen.findByText(/access_denied/)).toBeTruthy();
    expect(completeExternalSignIn).not.toHaveBeenCalled();
  });
});
