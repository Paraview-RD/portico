import { useCallback, useEffect, useState } from "react";

import { organizationApi, userApi } from "../api/endpoints";
import type {
  AdminScope,
  Organization,
  OrganizationAdministrator,
  User,
} from "../api/types";
import {
  Alert,
  Badge,
  Button,
  Field,
  Input,
  Modal,
  Select,
} from "../components/ui";
import { useErrorMessage, useT } from "../i18n";

/**
 * Who is recorded as administering an organization.
 *
 * The screen says, in its own words, that this grants nothing — because a
 * list headed "Administrators" with an **Add** button reads as a permission
 * being handed out, and for now it is not one. Somebody filling this in is
 * describing the organization chart for a feature that has not shipped;
 * telling them otherwise here is cheaper than explaining it later.
 *
 * The scope has no default. It cannot be inferred afterwards — a row that
 * did not say whether it meant this organization or its whole branch is one
 * nobody can interpret when delegated administration arrives — and the only
 * person who knows is the one filling in this form.
 */
export function OrganizationAdministratorsDialog({
  open,
  organization,
  onClose,
}: {
  open: boolean;
  organization: Organization | null;
  onClose: () => void;
}) {
  const t = useT();
  const describeError = useErrorMessage();

  const [administrators, setAdministrators] = useState<
    OrganizationAdministrator[]
  >([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const [keyword, setKeyword] = useState("");
  const [candidates, setCandidates] = useState<User[]>([]);
  const [searching, setSearching] = useState(false);
  const [selected, setSelected] = useState<User | null>(null);
  const [scope, setScope] = useState<AdminScope | "">("");

  const organizationId = organization?.id ?? "";

  const load = useCallback(async () => {
    if (!organizationId) return;
    setLoading(true);
    setError("");
    try {
      setAdministrators(await organizationApi.administrators(organizationId));
    } catch (err) {
      setError(describeError(err));
    } finally {
      setLoading(false);
    }
  }, [organizationId, describeError]);

  useEffect(() => {
    if (open) {
      setKeyword("");
      setCandidates([]);
      setSelected(null);
      setScope("");
      void load();
    }
  }, [open, load]);

  async function search() {
    setSearching(true);
    setError("");
    try {
      const page = await userApi.list({
        keyword: keyword.trim(),
        pageSize: 10,
      });
      setCandidates(page.items);
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSearching(false);
    }
  }

  async function assign() {
    if (!selected || !scope) return;
    setError("");
    try {
      await organizationApi.assignAdministrator(
        organizationId,
        selected.id,
        scope,
      );
      setSelected(null);
      setScope("");
      setKeyword("");
      setCandidates([]);
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  async function revoke(userId: string) {
    setError("");
    try {
      await organizationApi.revokeAdministrator(organizationId, userId);
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  return (
    <Modal
      open={open}
      title={t("organizations.administrators", organization?.name ?? "")}
      onClose={onClose}
      footer={
        <Button variant="ghost" onClick={onClose}>
          {t("common.close")}
        </Button>
      }
    >
      {/* Not an Alert: nothing has gone wrong and nothing needs doing. It
          is the standing fact about this screen, and the three Alert tones
          all say something else. */}
      <p className="rounded border border-[var(--color-border)] bg-[var(--color-bg-soft)] px-3 py-2 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
        {t("organizations.administratorsGrantNothing")}
      </p>

      {error && (
        <div className="mt-4">
          <Alert tone="danger">{error}</Alert>
        </div>
      )}

      <div className="mt-4">
        {loading ? (
          <p className="text-[var(--color-fg-muted)]">{t("common.loading")}</p>
        ) : administrators.length === 0 ? (
          <p className="text-[var(--color-fg-muted)]">
            {t("organizations.noAdministrators")}
          </p>
        ) : (
          <ul className="divide-y divide-[var(--color-border)]">
            {administrators.map((admin) => (
              <li
                key={admin.userId}
                className="flex items-center justify-between gap-3 py-2"
              >
                <div>
                  <div className="flex items-center gap-2">
                    <span>{admin.displayName}</span>
                    <code className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                      {admin.username}
                    </code>
                    {/* The account's own status, not the assignment's: an
                        assignment is not removed when somebody is suspended,
                        because it would come back on its own when they were
                        reinstated and nobody would have decided either. */}
                    {admin.status !== "ACTIVE" && (
                      <Badge tone="danger">{t(`status.${admin.status}`)}</Badge>
                    )}
                  </div>
                  <div className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                    {t(`organizations.scope.${admin.scope}`)} ·{" "}
                    {t("organizations.grantedBy", admin.grantedByName)}
                  </div>
                </div>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => void revoke(admin.userId)}
                >
                  {t("common.remove")}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="mt-6 border-t border-[var(--color-border)] pt-4">
        <Field label={t("organizations.addAdministrator")}>
          <div className="flex gap-2">
            <Input
              value={keyword}
              placeholder={t("organizations.searchAccount")}
              onChange={(e) => setKeyword(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  void search();
                }
              }}
            />
            <Button variant="secondary" onClick={() => void search()}>
              {searching ? t("common.loading") : t("common.search")}
            </Button>
          </div>
        </Field>

        {candidates.length > 0 && (
          <ul className="mt-2 max-h-40 overflow-y-auto rounded border border-[var(--color-border)]">
            {candidates.map((candidate) => (
              <li key={candidate.id}>
                <button
                  type="button"
                  className={`w-full px-3 py-1.5 text-left hover:bg-[var(--color-bg-soft)] ${
                    selected?.id === candidate.id
                      ? "bg-[var(--color-bg-soft)]"
                      : ""
                  }`}
                  onClick={() => setSelected(candidate)}
                >
                  {candidate.displayName}{" "}
                  <code className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                    {candidate.username}
                  </code>
                </button>
              </li>
            ))}
          </ul>
        )}

        {selected && (
          <div className="mt-3">
            <Field
              label={t("organizations.scopeLabel")}
              hint={t("organizations.scopeHelp")}
              required
            >
              <Select
                value={scope}
                onChange={(e) => setScope(e.target.value as AdminScope | "")}
              >
                {/* No default. Which of the two was meant cannot be
                    recovered later, and a pre-selected value would be the
                    form answering on the reader's behalf. */}
                <option value="">{t("organizations.scopeChoose")}</option>
                <option value="SELF">{t("organizations.scope.SELF")}</option>
                <option value="SUBTREE">
                  {t("organizations.scope.SUBTREE")}
                </option>
              </Select>
            </Field>
            <div className="mt-3">
              <Button disabled={!scope} onClick={() => void assign()}>
                {t("organizations.recordAdministrator", selected.displayName)}
              </Button>
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}
