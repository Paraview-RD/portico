import { useEffect, useState } from "react";

import { settingsApi } from "../api/endpoints";
import type { Branding, FooterLinkMode, Settings } from "../api/types";
import {
  BrandingFooterLinks,
  brandingBackgroundStyle,
  brandingStyle,
} from "../components/branding";
import { BrandLockup } from "../components/brand";
import { BgImageField } from "../components/BgImageField";
import { LogoField } from "../components/LogoField";
import {
  Alert,
  Button,
  Card,
  DocsLink,
  Field,
  Input,
  PageHeader,
  Select,
  Textarea,
} from "../components/ui";
import { useErrorMessage, useT } from "../i18n";

/**
 * The curated font choices. Not a free-text field: the CSP this server sends
 * is font-src 'self', so an external font file (Google Fonts and the like)
 * can never load here regardless of what an administrator types — free text
 * mostly let somebody type something that silently did nothing. These three
 * are stacks this server can actually render, `""` matching the unbranded
 * default already in theme.css's --font-family.
 */
const FONT_FAMILY_SERIF = "Georgia, 'Songti SC', serif";
const FONT_FAMILY_MONO =
  "ui-monospace, 'SFMono-Regular', Menlo, Consolas, 'Courier New', monospace";

/**
 * Its own screen rather than a card on Settings — the same move already
 * made for provisioning and identity providers, once each grew past being
 * one section among many. This one grew a live preview and two long-text
 * fields, which is more than a settings card was built to hold.
 *
 * The load/save pattern is unchanged from SettingsPage: fetch the whole
 * Settings object once, edit the branding fields here, save the whole
 * object back. Not a narrower endpoint of its own — the backend already
 * supports a partial PUT, but every screen that edits settings sends the
 * full object, and a second convention for one screen would be a second
 * thing to remember for no benefit a reader would notice.
 */
export function BrandingPage() {
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
          title={t("branding.title")}
          subtitle={t("branding.subtitle")}
        />
        {error ? (
          <Alert tone="danger">{error}</Alert>
        ) : (
          <p>{t("common.loading")}</p>
        )}
      </>
    );
  }

  // What the real sign-in screen would receive from RegistrationStatus,
  // built from the in-memory form state rather than fetched — the preview
  // updates on every keystroke because it never leaves this component, and
  // it renders through the exact same brandingStyle/BrandingFooterLinks
  // AuthShell uses so it cannot show something a visitor would not see.
  const preview: Branding = {
    logoUrl: settings.brandingLogoUrl,
    productName: settings.brandingProductName,
    colorPrimary: settings.brandingColorPrimary,
    fontFamily: settings.brandingFontFamily,
    bgImageUrl: settings.brandingBgImageUrl,
    footerPrivacyMode: settings.brandingFooterPrivacyMode,
    footerPrivacyUrl: settings.brandingFooterPrivacyUrl,
    footerPrivacyText: settings.brandingFooterPrivacyText,
    footerTermsMode: settings.brandingFooterTermsMode,
    footerTermsUrl: settings.brandingFooterTermsUrl,
    footerTermsText: settings.brandingFooterTermsText,
    footerSupportMode: settings.brandingFooterSupportMode,
    footerSupportUrl: settings.brandingFooterSupportUrl,
    footerSupportText: settings.brandingFooterSupportText,
    loginHeading: settings.brandingLoginHeading,
  };

  return (
    <>
      <PageHeader
        title={t("branding.title")}
        subtitle={t("branding.subtitle")}
        actions={
          <DocsLink
            page="settings/"
            anchor={{ "en-US": "branding", "zh-CN": "品牌定制" }}
          />
        }
      />

      <form
        onSubmit={handleSubmit}
        className="grid items-start gap-4 lg:grid-cols-[minmax(0,var(--prose-form-width))_minmax(0,1fr)]"
      >
        <Card title={t("branding.fieldsLegend")}>
          <div className="flex flex-col gap-4">
            <LogoField
              name={settings.brandingProductName || settings.systemName}
              value={settings.brandingLogoUrl}
              onChange={(brandingLogoUrl) =>
                setSettings({ ...settings, brandingLogoUrl })
              }
            />

            <Field label={t("branding.productName")}>
              <Input
                value={settings.brandingProductName}
                onChange={(e) =>
                  setSettings({
                    ...settings,
                    brandingProductName: e.target.value,
                  })
                }
              />
            </Field>

            <Field
              label={t("branding.loginHeading")}
              hint={t("branding.loginHeadingHelp")}
            >
              <Input
                value={settings.brandingLoginHeading}
                onChange={(e) =>
                  setSettings({
                    ...settings,
                    brandingLoginHeading: e.target.value,
                  })
                }
              />
            </Field>

            <Field
              label={t("branding.colorPrimary")}
              hint={t("branding.colorPrimaryHelp")}
            >
              <div className="flex items-center gap-2">
                <input
                  type="color"
                  // A native colour picker beside the text field, not
                  // instead of it: the text field is what round-trips
                  // through the API and what a copy-pasted brand hex lands
                  // in, and the picker is a convenience for choosing one
                  // from scratch.
                  value={settings.brandingColorPrimary || "#2563eb"}
                  onChange={(e) =>
                    setSettings({
                      ...settings,
                      brandingColorPrimary: e.target.value,
                    })
                  }
                  className="h-9 w-9 shrink-0 cursor-pointer rounded-[var(--radius-sm)] border border-[var(--color-border)]"
                />
                <Input
                  value={settings.brandingColorPrimary}
                  placeholder="#2563eb"
                  onChange={(e) =>
                    setSettings({
                      ...settings,
                      brandingColorPrimary: e.target.value,
                    })
                  }
                />
              </div>
            </Field>

            <Field
              label={t("branding.fontFamily")}
              hint={t("branding.fontFamilyHelp")}
            >
              <Select
                value={settings.brandingFontFamily}
                onChange={(e) =>
                  setSettings({
                    ...settings,
                    brandingFontFamily: e.target.value,
                  })
                }
              >
                <option value="">
                  {t("branding.fontFamilyOptionDefault")}
                </option>
                <option value={FONT_FAMILY_SERIF}>
                  {t("branding.fontFamilyOptionSerif")}
                </option>
                <option value={FONT_FAMILY_MONO}>
                  {t("branding.fontFamilyOptionMono")}
                </option>
              </Select>
            </Field>

            <BgImageField
              value={settings.brandingBgImageUrl}
              onChange={(brandingBgImageUrl) =>
                setSettings({ ...settings, brandingBgImageUrl })
              }
            />

            <FooterLinkFields
              legend={t("branding.footerPrivacy")}
              mode={settings.brandingFooterPrivacyMode}
              url={settings.brandingFooterPrivacyUrl}
              text={settings.brandingFooterPrivacyText}
              urlPlaceholder="https://example.com/privacy"
              onChange={(mode, url, text) =>
                setSettings({
                  ...settings,
                  brandingFooterPrivacyMode: mode,
                  brandingFooterPrivacyUrl: url,
                  brandingFooterPrivacyText: text,
                })
              }
            />

            <FooterLinkFields
              legend={t("branding.footerTerms")}
              mode={settings.brandingFooterTermsMode}
              url={settings.brandingFooterTermsUrl}
              text={settings.brandingFooterTermsText}
              urlPlaceholder="https://example.com/terms"
              onChange={(mode, url, text) =>
                setSettings({
                  ...settings,
                  brandingFooterTermsMode: mode,
                  brandingFooterTermsUrl: url,
                  brandingFooterTermsText: text,
                })
              }
            />

            <FooterLinkFields
              legend={t("branding.footerSupport")}
              mode={settings.brandingFooterSupportMode}
              url={settings.brandingFooterSupportUrl}
              text={settings.brandingFooterSupportText}
              urlPlaceholder="mailto:support@example.com"
              onChange={(mode, url, text) =>
                setSettings({
                  ...settings,
                  brandingFooterSupportMode: mode,
                  brandingFooterSupportUrl: url,
                  brandingFooterSupportText: text,
                })
              }
            />
          </div>

          {error && (
            <div className="mt-4">
              <Alert tone="danger">{error}</Alert>
            </div>
          )}
          {saved && (
            <div className="mt-4">
              <Alert tone="success">{t("settings.saved")}</Alert>
            </div>
          )}

          <div className="mt-4">
            <Button type="submit" disabled={submitting}>
              {t("common.save")}
            </Button>
          </div>
        </Card>

        {/* Non-interactive, and deliberately not a live iframe of the real
            sign-in screen: it would need a second, unauthenticated render
            path just to host inside an authenticated page, for a result
            indistinguishable from rendering the same pieces directly. */}
        <div className="lg:sticky lg:top-4">
          <Card title={t("branding.previewLegend")}>
            <p className="mb-4 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
              {t("branding.previewHelp")}
            </p>
            <div
              className="rounded-[var(--radius-lg)] bg-[var(--color-bg-soft)] p-4"
              style={brandingBackgroundStyle(preview)}
            >
              <div
                className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-bg)] p-6 shadow-[var(--shadow-sm)]"
                style={brandingStyle(preview)}
              >
                <div className="mb-5 flex items-center justify-between">
                  <BrandLockup
                    name={preview.productName || "Portico"}
                    size={32}
                    logoSrc={preview.logoUrl || undefined}
                  />
                </div>
                <h1 className="text-[length:var(--font-size-lg)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
                  {preview.loginHeading || t("login.title")}
                </h1>
                <div className="mt-5 flex flex-col gap-3">
                  <div className="h-9 rounded-[var(--radius-sm)] border border-[var(--color-border)]" />
                  <div className="h-9 rounded-[var(--radius-sm)] border border-[var(--color-border)]" />
                  <div className="h-9 rounded-[var(--radius-sm)] bg-[var(--color-primary)]" />
                </div>
                <BrandingFooterLinks branding={preview} />
              </div>
            </div>
          </Card>
        </div>
      </form>
    </>
  );
}

/**
 * One footer link's mode selector plus whichever field the mode uses.
 * Shared by all three slots so the three cannot drift the way three
 * independent copies would.
 */
function FooterLinkFields({
  legend,
  mode,
  url,
  text,
  urlPlaceholder,
  onChange,
}: {
  legend: string;
  mode: FooterLinkMode;
  url: string;
  text: string;
  urlPlaceholder: string;
  onChange: (mode: FooterLinkMode, url: string, text: string) => void;
}) {
  const t = useT();

  return (
    <fieldset className="flex flex-col gap-2 border-t border-[var(--color-border)] pt-4">
      <legend className="mb-2 text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
        {legend}
      </legend>

      <Field label={t("branding.footerMode")}>
        <Select
          value={mode}
          onChange={(e) =>
            onChange(e.target.value as FooterLinkMode, url, text)
          }
        >
          <option value="">{t("branding.footerModeOff")}</option>
          <option value="link">{t("branding.footerModeLink")}</option>
          <option value="text">{t("branding.footerModeText")}</option>
        </Select>
      </Field>

      {mode === "link" && (
        <Field
          label={t("branding.footerLinkAddress")}
          hint={t("branding.footerLinkHelp")}
        >
          <Input
            value={url}
            placeholder={urlPlaceholder}
            onChange={(e) => onChange(mode, e.target.value, text)}
          />
        </Field>
      )}

      {mode === "text" && (
        <Field
          label={t("branding.footerLinkText")}
          hint={t("branding.footerTextHelp")}
        >
          <Textarea
            rows={5}
            value={text}
            onChange={(e) => onChange(mode, url, e.target.value)}
          />
        </Field>
      )}
    </fieldset>
  );
}
