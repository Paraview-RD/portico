import { useCallback, useEffect, useState } from "react";

import { groupsApi, invitationsApi, organizationApi } from "../api/endpoints";
import type { Group, Invitation, Organization } from "../api/types";
import { useErrorMessage, useT } from "../i18n";
import { formatInstant } from "../i18n/format";
import type { Translate } from "../i18n";
import {
  Alert,
  Badge,
  Button,
  Code,
  ConfirmDialog,
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
  Th,
} from "../components/ui";

/**
 * Invitation-gated registration: administrator-issued, quota-limited codes
 * that let self-registration stay closed to the public while still
 * admitting specific people, without an administrator creating each
 * account by hand.
 *
 * Its own screen rather than a section of settings, for the same reason as
 * Webhooks — a code is a thing with its own lifecycle (issued, spent,
 * disabled), not a preference. The setting that actually requires a code on
 * every registration lives on the Settings page; this screen is where codes
 * themselves are issued and retired.
 *
 * See docs/adr/0001-invitation-code-lifecycle-and-authorization-model.md.
 */

/** Converts a datetime-local value to the RFC 3339 the API expects. */
function toRFC3339(localValue: string): string {
  if (!localValue) return "";
  const parsed = new Date(localValue);
  return Number.isNaN(parsed.getTime()) ? "" : parsed.toISOString();
}

/**
 * A code nobody has to invent by hand: unambiguous characters only.
 *
 * Drawn from crypto.getRandomValues rather than Math.random. This is a
 * default an administrator can edit or regenerate freely, not the only
 * thing standing between a stranger and an account — quota and expiry do
 * that work too — but it is still a bearer credential, and there is no
 * reason to hand one out with a non-cryptographic generator when the
 * browser provides a proper one for free.
 */
function randomCode(): string {
  const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789";
  const bytes = new Uint32Array(10);
  crypto.getRandomValues(bytes);
  let out = "";
  for (let i = 0; i < 10; i++) {
    out += alphabet[bytes[i] % alphabet.length];
    if (i === 4) out += "-";
  }
  return out;
}

/**
 * ACTIVE/DISABLED is the whole of what the server stores — see
 * model.Invitation. "Exhausted" and "expired" are derived here, the same
 * way the server derives them at redemption time, and take priority over a
 * stored ACTIVE: a code showing plain "Active" after its quota is spent
 * would read as still usable when it is not.
 */
function deriveStatus(
  invitation: Invitation,
): "active" | "exhausted" | "expired" | "disabled" {
  if (invitation.status === "DISABLED") return "disabled";
  if (invitation.usedCount >= invitation.quota) return "exhausted";
  if (invitation.expiresAt && new Date(invitation.expiresAt) <= new Date()) {
    return "expired";
  }
  return "active";
}

function InvitationStatusBadge({ invitation }: { invitation: Invitation }) {
  const t = useT();
  const derived = deriveStatus(invitation);
  const tone =
    derived === "active"
      ? "success"
      : derived === "disabled"
        ? "neutral"
        : "warning";
  return <Badge tone={tone}>{t(`invitations.status.${derived}`)}</Badge>;
}

function scopeLabel(
  t: Translate,
  invitation: Invitation,
  organizations: Organization[],
  groups: Group[],
): string {
  const parts: string[] = [];
  const org = organizations.find((o) => o.id === invitation.organizationId);
  if (org) parts.push(org.name);
  for (const groupId of invitation.groupIds) {
    const group = groups.find((g) => g.id === groupId);
    if (group) parts.push(group.displayName);
  }
  return parts.length > 0 ? parts.join(", ") : t("invitations.noScope");
}

export function InvitationsPage() {
  const t = useT();
  const describeError = useErrorMessage();

  const [invitations, setInvitations] = useState<Invitation[] | null>(null);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [error, setError] = useState("");

  const [creating, setCreating] = useState(false);
  const [code, setCode] = useState(randomCode());
  const [quota, setQuota] = useState("100");
  const [unlimited, setUnlimited] = useState(false);
  const [organizationId, setOrganizationId] = useState("");
  const [groupId, setGroupId] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const [disabling, setDisabling] = useState<Invitation | null>(null);

  const load = useCallback(async () => {
    try {
      setInvitations(await invitationsApi.list());
    } catch (err) {
      setError(describeError(err));
    }
  }, [describeError]);

  useEffect(() => {
    void load();
    organizationApi
      .list(true)
      .then(setOrganizations)
      .catch(() => setOrganizations([]));
    groupsApi
      .list()
      .then(setGroups)
      .catch(() => setGroups([]));
  }, [load]);

  function openCreate() {
    setCode(randomCode());
    setQuota("100");
    setUnlimited(false);
    setOrganizationId("");
    setGroupId("");
    setExpiresAt("");
    setError("");
    setCreating(true);
  }

  async function create(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      await invitationsApi.create({
        code,
        // A million is InvitationService.MaxInvitationQuota — comfortably
        // above any real deployment and safely below what the server
        // stores, so "unlimited" sends a concrete number rather than a
        // sentinel the API would have to special-case.
        quota: unlimited ? 1_000_000 : Number(quota),
        organizationId: organizationId || undefined,
        groupIds: groupId ? [groupId] : undefined,
        expiresAt: toRFC3339(expiresAt) || undefined,
      });
      setCreating(false);
      await load();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  async function confirmDisable() {
    if (!disabling) return;
    setError("");
    try {
      await invitationsApi.disable(disabling.id);
      setDisabling(null);
      await load();
    } catch (err) {
      setError(describeError(err));
      setDisabling(null);
    }
  }

  return (
    <div>
      <PageHeader
        title={t("invitations.title")}
        subtitle={t("invitations.subtitle")}
        actions={
          <Button variant="primary" onClick={openCreate}>
            {t("invitations.new")}
          </Button>
        }
      />

      <GuidePanel id="invitations" title={t("invitations.guideTitle")}>
        {t("invitations.guideBody")}
      </GuidePanel>

      {error && <Alert tone="danger">{error}</Alert>}

      <Table>
        <thead>
          <tr>
            <Th>{t("invitations.colCode")}</Th>
            <Th>{t("invitations.colQuota")}</Th>
            <Th>{t("invitations.colScope")}</Th>
            <Th>{t("invitations.colExpires")}</Th>
            <Th>{t("invitations.colStatus")}</Th>
            <Th>{t("common.actions")}</Th>
          </tr>
        </thead>
        <tbody>
          {invitations === null && <LoadingRow colSpan={6} />}
          {invitations?.length === 0 && <EmptyRow colSpan={6} />}
          {invitations?.map((invitation) => (
            <tr key={invitation.id}>
              <Td>
                <Code>{invitation.code}</Code>
              </Td>
              <Td>
                {invitation.usedCount} / {invitation.quota}
              </Td>
              <Td>{scopeLabel(t, invitation, organizations, groups)}</Td>
              <Td>
                {invitation.expiresAt ? (
                  formatInstant(invitation.expiresAt, "date")
                ) : (
                  <span className="text-[var(--color-fg-muted)]">
                    {t("invitations.neverExpires")}
                  </span>
                )}
              </Td>
              <Td>
                <InvitationStatusBadge invitation={invitation} />
              </Td>
              <Td>
                {invitation.status === "DISABLED" ? (
                  <span className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                    {t("invitations.terminal")}
                  </span>
                ) : (
                  <Button
                    variant="ghost-danger"
                    onClick={() => setDisabling(invitation)}
                  >
                    {t("common.disable")}
                  </Button>
                )}
              </Td>
            </tr>
          ))}
        </tbody>
      </Table>

      <Modal
        open={creating}
        title={t("invitations.new")}
        onClose={() => setCreating(false)}
        footer={
          <>
            <Button variant="secondary" onClick={() => setCreating(false)}>
              {t("common.cancel")}
            </Button>
            <Button
              type="submit"
              form="create-invitation"
              disabled={submitting || code.trim() === ""}
            >
              {t("common.create")}
            </Button>
          </>
        }
      >
        <form
          id="create-invitation"
          className="flex flex-col gap-4"
          onSubmit={(e) => void create(e)}
        >
          <Field label={t("invitations.fieldCode")} required>
            <div className="flex gap-2">
              <Input
                value={code}
                onChange={(e) => setCode(e.target.value)}
                autoFocus
              />
              <Button
                type="button"
                variant="secondary"
                onClick={() => setCode(randomCode())}
              >
                {t("invitations.generate")}
              </Button>
            </div>
          </Field>

          <Field label={t("invitations.fieldQuota")} required>
            <Input
              type="number"
              min={1}
              value={quota}
              disabled={unlimited}
              onChange={(e) => setQuota(e.target.value)}
            />
          </Field>
          <label className="-mt-2 flex items-center gap-2 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            <input
              type="checkbox"
              checked={unlimited}
              onChange={(e) => setUnlimited(e.target.checked)}
            />
            {t("invitations.unlimited")}
          </label>

          <Field
            label={t("invitations.fieldExpires")}
            hint={t("invitations.fieldExpiresHint")}
          >
            <Input
              type="datetime-local"
              value={expiresAt}
              onChange={(e) => setExpiresAt(e.target.value)}
            />
          </Field>

          <Field label={t("invitations.fieldOrganization")}>
            <Select
              value={organizationId}
              onChange={(e) => setOrganizationId(e.target.value)}
            >
              <option value="">{t("invitations.noneOption")}</option>
              {organizations.map((org) => (
                <option key={org.id} value={org.id}>
                  {org.name}
                </option>
              ))}
            </Select>
          </Field>

          <Field label={t("invitations.fieldGroup")}>
            <Select
              value={groupId}
              onChange={(e) => setGroupId(e.target.value)}
            >
              <option value="">{t("invitations.noneOption")}</option>
              {groups.map((group) => (
                <option key={group.id} value={group.id}>
                  {group.displayName}
                </option>
              ))}
            </Select>
          </Field>

          <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {t("invitations.assignHint")}
          </p>
        </form>
      </Modal>

      <ConfirmDialog
        open={disabling !== null}
        title={t("invitations.disableTitle")}
        message={t("invitations.disableMessage", disabling?.code ?? "")}
        destructive
        onConfirm={() => void confirmDisable()}
        onCancel={() => setDisabling(null)}
      />
    </div>
  );
}
