/**
 * The picture on an application's tile: upload one, or type a path.
 *
 * One component behind three registration forms. The three protocols disagree
 * about nearly everything and not about this — a tile is a tile — and the field
 * was previously copied into each of them, which is how the three drifted apart
 * the first time somebody improved one of them.
 *
 * Both ways in, deliberately. Uploading is what somebody registering an
 * application through a browser needs, and it did not exist. Typing a path is
 * what a deployment that ships its own icons under /icons already relies on,
 * and it is the only way to name a file this server did not store — so removing
 * it to make room for the upload would have taken away the case the column was
 * originally designed for.
 */

import { useRef, useState } from "react";

import { applicationApi } from "../api/endpoints";
import { useErrorMessage, useT } from "../i18n";
import { AppIcon, Button, Field, Input } from "./ui";

export function LogoField({
  name,
  value,
  onChange,
}: {
  /** The application's name, for the lettered fallback tile. */
  name: string;
  value: string;
  onChange: (logoUri: string) => void;
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
      const { path } = await applicationApi.uploadLogo(file);
      // The server answers with the path to reference the stored file by, so
      // the upload and the typed-in case converge on one value: whatever ends
      // up in this field is what gets saved, either way.
      onChange(path);
    } catch (err) {
      setError(describeError(err));
    } finally {
      setUploading(false);
      // Cleared so that picking the same file again re-fires the change event.
      // Without this, retrying after a failure appears to do nothing.
      if (fileInput.current) {
        fileInput.current.value = "";
      }
    }
  }

  return (
    <Field
      label={`${t("applications.logoUri")} (${t("common.optional")})`}
      hint={t("applications.logoUriHelp")}
      error={error}
    >
      <div className="flex items-center gap-3">
        <AppIcon name={name || "?"} src={value} size={36} />
        <Input
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="/icons/wiki.svg"
        />
        {/* Hidden and driven by the button beside it: a bare file input cannot
            be styled to match anything, and its label reads as whatever the
            browser's locale says rather than as this console's language. */}
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
