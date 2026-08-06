import { useCallback, useEffect, useState } from "react";

import { ApiError } from "../api/client";
import { auditApi } from "../api/endpoints";
import type { AuditLog, LogKind } from "../api/types";
import {
  Alert,
  Badge,
  EmptyRow,
  Input,
  PageHeader,
  Pagination,
  Select,
  Table,
  Td,
  Th,
} from "../components/ui";
import { useT } from "../i18n";

const PAGE_SIZE = 20;
const kinds: LogKind[] = [
  "LOGIN",
  "OPERATION",
  "AUTH",
  "REGISTRATION",
  "ORGANIZATION",
];

/** Converts a datetime-local value to the RFC 3339 the API expects. */
function toRFC3339(localValue: string): string {
  if (!localValue) return "";
  const parsed = new Date(localValue);
  return Number.isNaN(parsed.getTime()) ? "" : parsed.toISOString();
}

export function AuditLogsPage() {
  const t = useT();

  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [kind, setKind] = useState<LogKind | "">("");
  const [keyword, setKeyword] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await auditApi.list({
        page,
        pageSize: PAGE_SIZE,
        kind,
        keyword,
        from: toRFC3339(from),
        to: toRFC3339(to),
      });
      setLogs(result.items);
      setTotal(result.total);
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : t("common.unexpectedError"),
      );
    } finally {
      setLoading(false);
    }
  }, [page, kind, keyword, from, to, t]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <>
      <PageHeader
        title={t("auditLogs.title")}
        subtitle={t("auditLogs.subtitle")}
      />

      <div className="mb-4 flex flex-wrap items-end gap-3">
        <div className="w-64">
          <Input
            placeholder={t("auditLogs.searchPlaceholder")}
            value={keyword}
            onChange={(e) => {
              setKeyword(e.target.value);
              setPage(1);
            }}
          />
        </div>

        <div className="w-48">
          <Select
            value={kind}
            onChange={(e) => {
              setKind(e.target.value as LogKind | "");
              setPage(1);
            }}
          >
            <option value="">
              {t("auditLogs.filterKind")}: {t("common.all")}
            </option>
            {kinds.map((k) => (
              <option key={k} value={k}>
                {t(`auditLogs.kind.${k}`)}
              </option>
            ))}
          </Select>
        </div>

        <label className="flex w-52 flex-col gap-1">
          <span className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {t("auditLogs.filterFrom")}
          </span>
          <Input
            type="datetime-local"
            value={from}
            onChange={(e) => {
              setFrom(e.target.value);
              setPage(1);
            }}
          />
        </label>

        <label className="flex w-52 flex-col gap-1">
          <span className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {t("auditLogs.filterTo")}
          </span>
          <Input
            type="datetime-local"
            value={to}
            onChange={(e) => {
              setTo(e.target.value);
              setPage(1);
            }}
          />
        </label>
      </div>

      {error && (
        <div className="mb-4">
          <Alert tone="danger">{error}</Alert>
        </div>
      )}

      <Table>
        <thead>
          <tr>
            <Th>{t("auditLogs.colTime")}</Th>
            <Th>{t("auditLogs.colKind")}</Th>
            <Th>{t("auditLogs.colAction")}</Th>
            <Th>{t("auditLogs.colActor")}</Th>
            <Th>{t("auditLogs.colTarget")}</Th>
            <Th>{t("auditLogs.colResult")}</Th>
            <Th>{t("auditLogs.colIp")}</Th>
          </tr>
        </thead>
        <tbody>
          {loading ? (
            <tr>
              <Td className="py-10 text-center">{t("common.loading")}</Td>
            </tr>
          ) : logs.length === 0 ? (
            <EmptyRow colSpan={7} />
          ) : (
            logs.map((log) => (
              <tr key={log.id}>
                <Td className="whitespace-nowrap">
                  {new Date(log.createdAt).toLocaleString()}
                </Td>
                <Td>{t(`auditLogs.kind.${log.kind}`)}</Td>
                <Td>
                  <code className="text-[length:var(--font-size-sm)]">
                    {log.action}
                  </code>
                </Td>
                <Td>{log.actorName || "—"}</Td>
                <Td>{log.targetName || "—"}</Td>
                <Td>
                  <Badge tone={log.result === "SUCCESS" ? "success" : "danger"}>
                    {t(`auditLogs.result.${log.result}`)}
                  </Badge>
                </Td>
                <Td>
                  <span className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                    {log.ip || "—"}
                  </span>
                </Td>
              </tr>
            ))
          )}
        </tbody>
      </Table>

      <Pagination
        page={page}
        pageSize={PAGE_SIZE}
        total={total}
        onChange={setPage}
      />
    </>
  );
}
