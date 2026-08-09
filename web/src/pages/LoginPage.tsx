import { useEffect, useState } from "react";

import { ApiError, tenantStore } from "../api/client";
import { authApi } from "../api/endpoints";
import { AuthLink, AuthShell } from "../components/AuthShell";
import { Alert, Button, Field, Input } from "../components/ui";
import { useErrorMessage, useT } from "../i18n";
import { useRouter } from "../router";
import { useSession } from "../session";

export function LoginPage() {
  const t = useT();
  const describeError = useErrorMessage();
  const { signIn, signInWithReplacedPassword, expired } = useSession();
  const { navigate } = useRouter();

  // Remembered from the last sign-in, or taken from a ?tenant= link, so an
  // operator can hand out a URL that lands on the right tenant. Blank means
  // the default tenant, which is all a single-tenant deployment ever needs.
  const params = new URLSearchParams(window.location.search);

  // When an application is waiting on this sign-in, the screen must not
  // navigate anywhere afterwards: AuthorizePage is rendering this form and
  // takes over the moment the session exists. Navigating would replace the
  // URL and lose the request along with it.
  const completingAuthorization =
    params.has("auth_request") ||
    params.has("saml_request") ||
    params.has("cas_service");

  // A CAS client asked for the session to end, and it has.
  const signedOut = params.has("cas_logout");

  // The remembered tenant is a convenience for someone returning to sign
  // in. It must not win over an authorization request, whose tenant is
  // decided by the issuer the application asked: a stale memory would
  // otherwise pre-fill a different tenant, and signing in there succeeds
  // and then fails to complete the request they came for.
  const initialTenant =
    params.get("tenant") ??
    (completingAuthorization ? "" : (tenantStore.get() ?? ""));

  const [tenant, setTenant] = useState(initialTenant);
  // The tenant the lookup below has run for. It trails the field rather than
  // tracking it, because the field changes on every keystroke and typing
  // "acme" would otherwise issue four requests, three of them for tenants
  // that do not exist.
  const [lookedUpTenant, setLookedUpTenant] = useState(initialTenant);
  const [identifier, setIdentifier] = useState("");
  // Set when a sign-in is refused for an unconfirmed address, which is the
  // one refusal this screen can act on.
  const [unverified, setUnverified] = useState(false);
  const [resent, setResent] = useState(false);
  const [resendTo, setResendTo] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [registrationOpen, setRegistrationOpen] = useState(false);
  // Set when the server says the password is right but too old to use. The
  // form then asks for a replacement rather than leaving somebody staring
  // at an error with no way forward — which is what an expiry policy
  // produces if the screen does not know about it.
  const [mustReplacePassword, setMustReplacePassword] = useState(false);
  const [newPassword, setNewPassword] = useState("");
  const [systemName, setSystemName] = useState("Portico");

  // The sign-in screen only offers registration when the server says it is
  // open, so a closed instance does not advertise a dead end. Both that and
  // the name in the header belong to the tenant being signed in to, so the
  // lookup runs again when the tenant settles.
  useEffect(() => {
    tenantStore.set(lookedUpTenant);

    const controller = new AbortController();
    authApi
      .registrationStatus(controller.signal)
      .then((status) => {
        setRegistrationOpen(status.registrationEnabled);
        setSystemName(status.systemName);
      })
      .catch(() => {
        // An unknown tenant lands here. Sign-in will say so plainly, which
        // is more useful than an error under a field they may not have
        // finished filling in — but the header must not keep showing the
        // previous tenant's name as though this one existed.
        setRegistrationOpen(false);
        setSystemName("Portico");
      });

    return () => controller.abort();
  }, [lookedUpTenant]);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      // Submitting without leaving the field never fires onBlur, so settle
      // the lookup here too.
      setLookedUpTenant(tenant.trim());

      if (mustReplacePassword) {
        await signInWithReplacedPassword(
          tenant.trim(),
          identifier,
          password,
          newPassword,
        );
      } else {
        await signIn(tenant.trim(), identifier, password);
      }

      if (!completingAuthorization) {
        // The home screen, for everybody. Sending an administrator straight
        // to the user list skipped the one screen that says what this
        // deployment has in it.
        navigate("/");
      }
    } catch (err) {
      // Not an error to report and stop at: it is a different form. The
      // password field keeps what was typed, because it is still needed as
      // the current password.
      if (err instanceof ApiError && err.code === "PASSWORD_EXPIRED") {
        setMustReplacePassword(true);
        setError("");
        return;
      }
      // Also not a dead end. Somebody who registered and never opened the
      // message needs a way to get another one, and this is the only screen
      // that can offer it — the confirmation link carries a token, not an
      // address, so the page it lands on does not know who to send to.
      if (err instanceof ApiError && err.code === "ACCOUNT_UNVERIFIED") {
        setUnverified(true);
        // Pre-filled only when they signed in with an address, which is the
        // case where we already know the answer. Otherwise it stays empty
        // rather than being seeded with a username that would not work.
        if (identifier.includes("@")) setResendTo(identifier.trim());
      }
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthShell
      title={t("login.title")}
      subtitle={
        // Which tenant is being signed in to, once the server has resolved
        // one. A deployment with only the default tenant never sees this,
        // because it has nothing to disambiguate.
        systemName !== "Portico" ? systemName : undefined
      }
    >
      {expired && (
        <div className="mb-4">
          <Alert tone="danger">{t("login.sessionExpired")}</Alert>
        </div>
      )}

      {signedOut && !expired && (
        <div className="mb-4">
          <Alert tone="success">{t("authorize.signedOut")}</Alert>
        </div>
      )}

      {mustReplacePassword && (
        <div className="mb-4">
          <Alert tone="danger">{t("login.passwordExpired")}</Alert>
        </div>
      )}

      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <Field label={t("login.tenant")} hint={t("login.tenantHint")}>
          <Input
            value={tenant}
            onChange={(e) => setTenant(e.target.value)}
            onBlur={() => setLookedUpTenant(tenant.trim())}
            autoComplete="organization"
            placeholder="default"
          />
        </Field>

        <Field
          label={t("login.identifier")}
          hint={t("login.identifierHint")}
          required
        >
          <Input
            value={identifier}
            onChange={(e) => setIdentifier(e.target.value)}
            autoComplete="username"
            autoFocus
            required
          />
        </Field>

        <Field
          label={
            mustReplacePassword
              ? t("login.currentPassword")
              : t("login.password")
          }
          required
        >
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </Field>

        {mustReplacePassword && (
          <Field label={t("login.newPassword")} required>
            <Input
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              autoComplete="new-password"
              required
            />
          </Field>
        )}

        {error && <Alert tone="danger">{error}</Alert>}

        {unverified && !resent && (
          // The address, asked for rather than taken from the field above.
          //
          // That field holds a username as often as an address, and the
          // server resolves a resend against the contact columns only — so
          // reusing it would produce a button that reports success and
          // sends nothing. Widening the server's lookup instead would mean
          // a username could name the account a message goes to, which is
          // the shape of mistake password recovery deliberately avoids.
          <Field
            label={t("verify.resendAddress")}
            hint={t("verify.resendAddressHelp")}
          >
            <div className="flex gap-2">
              <Input
                type="email"
                value={resendTo}
                onChange={(e) => setResendTo(e.target.value)}
                autoComplete="email"
              />
              <Button
                variant="secondary"
                disabled={resendTo.trim() === ""}
                onClick={() => {
                  // Fired and forgotten on purpose. The endpoint answers
                  // the same thing whether or not the address belongs to
                  // anybody, so there is nothing to report — and reporting
                  // a failure would leak exactly what it refuses to.
                  void authApi
                    .resendVerification(resendTo.trim(), tenant.trim())
                    .catch(() => undefined);
                  setResent(true);
                }}
              >
                {t("verify.resend")}
              </Button>
            </div>
          </Field>
        )}
        {resent && <Alert tone="success">{t("verify.resent")}</Alert>}

        <Button type="submit" disabled={submitting}>
          {submitting ? t("login.signingIn") : t("login.submit")}
        </Button>

        <div className="text-center">
          <AuthLink onClick={() => navigate("/forgot-password")}>
            {t("login.forgotPassword")}
          </AuthLink>
        </div>
      </form>

      {registrationOpen && (
        <p className="mt-5 border-t border-[var(--color-border)] pt-4 text-center text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
          {t("login.noAccount")}{" "}
          <AuthLink onClick={() => navigate("/register")}>
            {t("login.register")}
          </AuthLink>
        </p>
      )}
    </AuthShell>
  );
}
