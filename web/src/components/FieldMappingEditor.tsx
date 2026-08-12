import { useCallback, useEffect, useState } from "react";

import { fieldMappingsApi, fieldsApi } from "../api/endpoints";
import type { CatalogueField, FieldMapping, RecipientKind } from "../api/types";
import { useErrorMessage, useT } from "../i18n";
import type { TranslationKey } from "../i18n/en-US";
import { Alert, Badge, Button, Input, Modal } from "./ui";

/**
 * What one recipient receives, and under what name.
 *
 * The same editor for all four kinds, because the rules are the same rules —
 * only the path differs, and that is a prop. Four copies of this form would
 * drift the way the three registration forms did.
 *
 * The screen shows the whole catalogue rather than only what is configured.
 * A picker that lists nothing until you add something answers "what can I
 * send?" with silence, and that question is the one somebody arrives with.
 */

/** The order the sections are drawn in, which is the order of the catalogue. */
const groupOrder: CatalogueField["group"][] = [
  "identity",
  "profile",
  "organization",
  "tenant",
  "custom",
];

/** One field's state in the form. */
interface RuleState {
  targetName: string;
  suppressed: boolean;
}

/**
 * A row is configured when it says something. An empty target and no
 * suppression is a row somebody looked at and left alone, and it is not sent.
 */
function configured(rule: RuleState | undefined): boolean {
  if (!rule) return false;
  return rule.suppressed || rule.targetName.trim() !== "";
}

export function FieldMappingEditor({
  kind,
  recipientId,
  recipientName,
  onClose,
}: {
  kind: RecipientKind;
  recipientId: string;
  recipientName: string;
  onClose: () => void;
}) {
  const t = useT();
  const describeError = useErrorMessage();

  const [fields, setFields] = useState<CatalogueField[]>([]);
  const [rules, setRules] = useState<Record<string, RuleState>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  // Off by default: the catalogue is also the answer to "what can I send?",
  // and a list that starts empty answers that with silence. The filter is for
  // coming back to something already configured, which is the other visit.
  const [onlyConfigured, setOnlyConfigured] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [catalogue, mappings] = await Promise.all([
        fieldsApi.list(),
        fieldMappingsApi.list(kind, recipientId),
      ]);
      setFields(catalogue);

      const state: Record<string, RuleState> = {};
      for (const mapping of mappings) {
        state[mapping.sourceKey] = {
          targetName: mapping.targetName ?? "",
          suppressed: mapping.suppressed ?? false,
        };
      }
      setRules(state);
    } catch (err) {
      setError(describeError(err));
    } finally {
      setLoading(false);
    }
  }, [kind, recipientId, describeError]);

  useEffect(() => {
    void load();
  }, [load]);

  function update(key: string, change: Partial<RuleState>) {
    setRules((current) => {
      const existing = current[key] ?? { targetName: "", suppressed: false };
      return { ...current, [key]: { ...existing, ...change } };
    });
  }

  async function save() {
    setSaving(true);
    setError("");
    try {
      const payload: FieldMapping[] = [];
      for (const field of fields) {
        const rule = rules[field.key];
        if (!configured(rule)) continue;
        payload.push(
          rule.suppressed
            ? { sourceKey: field.key, suppressed: true }
            : { sourceKey: field.key, targetName: rule.targetName.trim() },
        );
      }
      await fieldMappingsApi.replace(kind, recipientId, payload);
      onClose();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSaving(false);
    }
  }

  /** A built-in reads from the bundle; a tenant's own carries its own name. */
  function labelOf(field: CatalogueField): string {
    if (field.custom) return field.label || field.key;
    return t(`fields.${field.key}` as TranslationKey);
  }

  const changed = fields.filter((field) => configured(rules[field.key])).length;

  /** Rows to draw, which the filter narrows without touching what is saved. */
  function rowsIn(group: CatalogueField["group"]): CatalogueField[] {
    return fields.filter(
      (field) =>
        field.group === group &&
        (!onlyConfigured || configured(rules[field.key])),
    );
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={t("fieldMappings.title") + " — " + recipientName}
    >
      <div className="space-y-3">
        <p className="text-sm text-[var(--color-fg-muted)]">
          {t("fieldMappings.intro")}
        </p>
        {error ? <Alert tone="danger">{error}</Alert> : null}

        {loading ? (
          <p className="text-sm text-[var(--color-fg-muted)]">
            {t("common.loading")}
          </p>
        ) : (
          <>
            {/* The filter sits above the header rather than in it: it changes
                what the table shows, not what a column means. */}
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={onlyConfigured}
                onChange={(event) => setOnlyConfigured(event.target.checked)}
              />
              {t("fieldMappings.onlyConfigured")}
              <span className="text-[var(--color-fg-muted)]">({changed})</span>
            </label>

            {/* A header, so that the middle column does not have to be
                explained by its own placeholder. */}
            <div
              className="grid grid-cols-[minmax(0,1fr)_12rem_4rem] items-center
                gap-3 border-b border-[var(--color-border)] pb-1
                text-xs text-[var(--color-fg-muted)]"
            >
              <span>{t("fieldMappings.columnField")}</span>
              <span>{t("fieldMappings.columnTarget")}</span>
              <span className="w-16 text-center whitespace-nowrap">
                {t("fieldMappings.suppress")}
              </span>
            </div>

            {onlyConfigured && changed === 0 ? (
              <p className="py-2 text-sm text-[var(--color-fg-muted)]">
                {t("fieldMappings.none")}
              </p>
            ) : null}

            {groupOrder.map((group) => {
              const inGroup = rowsIn(group);
              if (inGroup.length === 0) return null;
              return (
                <section key={group} className="pt-3">
                  <h3 className="pb-1 text-xs font-medium tracking-wide text-[var(--color-fg-muted)] uppercase">
                    {t(`fieldGroup.${group}` as TranslationKey)}
                  </h3>
                  <div>
                    {inGroup.map((field) => {
                      const rule = rules[field.key] ?? {
                        targetName: "",
                        suppressed: false,
                      };
                      const isSet = configured(rule);
                      return (
                        <div
                          key={field.key}
                          className={
                            "grid grid-cols-[minmax(0,1fr)_12rem_4rem] items-center " +
                            "gap-3 border-l-2 py-1 pl-2 " +
                            // A configured row is the one somebody came back
                            // for. Three of them among forty are otherwise
                            // indistinguishable from the rest.
                            (isSet
                              ? "border-[var(--color-primary)] bg-[var(--color-bg-subtle)]"
                              : "border-transparent")
                          }
                        >
                          <div className="flex min-w-0 items-baseline gap-2">
                            <span className="shrink-0 text-sm">
                              {labelOf(field)}
                            </span>
                            {/* The key is what the API stores and what whoever
                                writes the receiving end needs, so it stays
                                visible — but it is secondary, so it no longer
                                takes a line of its own. */}
                            <code
                              className="truncate text-xs text-[var(--color-fg-muted)]"
                              title={field.key}
                            >
                              {field.key}
                            </code>
                            {field.disabled ? (
                              <Badge tone="neutral">
                                {t("fieldMappings.retired")}
                              </Badge>
                            ) : null}
                          </div>
                          <Input
                            value={rule.targetName}
                            disabled={rule.suppressed}
                            placeholder={t("fieldMappings.defaultName")}
                            onChange={(event) =>
                              update(field.key, {
                                targetName: event.target.value,
                              })
                            }
                          />
                          <input
                            type="checkbox"
                            className="w-16"
                            aria-label={
                              t("fieldMappings.suppress") + " " + field.key
                            }
                            checked={rule.suppressed}
                            onChange={(event) =>
                              update(field.key, {
                                suppressed: event.target.checked,
                                targetName: event.target.checked
                                  ? ""
                                  : rule.targetName,
                              })
                            }
                          />
                        </div>
                      );
                    })}
                  </div>
                </section>
              );
            })}
          </>
        )}

        <div className="flex items-center justify-between gap-2">
          <span className="text-xs text-muted-foreground">
            {changed === 0
              ? t("fieldMappings.none")
              : t("fieldMappings.count").replace("{n}", String(changed))}
          </span>
          <div className="flex gap-2">
            <Button variant="secondary" onClick={onClose} disabled={saving}>
              {t("common.cancel")}
            </Button>
            <Button onClick={() => void save()} disabled={saving || loading}>
              {t("common.save")}
            </Button>
          </div>
        </div>
      </div>
    </Modal>
  );
}
