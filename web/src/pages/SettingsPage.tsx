import { useEffect, useState } from "react";

import { settingsApi } from "../api/endpoints";
import type { Settings } from "../api/types";
import { Alert, Button, Field, Input, PageHeader } from "../components/ui";
import { useErrorMessage, useT } from "../i18n";

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

      <form onSubmit={handleSubmit} className="flex max-w-md flex-col gap-4">
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

        <fieldset className="flex flex-col gap-4 rounded-[var(--radius-sm)] border border-[var(--color-border)] p-4">
          <legend className="px-1 text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-medium)] text-[var(--color-fg)]">
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

        <fieldset className="flex flex-col gap-4 rounded-[var(--radius-sm)] border border-[var(--color-border)] p-4">
          <legend className="px-1 text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-medium)] text-[var(--color-fg)]">
            {t("settings.passwordLegend")}
          </legend>

          {/* Said plainly rather than left for an operator to discover: the
              composition rules below make passwords more guessable, and are
              here for auditors rather than for security. */}
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
