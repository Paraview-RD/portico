/**
 * A minimal hash-free router over the History API.
 *
 * The app has eight screens and no nested or parameterized routes, so a
 * routing library would be more code than it saves — and the current
 * react-router releases carry an open advisory. This is the whole router.
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

export type Route =
  | "/login"
  | "/register"
  | "/users"
  | "/organizations"
  | "/audit-logs"
  | "/settings"
  | "/profile";

const routes: Route[] = [
  "/login",
  "/register",
  "/users",
  "/organizations",
  "/audit-logs",
  "/settings",
  "/profile",
];

function currentRoute(): Route {
  const path = window.location.pathname as Route;
  return routes.includes(path) ? path : "/users";
}

interface RouterValue {
  route: Route;
  navigate: (route: Route) => void;
}

const RouterContext = createContext<RouterValue | null>(null);

export function RouterProvider({ children }: { children: ReactNode }) {
  const [route, setRoute] = useState<Route>(currentRoute);

  // Keep in step with the browser's back and forward buttons.
  useEffect(() => {
    const onPopState = () => setRoute(currentRoute());
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  const navigate = useCallback((next: Route) => {
    if (window.location.pathname !== next) {
      window.history.pushState({}, "", next);
    }
    setRoute(next);
  }, []);

  const value = useMemo(() => ({ route, navigate }), [route, navigate]);

  return (
    <RouterContext.Provider value={value}>{children}</RouterContext.Provider>
  );
}

export function useRouter(): RouterValue {
  const context = useContext(RouterContext);
  if (!context) {
    throw new Error("useRouter must be used inside a RouterProvider");
  }
  return context;
}
