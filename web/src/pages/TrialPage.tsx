import { useState } from "react";

import { trialApi } from "../api/endpoints";
import { AuthShell } from "../components/AuthShell";
import { Alert, Button, Field, Input, Select } from "../components/ui";
import { useErrorMessage, useT } from "../i18n";
import { useRouter } from "../router";

/**
 * The form a visitor with no account fills in to get a tenant.
 *
 * Only reachable on a deployment that turned trials on — the entry point that
 * leads here is drawn from the same status call, and the endpoints behind it
 * are not routed otherwise. Somebody who types this address on an ordinary
 * installation gets the form and a refusal on submit, which is the honest
 * order: the alternative is checking on mount and showing a screen that
 * flickers into an error.
 *
 * Two things are said out loud rather than left to be discovered. The password
 * arrives by email, so a typo in the address is the one mistake that cannot be
 * recovered from here. And the tenant code cannot be changed afterwards,
 * because it is what everybody in that tenant signs in with.
 */
export function TrialPage() {
  const t = useT();
  const describeError = useErrorMessage();
  const { navigate } = useRouter();

  const [email, setEmail] = useState("");
  const [companyName, setCompanyName] = useState("");
  const [tenantCode, setTenantCode] = useState("");
  const [industry, setIndustry] = useState("generic");

  const [submitting, setSubmitting] = useState(false);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      await trialApi.requestTrial({ email, companyName, tenantCode, industry });
      setSent(true);
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  if (sent) {
    return (
      <AuthShell title={t("trial.sentTitle")} subtitle={t("trial.sentBody")}>
        {/* The address is repeated because it is the one thing they cannot
            fix from here: a link sent to a mistyped address is gone, and
            reading it back is how somebody notices. */}
        <Alert tone="success">{t("trial.sentTo", email)}</Alert>
        <Button variant="secondary" onClick={() => navigate("/login")}>
          {t("trial.backToSignIn")}
        </Button>
      </AuthShell>
    );
  }

  return (
    <AuthShell title={t("trial.title")} subtitle={t("trial.subtitle")}>
      <form className="flex flex-col gap-4" onSubmit={(e) => void submit(e)}>
        {error && <Alert tone="danger">{error}</Alert>}

        <Field label={t("trial.email")} hint={t("trial.emailHint")} required>
          <Input
            type="email"
            value={email}
            required
            autoComplete="email"
            onChange={(e) => setEmail(e.target.value)}
          />
        </Field>

        <Field label={t("trial.company")} required>
          <Input
            value={companyName}
            required
            onChange={(e) => setCompanyName(e.target.value)}
          />
        </Field>

        <Field
          label={t("trial.tenantCode")}
          hint={t("trial.tenantCodeHint")}
          required
        >
          <Input
            value={tenantCode}
            required
            // Lower-cased as it is typed rather than silently on the server,
            // so what somebody sees here is what they will type at sign-in.
            onChange={(e) => setTenantCode(e.target.value.toLowerCase())}
          />
        </Field>

        <Field label={t("trial.industry")} hint={t("trial.industryHint")}>
          <Select
            value={industry}
            onChange={(e) => setIndustry(e.target.value)}
          >
            <option value="generic">{t("trial.industry.generic")}</option>
          </Select>
        </Field>

        <Alert tone="warning">{t("trial.notReal")}</Alert>

        <Button type="submit" disabled={submitting}>
          {submitting ? t("trial.submitting") : t("trial.submit")}
        </Button>
        <Button variant="ghost" onClick={() => navigate("/login")}>
          {t("trial.backToSignIn")}
        </Button>
      </form>
    </AuthShell>
  );
}
