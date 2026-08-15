import { describe, expect, it } from "vitest";

import type { Route } from "./router";
import { publicRoutes, redirectFor } from "./routing";

/**
 * The redirect rule, asked directly.
 *
 * This exists because of what happened when it could not be: `/trial/confirm`
 * was missing from the public list, so the confirmation link mailed to every
 * trial applicant bounced to the sign-in screen and no tenant was ever
 * created. The API answered correctly throughout and had passing contract
 * tests; the component tests did not run the rule; the browser suite had never
 * opened that address. A rule nothing can ask is a rule nothing checks.
 */

const signedOut = { signedIn: false, isAdmin: false };
const admin = { signedIn: true, isAdmin: true };
const ordinary = { signedIn: true, isAdmin: false };

describe("a signed-out visitor", () => {
  it.each([
    "/login",
    "/register",
    "/forgot-password",
    "/reset-password",
    "/verify",
  ] as Route[])("may stay on %s", (route) => {
    expect(redirectFor({ ...signedOut, route, landing: false })).toBeNull();
  });

  // The two that shipped broken. Both are opened from an email by somebody
  // who has no account anywhere — which is the whole point of the trial — so
  // sending them to a sign-in form asks for something they cannot have.
  it.each(["/trial", "/trial/confirm"] as Route[])(
    "may reach %s, which is opened by somebody with no account",
    (route) => {
      expect(redirectFor({ ...signedOut, route, landing: false })).toBeNull();
    },
  );

  it("is sent to sign in from an administrative screen", () => {
    expect(redirectFor({ ...signedOut, route: "/users", landing: false })).toBe(
      "/login",
    );
  });
});

describe("the root address", () => {
  // The default, and the behaviour every deployment had before the landing
  // page existed. This is the assertion that says the toggle changed nothing
  // for anybody who did not ask for it.
  it("sends a signed-out visitor to sign in when no landing page is configured", () => {
    expect(redirectFor({ ...signedOut, route: "/", landing: false })).toBe(
      "/login",
    );
  });

  it("is left alone when a landing page is configured", () => {
    expect(redirectFor({ ...signedOut, route: "/", landing: true })).toBeNull();
  });

  it("is not made public by the landing page for anything else", () => {
    // The toggle adds exactly one route and no more.
    const off = publicRoutes(false);
    const on = publicRoutes(true);
    expect(on.filter((route) => !off.includes(route))).toEqual(["/"]);
  });
});

describe("a signed-in visitor", () => {
  it("is taken off the sign-in form", () => {
    expect(redirectFor({ ...admin, route: "/login", landing: false })).toBe(
      "/",
    );
  });

  // Even with a landing page configured: somebody who is already signed in has
  // no use for a page explaining what this is.
  it("is taken off the landing page", () => {
    expect(redirectFor({ ...admin, route: "/", landing: true })).toBe("/");
  });

  it("stays on an administrative screen when they are an administrator", () => {
    expect(
      redirectFor({ ...admin, route: "/users", landing: false }),
    ).toBeNull();
  });

  it("is sent home from an administrative screen when they are not", () => {
    expect(redirectFor({ ...ordinary, route: "/users", landing: false })).toBe(
      "/",
    );
  });

  it("may see their own profile without being an administrator", () => {
    expect(
      redirectFor({ ...ordinary, route: "/profile", landing: false }),
    ).toBeNull();
  });

  it("stays on the home screen", () => {
    expect(redirectFor({ ...ordinary, route: "/", landing: false })).toBeNull();
  });
});

// A signed-in visitor on "/" with a landing page configured is told to go to
// "/" — where they already are. App checks for that before navigating, and
// this records why that check is not redundant.
it("can answer with the route it was given", () => {
  expect(redirectFor({ ...admin, route: "/", landing: true })).toBe("/");
});
