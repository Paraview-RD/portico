import { useEffect, useMemo, useState } from "react";

import { Layout } from "./components/Layout";
import { useT } from "./i18n";
import { ApplicationsPage } from "./pages/ApplicationsPage";
import { AuditLogsPage } from "./pages/AuditLogsPage";
import { AuthorizePage, pendingAuthorization } from "./pages/AuthorizePage";
import {
  ExternalCallbackPage,
  externalCallback,
} from "./pages/ExternalCallbackPage";
import { ForgotPasswordPage } from "./pages/ForgotPasswordPage";
import { LoginPage } from "./pages/LoginPage";
import { GroupsPage } from "./pages/GroupsPage";
import { IdentityProvidersPage } from "./pages/IdentityProvidersPage";
import { OrganizationsPage } from "./pages/OrganizationsPage";
import { PortalPage } from "./pages/PortalPage";
import { ProfilePage } from "./pages/ProfilePage";
import { ProvisioningPage } from "./pages/ProvisioningPage";
import { RegisterPage } from "./pages/RegisterPage";
import { ResetPasswordPage } from "./pages/ResetPasswordPage";
import { TrialConfirmPage } from "./pages/TrialConfirmPage";
import { TrialPage } from "./pages/TrialPage";
import { VerifyPage } from "./pages/VerifyPage";
import { SettingsPage } from "./pages/SettingsPage";
import { UserAttributesPage } from "./pages/UserAttributesPage";
import { UsersPage } from "./pages/UsersPage";
import { WebhooksPage } from "./pages/WebhooksPage";
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

  // A browser coming back from somebody else's provider. Read once, from the
  // address it landed on, for the same reason as the two above: this is a
  // path the router does not know, carrying values that are spent the first
  // time they are read.
  //
  // State rather than a memo, because unlike those two it ends: the screen
  // clears it once the exchange is over, and what was a landing becomes an
  // ordinary console the router owns again.
  const [returning, setReturning] = useState(() =>
    externalCallback(window.location.pathname, window.location.search),
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
    // Nor mid-exchange. Sending a signed-out visitor to /login here would
    // unmount the screen holding the only copy of a single-use code, and the
    // sign-in that was seconds from working would be one nobody can retry.
    if (returning) return;

    const publicRoutes = [
      "/login",
      "/register",
      "/forgot-password",
      "/reset-password",
      // Reached from a link in an email, by somebody who by definition
      // cannot sign in yet.
      "/verify",
      // The same, and more so: a trial applicant has no account anywhere.
      // Left out of this list, both of these bounced to the sign-in screen —
      // which meant the confirmation link mailed to every applicant asked
      // them to sign in to the account it was about to create. The switch
      // below has always rendered them; being rendered is no use while
      // something navigates away first.
      "/trial",
      "/trial/confirm",
    ];
    if (!user && !publicRoutes.includes(route)) {
      navigate("/login");
    } else if (user && publicRoutes.includes(route)) {
      navigate("/");
    } else if (
      user &&
      user.role !== "SUPER_ADMIN" &&
      route !== "/" &&
      route !== "/profile"
    ) {
      // An ordinary user who typed an administrative URL is shown the home
      // screen, and the address bar is corrected to say so. Rendering one
      // screen while the address names another means a reload, a bookmark,
      // and a copied link all disagree with what the person is looking at.
      navigate("/");
    }
  }, [user, loading, route, navigate, pending, casLogout, returning]);

  if (pending) {
    return <AuthorizePage request={pending} />;
  }

  // Before the loading check as well as before routing: the exchange does not
  // need to know who is signed in — it spends a state the server is holding
  // — and waiting on the session would leave a spinner where the one screen
  // that can complete the sign-in should be.
  if (returning) {
    return (
      <ExternalCallbackPage
        callback={returning}
        onDone={() => setReturning(null)}
      />
    );
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
      case "/verify":
        return <VerifyPage />;
      // Reachable whether or not this deployment offers trials: the form
      // refuses on submit rather than on mount, so somebody who typed the
      // address is told by the server rather than by a screen that flickers
      // into an error.
      case "/trial":
        return <TrialPage />;
      case "/trial/confirm":
        return <TrialConfirmPage />;
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
    // The home screen and their own profile, and nothing else — an
    // administrative URL typed by hand lands on the portal rather than on a
    // screen that would only produce 403s.
    return route === "/profile" ? <ProfilePage /> : <PortalPage />;
  }

  switch (route) {
    case "/":
      return <PortalPage />;
    case "/organizations":
      return <OrganizationsPage />;
    case "/groups":
      return <GroupsPage />;
    case "/applications":
      return <ApplicationsPage />;
    case "/provisioning":
      return <ProvisioningPage />;
    case "/webhooks":
      return <WebhooksPage />;
    case "/identity-providers":
      return <IdentityProvidersPage />;
    case "/audit-logs":
      return <AuditLogsPage />;
    case "/settings":
      return <SettingsPage />;
    case "/user-attributes":
      return <UserAttributesPage />;
    case "/profile":
      return <ProfilePage />;
    default:
      return <UsersPage />;
  }
}
