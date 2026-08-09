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

import { authApi, userApi } from "./api/endpoints";
import { setSessionEndedHandler, tenantStore, tokenStore } from "./api/client";
import type { User } from "./api/types";

interface SessionValue {
  user: User | null;
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
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [expired, setExpired] = useState(false);

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
      setUser(await userApi.me());
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
  const adoptSession = useCallback(async (token: string, tenant: string) => {
    tokenStore.set(token);
    // Remembered so registration and a reload stay in the same tenant rather
    // than falling back to the default one.
    tenantStore.set(tenant);
    setExpired(false);
    setUser(await userApi.me());
  }, []);

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

export function useSession(): SessionValue {
  const context = useContext(SessionContext);
  if (!context) {
    throw new Error("useSession must be used inside a SessionProvider");
  }
  return context;
}
