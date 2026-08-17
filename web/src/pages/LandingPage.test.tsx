import { screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";

import { renderWithLanguage } from "../test/render";

/**
 * What the root address says, and — the part worth a test — what it only says
 * sometimes.
 *
 * The notice warning that everything here is periodically wiped is shown on
 * the strength of one fact: whether self-service trials are on. That flag is
 * what turns a private console into a public demonstration, so it is the
 * honest condition. Both ways of getting it wrong are real. Shown where
 * trials are off, it tells a company's own staff that their directory is
 * disposable. Missing where trials are on, somebody puts their address into a
 * form and finds out afterwards.
 *
 * Neither failure is visible in a type check: both are one boolean rendering
 * a paragraph or not.
 */

const trialStatus = vi.fn();

vi.mock("../api/endpoints", () => ({
  trialApi: { trialStatus: (signal: AbortSignal) => trialStatus(signal) },
}));

vi.mock("../router", () => ({
  useRouter: () => ({ navigate: vi.fn(), route: "/" }),
}));

const { LandingPage } = await import("./LandingPage");

beforeEach(() => {
  trialStatus.mockReset();
});

it("says what this is before asking for anything", async () => {
  trialStatus.mockResolvedValue({ enabled: false });
  renderWithLanguage(<LandingPage />);

  expect(
    screen.getByRole("heading", {
      level: 1,
      name: /Sign your people in, once, to everything/,
    }),
  ).toBeVisible();
  // The way in is a button, not a line of small print.
  expect(screen.getByRole("button", { name: "Sign in" })).toBeVisible();
});

// The language menu used to live only inside the signed-in shell, which put it
// behind the sign-in that this page is asking somebody to go through. Everyone
// who reads this page is signed out, so this is where it matters most.
it("lets somebody who has no account change the language", async () => {
  trialStatus.mockResolvedValue({ enabled: false });
  renderWithLanguage(<LandingPage />);

  expect(screen.getByRole("button", { name: "Language" })).toBeVisible();
});

it("offers a trial only where trials are on", async () => {
  trialStatus.mockResolvedValue({ enabled: true });
  renderWithLanguage(<LandingPage />);

  await waitFor(() => {
    expect(
      screen.getByRole("button", { name: "Get a tenant to try" }),
    ).toBeVisible();
  });
});

it("warns that this is a demonstration, where it is one", async () => {
  trialStatus.mockResolvedValue({ enabled: true });
  renderWithLanguage(<LandingPage />);

  await waitFor(() => {
    expect(screen.getByText(/public demonstration/)).toBeVisible();
  });
});

it("says nothing about being a demonstration where it is not one", async () => {
  trialStatus.mockResolvedValue({ enabled: false });
  renderWithLanguage(<LandingPage />);

  // Awaited rather than asserted immediately: the flag arrives from a request,
  // so "not there yet" is the state this page starts in and would pass a
  // synchronous check no matter what the answer turned out to be.
  await waitFor(() => {
    expect(screen.getByRole("button", { name: "Sign in" })).toBeVisible();
  });
  expect(screen.queryByText(/public demonstration/)).not.toBeInTheDocument();
});

// A deployment where the status call fails is not a deployment with trials on.
// The catch already defaulted to false; this holds it, because the failure
// mode is the loud one — a button that refuses every visitor.
it("offers no trial when it cannot tell", async () => {
  trialStatus.mockRejectedValue(new Error("network"));
  renderWithLanguage(<LandingPage />);

  await waitFor(() => {
    expect(screen.getByRole("button", { name: "Sign in" })).toBeVisible();
  });
  expect(
    screen.queryByRole("button", { name: "Get a tenant to try" }),
  ).not.toBeInTheDocument();
});
