import { useEffect } from "react";

import { Layout } from "./components/Layout";
import { useT } from "./i18n";
import { AuditLogsPage } from "./pages/AuditLogsPage";
import { LoginPage } from "./pages/LoginPage";
import { OrganizationsPage } from "./pages/OrganizationsPage";
import { ProfilePage } from "./pages/ProfilePage";
import { RegisterPage } from "./pages/RegisterPage";
import { SettingsPage } from "./pages/SettingsPage";
import { UsersPage } from "./pages/UsersPage";
import { useRouter } from "./router";
import { useSession } from "./session";

export function App() {
  const t = useT();
  const { user, loading } = useSession();
  const { route, navigate } = useRouter();

  // Signed-out visitors may only reach sign-in and registration; everyone
  // else lands on sign-in. This mirrors the server's rules so the UI never
  // renders a screen whose data it cannot fetch.
  useEffect(() => {
    if (loading) return;
    const publicRoutes = ["/login", "/register"];
    if (!user && !publicRoutes.includes(route)) {
      navigate("/login");
    } else if (user && publicRoutes.includes(route)) {
      navigate(user.role === "SUPER_ADMIN" ? "/users" : "/profile");
    }
  }, [user, loading, route, navigate]);

  if (loading) {
    return (
      <div className="flex min-h-dvh items-center justify-center text-[var(--color-fg-muted)]">
        {t("common.loading")}
      </div>
    );
  }

  if (!user) {
    return route === "/register" ? <RegisterPage /> : <LoginPage />;
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
