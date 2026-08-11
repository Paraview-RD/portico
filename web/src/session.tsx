/**
 * Session state: who is signed in, and what ends a session.
 *
 * Ending a session is centralized here because three different things do it
 * — signing out, changing a password, and the server rejecting a stale token
 * — and each one must clear the stored token and return to sign-in. Anything
 * that only cleared local state would leave the user on a screen whose next
 * request fails.
 */

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import type { ReactNode } from "react";

import { authApi, settingsApi, userApi } from "./api/endpoints";
import { setSessionEndedHandler, tenantStore, tokenStore } from "./api/client";
import type { User } from "./api/types";
import { hasStoredLanguage, matchLanguage, useLanguage } from "./i18n";

interface SessionValue {
  user: User | null;
  /**
   * Whether the tenant offers the explanatory panel at the top of each
   * administrative screen.
   *
   * Held here rather than fetched by each panel: seven screens each asking
   * would be seven requests for one boolean, and a panel that rendered
   * before its answer arrived would flash the explanation at somebody whose
   * tenant has switched it off — which is precisely what they switched off.
   *
   * True until told otherwise, and true for anybody who cannot read
   * settings. Only administrators see these panels at all, so the fallback
   * costs nothing; defaulting the other way would hide them from every
   * deployment that has never set it.
   */
  showGuides: boolean;
  /** True until the stored token has been checked against the server. */
  loading: boolean;
  /** Set when the previous session ended unexpectedly, for the login page. */
  expired: boolean;
  /**
   * Signs in to a tenant with any of the three identifiers. An empty tenant
   * code means the default tenant.
   */
  signIn: (
    tenant: string,
    identifier: string,
    password: string,
  ) => Promise<void>;
  /**
   * Replaces a password the server refused as expired, and signs in.
   *
   * Separate from signIn because it goes to a different endpoint: the
   * server will not issue a token for an expired password, so there is no
   * session to change it from.
   */
  signInWithReplacedPassword: (
    tenant: string,
    identifier: string,
    currentPassword: string,
    newPassword: string,
  ) => Promise<void>;
  signOut: () => Promise<void>;
  /** Ends the session locally, for flows the server already invalidated. */
  endSession: () => void;
  refresh: () => Promise<void>;
}

const SessionContext = createContext<SessionValue | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const { setLanguage } = useLanguage();
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [expired, setExpired] = useState(false);
  const [showGuides, setShowGuides] = useState(true);

  // Asked for only where it can be answered. Settings are administrator-only
  // and so are every screen these panels appear on, so a non-administrator
  // asking would get a 403 for a value they will never use.
  const loadGuidePreference = useCallback(async (account: User) => {
    if (account.role !== "SUPER_ADMIN") return;
    try {
      const settings = await settingsApi.get();
      setShowGuides(settings.showGuides);
    } catch {
      // Left on. A panel nobody asked to hide is a smaller failure than an
      // explanation missing from a screen somebody has never seen.
    }
  }, []);

  const endSession = useCallback(() => {
    tokenStore.clear();
    setUser(null);
  }, []);

  // The API client calls this when the server rejects a token, so an
  // expired session drops the user at sign-in instead of showing an error
  // on whatever screen they happened to be on.
  useEffect(() => {
    setSessionEndedHandler(() => {
      setUser(null);
      setExpired(true);
    });
  }, []);

  const refresh = useCallback(async () => {
    if (!tokenStore.get()) {
      setUser(null);
      setLoading(false);
      return;
    }
    try {
      const account = await userApi.me();
      setUser(account);
      void loadGuidePreference(account);
    } catch {
      // The client already cleared the token for auth failures; any other
      // error means we cannot confirm the session, so treat it as signed out.
      setUser(null);
    } finally {
      setLoading(false);
    }
  }, []);

  // Restore the session on load, so a refresh does not sign the user out.
  useEffect(() => {
    void refresh();
  }, [refresh]);

  // Whose account this is, asked of the one endpoint that answers it fully.
  //
  // A sign-in response carries a user too, and taking it from there is a
  // round trip cheaper — but it is a different, smaller shape: it is the
  // account row, while /users/me is the account row plus what the server
  // works out about it, starting with when this password expires. Reading
  // the cheaper one meant the expiry warning appeared on the next reload and
  // not at the sign-in it is about, which is the one moment somebody is
  // holding the password in their head and could act on it.
  const adoptSession = useCallback(
    async (token: string, tenant: string) => {
      tokenStore.set(token);
      // Remembered so registration and a reload stay in the same tenant rather
      // than falling back to the default one.
      tenantStore.set(tenant);
      setExpired(false);

      const account = await userApi.me();
      setUser(account);
      void loadGuidePreference(account);

      // The account's own language, but only where this browser has not been
      // told one. A person who picked English here should not have it
      // switched under them because an administrator filled in a field, and
      // somebody signing in to a shared machine should not inherit whatever
      // the last person chose either — "no stored choice" is the only case
      // where the account is the better answer than the browser.
      if (!hasStoredLanguage()) {
        const preferred = matchLanguage(account.profile?.preferredLanguage);
        if (preferred) setLanguage(preferred);
      }
    },
    [setLanguage],
  );

  const signIn = useCallback(
    async (tenant: string, identifier: string, password: string) => {
      const session = await authApi.login(tenant, identifier, password);
      await adoptSession(session.token, tenant);
    },
    [adoptSession],
  );

  const signInWithReplacedPassword = useCallback(
    async (
      tenant: string,
      identifier: string,
      currentPassword: string,
      newPassword: string,
    ) => {
      const session = await authApi.changeExpiredPassword({
        tenant,
        identifier,
        currentPassword,
        newPassword,
      });
      await adoptSession(session.token, tenant);
    },
    [adoptSession],
  );

  const signOut = useCallback(async () => {
    try {
      await authApi.logout();
    } catch {
      // Signing out locally matters more than the server call succeeding;
      // the token is revoked server-side either way once it expires.
    }
    endSession();
    setExpired(false);
  }, [endSession]);

  const value = useMemo(
    () => ({
      user,
      showGuides,
      loading,
      expired,
      signIn,
      signInWithReplacedPassword,
      signOut,
      endSession,
      refresh,
    }),
    [
      user,
      showGuides,
      loading,
      expired,
      signIn,
      signInWithReplacedPassword,
      signOut,
      endSession,
      refresh,
    ],
  );

  return (
    <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
  );
}

/**
 * The session if there is one, and null outside a provider.
 *
 * For the UI primitives, which have to render on their own — a component
 * test mounting one control should not have to stand up a session, and a
 * primitive that throws without one is a primitive that cannot be tested in
 * isolation. Everything that genuinely needs a session uses useSession and
 * still gets the error.
 */
export function useOptionalSession(): SessionValue | null {
  return useContext(SessionContext);
}

export function useSession(): SessionValue {
  const context = useContext(SessionContext);
  if (!context) {
    throw new Error("useSession must be used inside a SessionProvider");
  }
  return context;
}
