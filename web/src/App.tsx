import { useEffect, useMemo } from "react";

import { Layout } from "./components/Layout";
import { useT } from "./i18n";
import { ApplicationsPage } from "./pages/ApplicationsPage";
import { AuditLogsPage } from "./pages/AuditLogsPage";
import { AuthorizePage, pendingAuthorization } from "./pages/AuthorizePage";
import { ForgotPasswordPage } from "./pages/ForgotPasswordPage";
import { LoginPage } from "./pages/LoginPage";
import { OrganizationsPage } from "./pages/OrganizationsPage";
import { ProfilePage } from "./pages/ProfilePage";
import { RegisterPage } from "./pages/RegisterPage";
import { ResetPasswordPage } from "./pages/ResetPasswordPage";
import { SettingsPage } from "./pages/SettingsPage";
import { UsersPage } from "./pages/UsersPage";
import { useRouter } from "./router";
import { useSession } from "./session";

export function App() {
  const t = useT();
  const { user, loading, signOut } = useSession();
  const { route, navigate } = useRouter();

  // An application waiting on a sign-in, over either protocol. Read once,
  // from the URL the provider redirected to.
  // Memoized because it is an object: recreating it on every render would
  // make it a fresh dependency each time, and the effects that depend on it
  // would run forever.
  const pending = useMemo(
    () => pendingAuthorization(window.location.search),
    [],
  );

  // A CAS client sent the browser here to end the session. It could not end
  // it itself: the session is a token this application holds, which a plain
  // navigation to /cas/logout cannot reach.
  const casLogout = useMemo(
    () => new URLSearchParams(window.location.search).has("cas_logout"),
    [],
  );

  useEffect(() => {
    if (casLogout && user) void signOut();
  }, [casLogout, user, signOut]);

  // Signed-out visitors may only reach sign-in and registration; everyone
  // else lands on sign-in. This mirrors the server's rules so the UI never
  // renders a screen whose data it cannot fetch.
  useEffect(() => {
    if (loading) return;
    // Except while an authorization request is in flight. Sending an
    // already-signed-in administrator on to /users would drop the query
    // parameter, and the application that started the sign-in would wait
    // forever without ever being told why.
    if (pending) return;
    // Nor while signing out on a CAS client's behalf, which would otherwise
    // bounce them into the console for the moment before it completes.
    if (casLogout) return;

    const publicRoutes = [
      "/login",
      "/register",
      "/forgot-password",
      "/reset-password",
    ];
    if (!user && !publicRoutes.includes(route)) {
      navigate("/login");
    } else if (user && publicRoutes.includes(route)) {
      navigate(user.role === "SUPER_ADMIN" ? "/users" : "/profile");
    }
  }, [user, loading, route, navigate, pending, casLogout]);

  if (pending) {
    return <AuthorizePage request={pending} />;
  }

  if (loading) {
    return (
      <div className="flex min-h-dvh items-center justify-center text-[var(--color-fg-muted)]">
        {t("common.loading")}
      </div>
    );
  }

  if (!user) {
    switch (route) {
      case "/register":
        return <RegisterPage />;
      case "/forgot-password":
        return <ForgotPasswordPage />;
      case "/reset-password":
        return <ResetPasswordPage />;
      default:
        return <LoginPage />;
    }
  }

  return (
    <Layout>
      <AuthenticatedRoute route={route} isAdmin={user.role === "SUPER_ADMIN"} />
    </Layout>
  );
}

function AuthenticatedRoute({
  route,
  isAdmin,
}: {
  route: string;
  isAdmin: boolean;
}) {
  // A normal user who types an admin URL gets their own profile rather than
  // a screen that would only produce 403s. The server enforces the same
  // rule; this just avoids showing them a wall of permission errors.
  if (!isAdmin) {
    return <ProfilePage />;
  }

  switch (route) {
    case "/organizations":
      return <OrganizationsPage />;
    case "/applications":
      return <ApplicationsPage />;
    case "/audit-logs":
      return <AuditLogsPage />;
    case "/settings":
      return <SettingsPage />;
    case "/profile":
      return <ProfilePage />;
    default:
      return <UsersPage />;
  }
}
