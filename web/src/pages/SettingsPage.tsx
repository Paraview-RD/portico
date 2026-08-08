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
