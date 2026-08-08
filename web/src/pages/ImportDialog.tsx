import { useEffect, useRef, useState } from "react";

import { userApi } from "../api/endpoints";
import type { ImportResult } from "../api/types";
import { Alert, Button, Modal, Table, Td, Th } from "../components/ui";
import { useErrorMessage, useT } from "../i18n";

/**
 * Bulk import.
 *
 * The per-row error report is the point of this screen: the server imports
 * what it can and returns exactly which rows failed and why, so the report
 * is rendered in full rather than collapsed into "some rows failed".
 */
export function ImportDialog({
  open,
  onClose,
  onImported,
}: {
  open: boolean;
  onClose: () => void;
  onImported: () => void;
}) {
  const t = useT();
  const describeError = useErrorMessage();
  const fileInput = useRef<HTMLInputElement>(null);

  const [file, setFile] = useState<File | null>(null);
  const [result, setResult] = useState<ImportResult | null>(null);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setFile(null);
    setResult(null);
    setError("");
  }, [open]);

  async function handleImport() {
    if (!file) return;
    setError("");
    setSubmitting(true);
    try {
      const imported = await userApi.importUsers(file);
      setResult(imported);
      // Refresh the list even on a partial import — some rows did land.
      if (imported.imported > 0) {
        onImported();
      }
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Modal
      open={open}
      title={t("users.importTitle")}
      onClose={onClose}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {result ? t("common.close") : t("common.cancel")}
          </Button>
          {!result && (
            <Button
              onClick={() => void handleImport()}
              disabled={!file || submitting}
            >
              {submitting ? t("users.importing") : t("users.importSubmit")}
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <p className="text-[var(--color-fg-muted)]">{t("users.importHelp")}</p>

        {!result && (
          <>
            <div>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void userApi.downloadTemplate()}
              >
                {t("users.importDownloadTemplate")}
              </Button>
            </div>

            <div className="flex items-center gap-3">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => fileInput.current?.click()}
              >
                {t("users.importChooseFile")}
              </Button>
              <span className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                {file?.name}
              </span>
              <input
                ref={fileInput}
                type="file"
                accept=".xlsx"
                className="hidden"
                onChange={(e) => setFile(e.target.files?.[0] ?? null)}
              />
            </div>
          </>
        )}

        {error && <Alert tone="danger">{error}</Alert>}

        {result && (
          <div className="flex flex-col gap-3">
            <Alert tone={result.failed === 0 ? "success" : "danger"}>
              {t(
                "users.importSummary",
                result.imported,
                result.total,
                result.failed,
              )}
            </Alert>

            {result.errors.length > 0 && (
              <Table>
                <thead>
                  <tr>
                    <Th>{t("users.importColRow")}</Th>
                    <Th>{t("users.importColUsername")}</Th>
                    <Th>{t("users.importColProblem")}</Th>
                  </tr>
                </thead>
                <tbody>
                  {result.errors.map((rowError) => (
                    <tr key={rowError.row}>
                      <Td>{rowError.row}</Td>
                      <Td>{rowError.username || "—"}</Td>
                      <Td>{rowError.message}</Td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </div>
        )}
      </div>
    </Modal>
  );
}
