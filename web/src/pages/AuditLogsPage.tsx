import { useCallback, useEffect, useState } from "react";

import { auditApi } from "../api/endpoints";
import type { AuditLog, LogKind } from "../api/types";
import {
  Alert,
  Badge,
  EmptyRow,
  LoadingRow,
  Input,
  PageHeader,
  Pagination,
  Select,
  Table,
  Td,
  Th,
} from "../components/ui";
import { useErrorMessage, useT } from "../i18n";

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

  const describeError = useErrorMessage();
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [kind, setKind] = useState<LogKind | "">("");
  const [keyword, setKeyword] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");

  // Which rows have their detail open. A set rather than a single id: an
  // auditor comparing two entries — "did this registration permit the same
  // redirect URIs as that one" — needs both on screen at once.
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set());

  const toggleExpanded = useCallback((id: string) => {
    setExpanded((current) => {
      const next = new Set(current);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  }, []);

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
      setError(describeError(err));
    } finally {
      setLoading(false);
    }
  }, [page, kind, keyword, from, to, describeError]);

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
            <Th>{t("auditLogs.colDetail")}</Th>
          </tr>
        </thead>
        <tbody>
          {loading ? (
            <LoadingRow colSpan={8} />
          ) : logs.length === 0 ? (
            <EmptyRow colSpan={8} />
          ) : (
            logs.map((log) => (
              <AuditRow
                key={log.id}
                log={log}
                expanded={expanded.has(log.id)}
                onToggle={() => toggleExpanded(log.id)}
              />
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

/**
 * One entry, with its detail behind a toggle.
 *
 * The detail is where the answer usually is. An entry saying a relying party
 * was registered is not much use on its own; the redirect URIs it was
 * permitted are the thing an auditor compares against what was expected, and
 * the server has always recorded them. It just was not shown.
 *
 * It is behind a toggle rather than in a column because it is a sentence,
 * not a value: in a column it would either wrap the table into unreadability
 * or be truncated to the point of being decorative.
 */
function AuditRow({
  log,
  expanded,
  onToggle,
}: {
  log: AuditLog;
  expanded: boolean;
  onToggle: () => void;
}) {
  const t = useT();

  // Rows carrying nothing beyond what the columns already show get no
  // toggle. A control that opens an empty panel teaches people the control
  // does nothing, and they then stop using it where it would have helped.
  const hasDetail =
    log.detail !== "" || log.targetType !== "" || log.targetId !== "";
  const detailID = `audit-detail-${log.id}`;

  return (
    <>
      <tr>
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
        <Td>
          {hasDetail ? (
            <button
              type="button"
              onClick={onToggle}
              aria-expanded={expanded}
              aria-controls={detailID}
              className="text-[length:var(--font-size-sm)] text-[var(--color-primary)] hover:underline"
            >
              {expanded ? t("auditLogs.hideDetail") : t("auditLogs.showDetail")}
            </button>
          ) : (
            <span className="text-[var(--color-fg-muted)]">—</span>
          )}
        </Td>
      </tr>

      {hasDetail && expanded && (
        <tr id={detailID}>
          <Td colSpan={8} className="bg-[var(--color-bg-soft)]">
            <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1.5 text-[length:var(--font-size-sm)]">
              {log.detail && (
                <>
                  <dt className="text-[var(--color-fg-muted)]">
                    {t("auditLogs.detail")}
                  </dt>
                  <dd className="break-all">{log.detail}</dd>
                </>
              )}
              {log.targetType && (
                <>
                  <dt className="text-[var(--color-fg-muted)]">
                    {t("auditLogs.targetType")}
                  </dt>
                  <dd>
                    <code>{log.targetType}</code>
                  </dd>
                </>
              )}
              {log.targetId && (
                <>
                  <dt className="text-[var(--color-fg-muted)]">
                    {t("auditLogs.targetId")}
                  </dt>
                  <dd className="break-all">
                    <code>{log.targetId}</code>
                  </dd>
                </>
              )}
              {log.actorId && (
                <>
                  <dt className="text-[var(--color-fg-muted)]">
                    {t("auditLogs.actorId")}
                  </dt>
                  <dd className="break-all">
                    <code>{log.actorId}</code>
                  </dd>
                </>
              )}
            </dl>
          </Td>
        </tr>
      )}
    </>
  );
}
