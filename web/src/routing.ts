import type { Route } from "./router";

/**
 * Where a visitor should be sent, given who they are and what this deployment
 * offers.
 *
 * A pure function, extracted from the effect in App that used to hold it. Not
 * for tidiness: this decision is the one that has already shipped a total
 * failure once. `/trial/confirm` was missing from the list of routes a
 * signed-out visitor may reach, so the confirmation link mailed to every trial
 * applicant bounced to the sign-in screen and no tenant was ever created.
 * Nothing caught it, because a rule living inside a `useEffect` beside four
 * early returns and a session hook can only be exercised by driving a browser.
 *
 * Here it can be asked directly, which is what routing.test.ts does.
 */
export interface RoutingState {
  route: Route;
  /** Whether anybody is signed in. */
  signedIn: boolean;
  /** Whether that person may see the administrative screens. */
  isAdmin: boolean;
  /** Whether this deployment gives the root address a page of its own. */
  landing: boolean;
}

/**
 * publicRoutes are the screens reachable without signing in.
 *
 * Everything here is somewhere a person arrives with no account: a sign-in
 * form, a registration form, the two halves of password recovery, and the
 * links that arrive by email. A screen missing from this list is not merely
 * protected — it is unreachable, because the redirect below runs before it
 * renders.
 */
export function publicRoutes(landing: boolean): Route[] {
  const routes: Route[] = [
    "/login",
    "/register",
    "/forgot-password",
    "/reset-password",
    // Reached from a link in an email, by somebody who by definition cannot
    // sign in yet.
    "/verify",
    // The same, and more so: a trial applicant has no account anywhere.
    "/trial",
    "/trial/confirm",
  ];
  // The root address, but only where a page was asked for. Off — the default,
  // and what every deployment did before the landing page existed — "/" is not
  // public, and a signed-out visitor asking for it is sent to sign in.
  if (landing) {
    routes.push("/");
  }
  return routes;
}

/**
 * redirectFor returns the route to navigate to, or null to stay put.
 *
 * Three rules, in order: a signed-out visitor may only be on a public route; a
 * signed-in one has no business on a sign-in form; and an ordinary user who
 * typed an administrative address is shown the home screen with the address
 * bar corrected, because rendering one screen while the address names another
 * makes a reload, a bookmark and a copied link all disagree with what the
 * person is looking at.
 */
export function redirectFor(state: RoutingState): Route | null {
  const isPublic = publicRoutes(state.landing).includes(state.route);

  if (!state.signedIn) {
    return isPublic ? null : "/login";
  }
  if (isPublic) {
    return "/";
  }
  if (!state.isAdmin && state.route !== "/" && state.route !== "/profile") {
    return "/";
  }
  return null;
}
