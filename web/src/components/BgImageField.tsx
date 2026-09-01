/**
 * The branding background image: upload one, or type an address.
 *
 * Structured like LogoField (web/src/components/LogoField.tsx) — upload
 * button plus a text field, converging on the same value either way — minus
 * the small square preview LogoField shows beside itself. There is no
 * equivalent need here: the branding page's own live preview card already
 * renders the background at full size from this same field, so a second,
 * smaller preview beside the input would just be a second, worse one.
 */

import { useRef, useState } from "react";

import { settingsApi } from "../api/endpoints";
import { useErrorMessage, useT } from "../i18n";
import { Button, Field, Input } from "./ui";

export function BgImageField({
  value,
  onChange,
}: {
  value: string;
  onChange: (bgImageUrl: string) => void;
}) {
  const t = useT();
  const describeError = useErrorMessage();

  const fileInput = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState("");

  async function handleFile(file: File) {
    setError("");
    setUploading(true);
    try {
      const { path } = await settingsApi.uploadBgImage(file);
      // Same convergence as LogoField: whatever ends up in this field is
      // what gets saved, whether it was typed or uploaded.
      onChange(path);
    } catch (err) {
      setError(describeError(err));
    } finally {
      setUploading(false);
      // Cleared so that picking the same file again re-fires the change
      // event. Without this, retrying after a failure appears to do nothing.
      if (fileInput.current) {
        fileInput.current.value = "";
      }
    }
  }

  return (
    <Field
      label={t("branding.bgImageUrl")}
      hint={t("branding.bgImageUrlHelp")}
      error={error}
    >
      <div className="flex items-center gap-3">
        <Input
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="https://example.com/background.jpg"
        />
        {/* Hidden and driven by the button beside it, same reasoning as
            LogoField: a bare file input cannot be styled to match anything,
            and its label reads as whatever the browser's locale says rather
            than as this console's language. */}
        <input
          ref={fileInput}
          type="file"
          accept="image/png,image/jpeg"
          className="hidden"
          onChange={(e) => {
            const file = e.target.files?.[0];
            if (file) void handleFile(file);
          }}
        />
        <Button
          type="button"
          variant="secondary"
          disabled={uploading}
          onClick={() => fileInput.current?.click()}
        >
          {uploading
            ? t("applications.logoUploading")
            : t("applications.logoUpload")}
        </Button>
      </div>
    </Field>
  );
}
