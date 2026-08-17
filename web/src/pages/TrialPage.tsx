import { useEffect, useState } from "react";

import { trialApi } from "../api/endpoints";
import { AuthShell } from "../components/AuthShell";
import { Alert, Button, Field, Input, Select } from "../components/ui";
import { useErrorMessage, useLanguage, useT } from "../i18n";
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
/**
 * The message key for each world the server can seed.
 *
 * Written out rather than built as `trial.industry.${key}`, which does not
 * type-check: message keys are a union, and a template string is not a member
 * of it. That is the point rather than an obstacle — the server decides which
 * industries exist and this file decides what they are called, and the two are
 * connected by nothing the compiler can see. Spelled out here, adding a pack
 * without translating it is caught twice: by internal/demo's locale test, and
 * by the fallback below being visible in the picker.
 */
const industryKeys = {
  generic: "trial.industry.generic",
  manufacturing: "trial.industry.manufacturing",
  banking: "trial.industry.banking",
  hospital: "trial.industry.hospital",
  university: "trial.industry.university",
} as const;

/**
 * The three steps of the diagram, in order.
 *
 * Spelled out for the same reason as the industries above, and with more cause:
 * these used to be built as `trial.step.${step}` with a cast onto one of the
 * keys to make it compile. A cast is not a check — renaming a step would have
 * left the type green and the page rendering `trial.step.fill` at somebody.
 */
const stepKeys = [
  "trial.step.fill",
  "trial.step.confirm",
  "trial.step.ready",
] as const;

function industryLabel(t: ReturnType<typeof useT>, industry: string) {
  const key = industryKeys[industry as keyof typeof industryKeys];
  // An untranslated industry shows its own key, which is ugly and honest. The
  // alternative — hiding it — would leave a pack the server offers missing
  // from the form with nothing to explain why.
  return key ? t(key) : industry;
}

export function TrialPage() {
  const t = useT();
  const { language } = useLanguage();
  const describeError = useErrorMessage();
  const { navigate } = useRouter();

  const [email, setEmail] = useState("");
  const [tenantCode, setTenantCode] = useState("");
  const [industry, setIndustry] = useState("generic");

  const [submitting, setSubmitting] = useState(false);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState("");

  // The worlds on offer, asked for rather than listed here.
  //
  // Starts as the generic one so the picker is never empty, including on the
  // deployment where this endpoint answers 404. A failure is deliberately
  // silent: it costs the visitor four choices they may not have wanted, and
  // the alternative — an error above a form that still works — is worse.
  const [industries, setIndustries] = useState<string[]>(["generic"]);

  useEffect(() => {
    const cancel = new AbortController();
    void trialApi
      .trialStatus(cancel.signal)
      .then((status) => {
        if (status.industries.length > 0) setIndustries(status.industries);
      })
      .catch(() => {});
    return () => cancel.abort();
  }, []);

  // What the server will say about this code, said before it is asked.
  //
  // The rule is service.validateTenantCode's: lowercase letters, digits,
  // hyphens and underscores. Reported rather than enforced by rewriting what
  // was typed — the field already lower-cases silently, which is a change
  // somebody can see the sense of, and silently deleting a character they
  // meant to type is not the same thing.
  const codeError =
    tenantCode !== "" && !/^[a-z0-9_-]+$/.test(tenantCode)
      ? t("trial.tenantCodeInvalid")
      : "";

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      // No organization name. The field is gone from this form and the server
      // names the tenant after its code when none arrives, so sending an empty
      // string would be describing a decision that was already made.
      await trialApi.requestTrial({
        email,
        tenantCode,
        industry,
        locale: language,
      });
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
        <div className="flex flex-col gap-3">
          {/* The address is repeated because it is the one thing they cannot
              fix from here: a link sent to a mistyped address is gone, and
              reading it back is how somebody notices. */}
          <Alert tone="success">{t("trial.sentTo", email)}</Alert>
          {/* The way back, for the one mistake the hint on that field warns
              about. Until this existed, a visitor who read their own address
              back and saw it was wrong had nowhere to go but the sign-in
              screen and no way to return but retyping the whole form — the
              state is still here, so this returns them to it filled in. */}
          <Button variant="secondary" onClick={() => setSent(false)}>
            {t("trial.sentEdit")}
          </Button>
          <Button variant="ghost" onClick={() => navigate("/login")}>
            {t("trial.backToSignIn")}
          </Button>
        </div>
      </AuthShell>
    );
  }

  return (
    <AuthShell title={t("trial.title")} subtitle={t("trial.subtitle")} wide>
      {/* What is about to happen, before asking for anything.

          Drawn across rather than listed down. The same three steps as a
          stacked list of full sentences read as one more paragraph of small
          grey text above a form — which is what people skip. Three captions on
          a line, each a few words long, are a shape before they are words, and
          the shape says "three steps, you are at the first" without being
          read.

          The rule that costs somebody their tenant does not fit in a caption,
          so it is said once underneath: nothing is created until the link is
          opened. Somebody who does not know that reads the confirmation email
          as a receipt, closes it, and never finds out why the tenant they
          asked for does not exist. */}
      <div className="mb-6">
        <ol className="flex flex-col gap-3 sm:flex-row sm:gap-2">
          {stepKeys.map((key, index) => (
            <li key={key} className="flex items-center gap-3 sm:flex-1">
              <span
                aria-hidden="true"
                className="flex size-7 shrink-0 items-center justify-center rounded-full bg-[var(--color-primary-soft)] text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-bold)] text-[var(--color-primary)]"
              >
                {index + 1}
              </span>
              <span className="text-[length:var(--font-size-sm)] text-[var(--color-fg)]">
                {t(key)}
              </span>
              {/* The thread between the steps, and only between them: a rule
                  after the last one would point at nothing. Hidden while the
                  steps are stacked, where a horizontal line joins neither. */}
              {index < stepKeys.length - 1 && (
                <span
                  aria-hidden="true"
                  className="hidden h-px flex-1 bg-[var(--color-border)] sm:block"
                />
              )}
            </li>
          ))}
        </ol>
        <p className="mt-3 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
          {t("trial.stepNote")}
        </p>
      </div>

      <form className="flex flex-col gap-4" onSubmit={(e) => void submit(e)}>
        {error && <Alert tone="danger">{error}</Alert>}

        <Field label={t("trial.email")} hint={t("trial.emailHint")} required>
          <Input
            type="email"
            value={email}
            required
            // The page exists to be filled in, and this is the first thing to
            // fill in. Safe here in a way it would not be on a screen with
            // something to read first: there is nothing above it to scroll
            // past.
            autoFocus
            autoComplete="email"
            onChange={(e) => setEmail(e.target.value)}
          />
        </Field>

        <Field
          label={t("trial.tenantCode")}
          hint={t("trial.tenantCodeHint")}
          required
          error={codeError}
        >
          <Input
            value={tenantCode}
            required
            // The same bounds the server enforces. Not a substitute for it —
            // the check that counts is in service.validateTenantCode — but a
            // one-character code refused after a round trip is a refusal that
            // could have been a hint.
            minLength={2}
            maxLength={64}
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
            {industries.map((key) => (
              <option key={key} value={key}>
                {industryLabel(t, key)}
              </option>
            ))}
          </Select>
        </Field>

        <Alert tone="warning">{t("trial.notReal")}</Alert>

        <Button type="submit" disabled={submitting || codeError !== ""}>
          {submitting ? t("trial.submitting") : t("trial.submit")}
        </Button>
        <Button variant="ghost" onClick={() => navigate("/login")}>
          {t("trial.backToSignIn")}
        </Button>
      </form>
    </AuthShell>
  );
}
