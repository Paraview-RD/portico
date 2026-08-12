import type { UserAttributeDefinition } from "../api/types";
import { useT } from "../i18n";
import { Field, Input, Select } from "./ui";

/**
 * One control per attribute a tenant defined, holding this account's answers.
 *
 * Controlled, and stateless on purpose: the account form owns the values so
 * that saving them is part of saving the account rather than a second act
 * somebody has to remember.
 *
 * Every answer is a string, because that is what the server stores — the kind
 * decides which control collects it and what shape it has to be in, not
 * whether it stays text on the way there.
 */
export function UserAttributeValues({
  definitions,
  values,
  onChange,
}: {
  /**
   * Active definitions only. Retired ones are deliberately not offered: their
   * answers are kept and readable, but asking for a new one would be asking
   * for something the tenant already decided to stop collecting.
   */
  definitions: UserAttributeDefinition[];
  values: Record<string, string>;
  onChange: (key: string, value: string) => void;
}) {
  const t = useT();

  if (definitions.length === 0) {
    return (
      <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
        {t("userValues.none")}
      </p>
    );
  }

  return (
    <div className="grid gap-4 sm:grid-cols-2">
      {definitions.map((definition) => {
        const value = values[definition.key] ?? "";
        const set = (next: string) => onChange(definition.key, next);

        return (
          <Field
            key={definition.key}
            label={definition.label}
            hint={definition.description || undefined}
            required={definition.required}
          >
            {definition.kind === "SELECT" ? (
              <Select
                value={value}
                onChange={(e) => set(e.target.value)}
                required={definition.required}
              >
                {/* An explicit "not set" option rather than a blank one: a
                    select whose first entry is empty looks like a list that
                    failed to load. */}
                <option value="">{t("userValues.selectNone")}</option>
                {(definition.allowedValues ?? []).map((allowed) => (
                  <option key={allowed} value={allowed}>
                    {allowed}
                  </option>
                ))}
              </Select>
            ) : definition.kind === "BOOLEAN" ? (
              // A select rather than a checkbox, because there are three
              // states here and a checkbox can only hold two. "Never asked"
              // and "asked, and the answer is no" are different facts, and
              // an unticked box would send the second for both.
              <Select
                value={value}
                onChange={(e) => set(e.target.value)}
                required={definition.required}
              >
                <option value="">{t("userValues.selectNone")}</option>
                <option value="true">{t("userValues.booleanTrue")}</option>
                <option value="false">{t("userValues.booleanFalse")}</option>
              </Select>
            ) : (
              <Input
                // A date is a date only, which is what the server stores; a
                // number gets the numeric keyboard and the browser's own
                // refusal of letters, so the server's is the second line of
                // defence rather than the first thing anybody meets.
                type={
                  definition.kind === "DATE"
                    ? "date"
                    : definition.kind === "NUMBER"
                      ? "number"
                      : "text"
                }
                // Any decimal, because an attribute may hold a rate as
                // readily as a headcount, and the default step of 1 would
                // have the browser reject "1.5" with no explanation on the
                // page.
                step={definition.kind === "NUMBER" ? "any" : undefined}
                value={value}
                onChange={(e) => set(e.target.value)}
                required={definition.required}
              />
            )}
          </Field>
        );
      })}
    </div>
  );
}
