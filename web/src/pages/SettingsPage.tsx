import { useEffect, useState } from "react";

import { settingsApi } from "../api/endpoints";
import type { Settings } from "../api/types";
import {
  Alert,
  Button,
  Card,
  Field,
  GuidePanel,
  Input,
  PageHeader,
  Select,
} from "../components/ui";
import { locales, useErrorMessage, useT } from "../i18n";

/**
 * The grid this screen's content sits in, named because two things have to
 * agree on it: the cards, and the guide above them. Written out twice, they
 * drift, and the way that shows up is one row ending short of another.
 */
const settingsColumns =
  "grid items-start gap-4 lg:grid-cols-[repeat(2,minmax(0,var(--prose-form-width)))]";

export function SettingsPage() {
  const t = useT();

  const describeError = useErrorMessage();
  const [settings, setSettings] = useState<Settings | null>(null);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    settingsApi
      .get()
      .then(setSettings)
      .catch((err) => setError(describeError(err)));
  }, [describeError]);

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    if (!settings) return;
    setError("");
    setSaved(false);
    setSubmitting(true);
    try {
      setSettings(await settingsApi.update(settings));
      setSaved(true);
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  if (!settings) {
    return (
      <>
        <PageHeader
          title={t("settings.title")}
          subtitle={t("settings.subtitle")}
        />
        {error ? (
          <Alert tone="danger">{error}</Alert>
        ) : (
          <p>{t("common.loading")}</p>
        )}
      </>
    );
  }

  return (
    <>
      <PageHeader
        title={t("settings.title")}
        subtitle={t("settings.subtitle")}
      />

      {/* Laid out in the same grid as the cards below, spanning both columns.

          Not decoration. This screen's content column is narrower than the
          page's — two prose widths rather than the full 1440px — so a guide
          that simply filled the page would end 144px to the right of every
          other row on the screen, which is exactly the raggedness the layout
          suite exists to catch. It caught this. */}
      <div className={settingsColumns}>
        <div className="lg:col-span-2">
          <GuidePanel id="settings" title={t("settings.guideTitle")}>
            {t("settings.guideBody")}
          </GuidePanel>
        </div>
      </div>

      {/* Two columns of cards, not one column of fieldsets.

          Each group keeps the wider of the two form widths: this screen
          argues with the reader about why the defaults are what they are,
          and prose set to the width of a text box is prose nobody finishes.
          Stretching the fields to the full 1440px would make both the
          inputs and the paragraphs worse — what was wrong was never the
          width of the form but that there was only one of them, so most of
          the page sat empty beside it. Same fix the profile screen got: put
          something next to it rather than widening it.

          Still one <form> and one save button. Four cards with four save
          buttons would turn one deliberate act into four chances to leave
          half the screen unsaved. */}
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <div className={settingsColumns}>
          {/* Spanning both columns, which is not decoration.

              The four cards below pair up — basics beside the password policy,
              lockout beside audit — each pair starting on the same line. Five
              cards cannot pair, and the odd one out lands alone on a row half
              the width of every other row, which the browser suite reads as a
              mistake. Correctly: that is exactly what putting this card in the
              left column did. Spanning keeps the count even. */}
          <div className="lg:col-span-2">
            {/* Its own card rather than three more fields under "basics",
                  because the distinction these three need to carry is that they
                  are not the console's session — which sits in that card above,
                  and which everybody conflates with them. Two headings say it
                  where a hint under a fourth field would not. */}
            <Card title={t("settings.oidcTokensLegend")}>
              <fieldset className="flex flex-col gap-4">
                <legend className="sr-only">
                  {t("settings.oidcTokensLegend")}
                </legend>

                <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                  {t("settings.oidcTokensHelp")}
                </p>

                {/* Three numbers side by side rather than stacked. Stacked,
                      each input would be 1296px wide for a two-digit value; in a
                      row they read as what they are — one set of related
                      durations — and the card stays short. */}
                <div className="grid gap-4 lg:grid-cols-3">
                  <Field
                    label={t("settings.oidcAccessTokenTtl")}
                    hint={t("settings.oidcAccessTokenTtlHelp")}
                    required
                  >
                    <Input
                      type="number"
                      min={1}
                      max={60}
                      value={settings.oidcAccessTokenTtlMinutes}
                      onChange={(e) =>
                        setSettings({
                          ...settings,
                          oidcAccessTokenTtlMinutes: Number(e.target.value),
                        })
                      }
                      required
                    />
                  </Field>

                  <Field
                    label={t("settings.oidcRefreshTokenTtl")}
                    hint={t("settings.oidcRefreshTokenTtlHelp")}
                    required
                  >
                    <Input
                      type="number"
                      min={1}
                      max={90}
                      value={settings.oidcRefreshTokenTtlDays}
                      onChange={(e) =>
                        setSettings({
                          ...settings,
                          oidcRefreshTokenTtlDays: Number(e.target.value),
                        })
                      }
                      required
                    />
                  </Field>

                  {/* min is 0, not 1: zero is the off switch, and the only
                        value below the floor that is not a mistake. */}
                  <Field
                    label={t("settings.oidcSessionMaxAge")}
                    hint={t("settings.oidcSessionMaxAgeHelp")}
                  >
                    <Input
                      type="number"
                      min={0}
                      max={365}
                      value={settings.oidcSessionMaxAgeDays}
                      onChange={(e) =>
                        setSettings({
                          ...settings,
                          oidcSessionMaxAgeDays: Number(e.target.value),
                        })
                      }
                    />
                  </Field>
                </div>
              </fieldset>
            </Card>
          </div>

          <div className="flex flex-col gap-4">
            <Card title={t("settings.basicsLegend")}>
              <div className="flex flex-col gap-4">
                <Field label={t("settings.systemName")} required>
                  <Input
                    value={settings.systemName}
                    onChange={(e) =>
                      setSettings({ ...settings, systemName: e.target.value })
                    }
                    required
                  />
                </Field>

                <Field
                  label={t("settings.tokenTtl")}
                  hint={t("settings.tokenTtlHelp")}
                  required
                >
                  <Input
                    type="number"
                    min={5}
                    max={43200}
                    value={settings.tokenTtlMinutes}
                    onChange={(e) =>
                      setSettings({
                        ...settings,
                        tokenTtlMinutes: Number(e.target.value),
                      })
                    }
                    required
                  />
                </Field>

                {/* Not the console's language, which each reader picks for
                    themselves and which is remembered in their browser. This
                    is for the text that arrives where there is no menu — a
                    reset link, a confirmation. */}
                <Field
                  label={t("settings.defaultLocale")}
                  hint={t("settings.defaultLocaleHelp")}
                >
                  <Select
                    value={settings.defaultLocale}
                    onChange={(e) =>
                      setSettings({
                        ...settings,
                        defaultLocale: e.target.value,
                      })
                    }
                  >
                    <option value="">
                      {t("settings.defaultLocaleFollow")}
                    </option>
                    {locales.map((locale) => (
                      <option key={locale.code} value={locale.code}>
                        {locale.name}
                      </option>
                    ))}
                  </Select>
                </Field>

                <label className="flex items-start gap-2.5">
                  <input
                    type="checkbox"
                    className="mt-1"
                    checked={settings.registrationEnabled}
                    onChange={(e) =>
                      setSettings({
                        ...settings,
                        registrationEnabled: e.target.checked,
                      })
                    }
                  />
                  <span>
                    <span className="block font-[weight:var(--font-weight-medium)] text-[var(--color-fg)]">
                      {t("settings.registrationEnabled")}
                    </span>
                    <span className="block text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                      {t("settings.registrationHelp")}
                    </span>
                  </span>
                </label>

                {/* Nested under registration because it is meaningless
                    without it, and shown greyed rather than hidden when
                    registration is off — hiding it would make the setting
                    vanish and reappear as somebody toggles the box above,
                    which reads as a bug. */}
                <label className="ml-6 flex items-start gap-2.5">
                  <input
                    type="checkbox"
                    className="mt-1"
                    disabled={!settings.registrationEnabled}
                    checked={settings.registrationVerification}
                    onChange={(e) =>
                      setSettings({
                        ...settings,
                        registrationVerification: e.target.checked,
                      })
                    }
                  />
                  <span>
                    <span className="block font-[weight:var(--font-weight-medium)] text-[var(--color-fg)]">
                      {t("settings.registrationVerification")}
                    </span>
                    <span className="block text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                      {t("settings.registrationVerificationHelp")}
                    </span>
                  </span>
                </label>
              </div>
            </Card>

            {/* The card carries the heading; the fieldset stays because it
                is what tells a screen reader these controls are one group,
                and a card is a box, not a grouping. Hence the legend, read
                but not shown. */}
            <Card title={t("settings.lockoutLegend")}>
              <fieldset className="flex flex-col gap-4">
                <legend className="sr-only">
                  {t("settings.lockoutLegend")}
                </legend>

                <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                  {t("settings.lockoutHelp")}
                </p>

                <Field
                  label={t("settings.lockoutThreshold")}
                  hint={t("settings.lockoutThresholdHelp")}
                >
                  <Input
                    type="number"
                    min={0}
                    max={100}
                    value={settings.lockoutThreshold}
                    onChange={(e) =>
                      setSettings({
                        ...settings,
                        lockoutThreshold: Number(e.target.value),
                      })
                    }
                  />
                </Field>

                <Field
                  label={t("settings.lockoutDuration")}
                  hint={t("settings.lockoutDurationHelp")}
                >
                  <Input
                    type="number"
                    min={1}
                    max={1440}
                    value={settings.lockoutDurationMinutes}
                    disabled={settings.lockoutThreshold === 0}
                    onChange={(e) =>
                      setSettings({
                        ...settings,
                        lockoutDurationMinutes: Number(e.target.value),
                      })
                    }
                  />
                </Field>
              </fieldset>
            </Card>
          </div>

          <div className="flex flex-col gap-4">
            <Card title={t("settings.passwordLegend")}>
              <fieldset className="flex flex-col gap-4">
                <legend className="sr-only">
                  {t("settings.passwordLegend")}
                </legend>

                {/* Said plainly rather than left for an operator to
                    discover: the composition rules below make passwords more
                    guessable, and are here for auditors rather than for
                    security. */}
                <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                  {t("settings.passwordHelp")}
                </p>

                <Field
                  label={t("settings.passwordMinLength")}
                  hint={t("settings.passwordMinLengthHelp")}
                  required
                >
                  <Input
                    type="number"
                    min={8}
                    max={72}
                    value={settings.passwordMinLength}
                    onChange={(e) =>
                      setSettings({
                        ...settings,
                        passwordMinLength: Number(e.target.value),
                      })
                    }
                    required
                  />
                </Field>

                <div className="flex flex-col gap-2">
                  {(
                    [
                      ["passwordRequireUppercase", "settings.requireUppercase"],
                      ["passwordRequireLowercase", "settings.requireLowercase"],
                      ["passwordRequireDigit", "settings.requireDigit"],
                      ["passwordRequireSymbol", "settings.requireSymbol"],
                    ] as const
                  ).map(([key, labelKey]) => (
                    <label key={key} className="flex items-center gap-2.5">
                      <input
                        type="checkbox"
                        checked={settings[key]}
                        onChange={(e) =>
                          setSettings({ ...settings, [key]: e.target.checked })
                        }
                      />
                      <span className="text-[length:var(--font-size-sm)]">
                        {t(labelKey)}
                      </span>
                    </label>
                  ))}
                </div>

                <Field
                  label={t("settings.passwordHistory")}
                  hint={t("settings.passwordHistoryHelp")}
                >
                  <Input
                    type="number"
                    min={0}
                    max={24}
                    value={settings.passwordHistoryDepth}
                    onChange={(e) =>
                      setSettings({
                        ...settings,
                        passwordHistoryDepth: Number(e.target.value),
                      })
                    }
                  />
                </Field>

                <Field
                  label={t("settings.passwordMaxAge")}
                  hint={t("settings.passwordMaxAgeHelp")}
                >
                  <Input
                    type="number"
                    min={0}
                    max={3650}
                    value={settings.passwordMaxAgeDays}
                    onChange={(e) =>
                      setSettings({
                        ...settings,
                        passwordMaxAgeDays: Number(e.target.value),
                      })
                    }
                  />
                </Field>
              </fieldset>
            </Card>

            <Card title={t("settings.auditLegend")}>
              <Field
                label={t("settings.auditRetention")}
                hint={t("settings.auditRetentionHelp")}
              >
                <Input
                  type="number"
                  min={0}
                  max={3650}
                  value={settings.auditRetentionDays}
                  onChange={(e) =>
                    setSettings({
                      ...settings,
                      auditRetentionDays: Number(e.target.value),
                    })
                  }
                />
              </Field>
            </Card>
          </div>
        </div>

        {/* Below both columns, full width. The save button belongs to the
            whole form, and a button sitting in one column reads as saving
            that column. */}
        {error && <Alert tone="danger">{error}</Alert>}
        {saved && <Alert tone="success">{t("settings.saved")}</Alert>}

        <div>
          <Button type="submit" disabled={submitting}>
            {t("common.save")}
          </Button>
        </div>
      </form>
    </>
  );
}
