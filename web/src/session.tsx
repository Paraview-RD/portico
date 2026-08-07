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

  const signIn = useCallback(
    async (tenant: string, identifier: string, password: string) => {
      const session = await authApi.login(tenant, identifier, password);
      tokenStore.set(session.token);
      // Remembered so registration and a reload stay in the same tenant
      // rather than falling back to the default one.
      tenantStore.set(tenant);
      setExpired(false);
      setUser(session.user);
    },
    [],
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
    () => ({ user, loading, expired, signIn, signOut, endSession, refresh }),
    [user, loading, expired, signIn, signOut, endSession, refresh],
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
