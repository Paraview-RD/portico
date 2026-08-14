import { useCallback, useEffect, useState } from "react";

import { userAttributesApi } from "../api/endpoints";
import type { UserAttributeDefinition } from "../api/types";
import { useErrorMessage, useT } from "../i18n";
import {
  Alert,
  Badge,
  Button,
  Code,
  ConfirmDialog,
  DocsLink,
  EmptyRow,
  Field,
  GuidePanel,
  Input,
  LoadingRow,
  Modal,
  PageHeader,
  Select,
  Table,
  Td,
  Textarea,
  Th,
} from "../components/ui";

/** The five types an attribute may have, in the order the picker offers them. */
const kinds: UserAttributeDefinition["kind"][] = [
  "TEXT",
  "NUMBER",
  "BOOLEAN",
  "DATE",
  "SELECT",
];

/**
 * The attributes a tenant defined for itself.
 *
 * Its own screen rather than a section of the settings form, because that
 * form is one form with one save button — a list somebody adds rows to has a
 * different shape, and folding it in would make "save" mean two things.
 *
 * What is edited here is the definition: the name on the form, the type, and
 * whether it is asked for. The answers live on each account, on the user
 * screen. Both halves feed the same field catalogue, which is what a mapping
 * picks from — see docs/field-mappings.md.
 */
export function UserAttributesPage() {
  const t = useT();
  const describeError = useErrorMessage();

  const [attributes, setAttributes] = useState<
    UserAttributeDefinition[] | null
  >(null);
  const [error, setError] = useState("");

  const [editing, setEditing] = useState<UserAttributeDefinition | null>(null);
  const [open, setOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [deleting, setDeleting] = useState<UserAttributeDefinition | null>(
    null,
  );

  const [key, setKey] = useState("");
  const [label, setLabel] = useState("");
  const [description, setDescription] = useState("");
  const [kind, setKind] = useState<UserAttributeDefinition["kind"]>("TEXT");
  // One per line rather than comma-separated: a value may contain a comma,
  // and a separator that cannot appear in the data is worth the extra height.
  const [allowedValues, setAllowedValues] = useState("");
  const [required, setRequired] = useState(false);
  const [sortOrder, setSortOrder] = useState("0");

  const load = useCallback(async () => {
    try {
      setAttributes(await userAttributesApi.list());
    } catch (err) {
      setError(describeError(err));
    }
  }, [describeError]);

  useEffect(() => {
    void load();
  }, [load]);

  function openCreate() {
    setEditing(null);
    setKey("");
    setLabel("");
    setDescription("");
    setKind("TEXT");
    setAllowedValues("");
    setRequired(false);
    // After the last one, so a new attribute lands at the bottom of the form
    // rather than in the middle of it.
    setSortOrder(String((attributes?.length ?? 0) * 10));
    setOpen(true);
  }

  function openEdit(attribute: UserAttributeDefinition) {
    setEditing(attribute);
    setKey(attribute.key);
    setLabel(attribute.label);
    setDescription(attribute.description ?? "");
    setKind(attribute.kind);
    setAllowedValues((attribute.allowedValues ?? []).join("\n"));
    setRequired(attribute.required);
    setSortOrder(String(attribute.sortOrder));
    setOpen(true);
  }

  async function save(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    const input = {
      key: key.trim(),
      label,
      description,
      kind,
      allowedValues:
        kind === "SELECT"
          ? allowedValues
              .split("\n")
              .map((value) => value.trim())
              .filter((value) => value !== "")
          : [],
      required,
      sortOrder: Number(sortOrder) || 0,
    };
    try {
      if (editing) {
        await userAttributesApi.update(editing.id, input);
      } else {
        await userAttributesApi.define(input);
      }
      setOpen(false);
      await load();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  async function toggle(attribute: UserAttributeDefinition) {
    setError("");
    try {
      if (attribute.disabled) {
        await userAttributesApi.enable(attribute.id);
      } else {
        await userAttributesApi.disable(attribute.id);
      }
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  async function remove() {
    if (!deleting) return;
    setError("");
    try {
      await userAttributesApi.remove(deleting.id);
      setDeleting(null);
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  return (
    <>
      <PageHeader
        title={t("userAttributes.title")}
        subtitle={t("userAttributes.subtitle")}
        actions={
          <>
            <DocsLink page="field-mappings/" />
            <Button onClick={openCreate}>{t("userAttributes.new")}</Button>
          </>
        }
      />

      <GuidePanel
        id="user-attributes"
        docsPage="field-mappings/"
        title={t("userAttributes.guideTitle")}
      >
        {t("userAttributes.guideBody")}
      </GuidePanel>

      {error && <Alert tone="danger">{error}</Alert>}

      <Table>
        <thead>
          <tr>
            <Th>{t("userAttributes.colLabel")}</Th>
            <Th>{t("userAttributes.colKey")}</Th>
            <Th>{t("userAttributes.colKind")}</Th>
            <Th>{t("userAttributes.colRequired")}</Th>
            <Th>{t("userAttributes.colStatus")}</Th>
            <Th>{t("common.actions")}</Th>
          </tr>
        </thead>
        <tbody>
          {attributes === null && <LoadingRow colSpan={6} />}
          {attributes?.length === 0 && <EmptyRow colSpan={6} />}
          {attributes?.map((attribute) => (
            <tr key={attribute.id}>
              <Td>{attribute.label}</Td>
              {/* The key, monospaced, because it is what a mapping stores and
                  what an application will see — the one string on this row
                  that somebody may have to type somewhere else. */}
              <Td>
                <Code>{attribute.key}</Code>
              </Td>
              <Td>{t(`userAttributes.kind.${attribute.kind}`)}</Td>
              {/* A dash rather than the word "no": the column is scanned for
                  the ones that are required, and forty rows of "No" is
                  forty things to read past. */}
              <Td>
                {attribute.required ? t("userAttributes.isRequired") : "—"}
              </Td>
              <Td>
                <Badge tone={attribute.disabled ? "neutral" : "success"}>
                  {attribute.disabled
                    ? t("userAttributes.retired")
                    : t("userAttributes.inUse")}
                </Badge>
              </Td>
              <Td>
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => openEdit(attribute)}
                  >
                    {t("common.edit")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => void toggle(attribute)}
                  >
                    {attribute.disabled
                      ? t("userAttributes.restore")
                      : t("userAttributes.retire")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost-danger"
                    onClick={() => setDeleting(attribute)}
                  >
                    {t("common.delete")}
                  </Button>
                </div>
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>

      <Modal
        open={open}
        title={editing ? t("userAttributes.edit") : t("userAttributes.new")}
        onClose={() => setOpen(false)}
        footer={
          <>
            <Button variant="secondary" onClick={() => setOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" form="attribute-form" disabled={submitting}>
              {t("common.save")}
            </Button>
          </>
        }
      >
        <form
          id="attribute-form"
          onSubmit={save}
          className="flex flex-col gap-4"
        >
          {/* Read-only once it exists. A mapping stores the key, so renaming
              it would stop whichever rule names it while the screen it was
              configured on still looked right. */}
          <Field
            label={t("userAttributes.key")}
            hint={
              editing
                ? t("userAttributes.keyFixed")
                : t("userAttributes.keyHint")
            }
            required
          >
            <Input
              value={key}
              onChange={(e) => setKey(e.target.value)}
              disabled={editing !== null}
              required
              autoFocus={editing === null}
            />
          </Field>

          <Field label={t("userAttributes.label")} required>
            <Input
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              required
              autoFocus={editing !== null}
            />
          </Field>

          <Field
            label={t("userAttributes.description")}
            hint={t("userAttributes.descriptionHint")}
          >
            <Input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </Field>

          <Field label={t("userAttributes.kindLabel")}>
            <Select
              value={kind}
              onChange={(e) =>
                setKind(e.target.value as UserAttributeDefinition["kind"])
              }
            >
              {kinds.map((option) => (
                <option key={option} value={option}>
                  {t(`userAttributes.kind.${option}`)}
                </option>
              ))}
            </Select>
          </Field>

          {kind === "SELECT" && (
            <Field
              label={t("userAttributes.allowedValues")}
              hint={t("userAttributes.allowedValuesHint")}
              required
            >
              <Textarea
                value={allowedValues}
                onChange={(e) => setAllowedValues(e.target.value)}
                rows={4}
              />
            </Field>
          )}

          <label className="flex items-start gap-2.5">
            <input
              type="checkbox"
              className="mt-1"
              checked={required}
              onChange={(e) => setRequired(e.target.checked)}
            />
            <span>
              <span className="block font-[weight:var(--font-weight-medium)] text-[var(--color-fg)]">
                {t("userAttributes.required")}
              </span>
              <span className="block text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                {t("userAttributes.requiredHint")}
              </span>
            </span>
          </label>

          <Field
            label={t("userAttributes.sortOrder")}
            hint={t("userAttributes.sortOrderHint")}
          >
            <Input
              type="number"
              value={sortOrder}
              onChange={(e) => setSortOrder(e.target.value)}
            />
          </Field>
        </form>
      </Modal>

      <ConfirmDialog
        open={deleting !== null}
        title={t("userAttributes.confirmDeleteTitle")}
        message={t("userAttributes.confirmDelete", deleting?.label ?? "")}
        destructive
        onConfirm={() => void remove()}
        onCancel={() => setDeleting(null)}
      />
    </>
  );
}
