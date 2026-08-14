import { useCallback, useEffect, useState } from "react";

import { groupsApi, userApi } from "../api/endpoints";
import type { Group, GroupMember, User } from "../api/types";
import { useErrorMessage, useT } from "../i18n";
import {
  Alert,
  Badge,
  Button,
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
  Th,
} from "../components/ui";

/**
 * Groups, and who is in them.
 *
 * A separate screen from organizations because they are separate concepts:
 * an organization is where somebody sits, one of them, in a tree; a group is
 * a set they belong to, any number of them, flat. Membership grants nothing,
 * which is why there is nothing on this screen about permissions.
 */
export function GroupsPage() {
  const t = useT();
  const describeError = useErrorMessage();

  const [groups, setGroups] = useState<Group[] | null>(null);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Group | null>(null);
  const [creating, setCreating] = useState(false);
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [deleting, setDeleting] = useState<Group | null>(null);

  const [managing, setManaging] = useState<Group | null>(null);
  const [members, setMembers] = useState<GroupMember[]>([]);
  const [candidates, setCandidates] = useState<User[]>([]);
  const [toAdd, setToAdd] = useState("");

  const load = useCallback(async () => {
    try {
      setGroups(await groupsApi.list());
    } catch (err) {
      setError(describeError(err));
    }
  }, [describeError]);

  useEffect(() => {
    void load();
  }, [load]);

  function openCreate() {
    setEditing(null);
    setDisplayName("");
    setDescription("");
    setCreating(true);
  }

  function openEdit(group: Group) {
    setEditing(group);
    setDisplayName(group.displayName);
    setDescription(group.description);
    setCreating(true);
  }

  async function save(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      if (editing) {
        await groupsApi.update(editing.id, { displayName, description });
      } else {
        await groupsApi.create({ displayName, description });
      }
      setCreating(false);
      await load();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  async function openMembers(group: Group) {
    setError("");
    setManaging(group);
    setToAdd("");
    try {
      const [current, page] = await Promise.all([
        groupsApi.members(group.id),
        userApi.list({ page: 1, pageSize: 200 }),
      ]);
      setMembers(current);
      setCandidates(page.items);
    } catch (err) {
      setError(describeError(err));
    }
  }

  async function addMember() {
    if (!managing || !toAdd) return;
    setError("");
    try {
      await groupsApi.addMembers(managing.id, [toAdd]);
      setMembers(await groupsApi.members(managing.id));
      setToAdd("");
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  async function removeMember(userId: string) {
    if (!managing) return;
    setError("");
    try {
      await groupsApi.removeMember(managing.id, userId);
      setMembers(await groupsApi.members(managing.id));
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  async function remove() {
    if (!deleting) return;
    setError("");
    try {
      await groupsApi.remove(deleting.id);
      setDeleting(null);
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  const memberIds = new Set(members.map((m) => m.userId));
  const addable = candidates.filter((user) => !memberIds.has(user.id));

  return (
    <>
      <PageHeader
        title={t("groups.title")}
        subtitle={t("groups.subtitle")}
        actions={
          <>
            <DocsLink page="organizations/" />
            <Button onClick={openCreate}>{t("groups.new")}</Button>
          </>
        }
      />

      <GuidePanel
        id="groups"
        docsPage="organizations/"
        title={t("groups.guideTitle")}
      >
        {t("groups.guideBody")}
      </GuidePanel>

      {error && <Alert tone="danger">{error}</Alert>}

      <Table>
        <thead>
          <tr>
            <Th>{t("groups.colName")}</Th>
            <Th>{t("groups.colDescription")}</Th>
            <Th>{t("groups.colMembers")}</Th>
            <Th>{t("groups.colSource")}</Th>
            <Th>{t("common.actions")}</Th>
          </tr>
        </thead>
        <tbody>
          {groups === null && <LoadingRow colSpan={5} />}
          {groups?.length === 0 && <EmptyRow colSpan={5} />}
          {groups?.map((group) => (
            <tr key={group.id}>
              <Td>{group.displayName}</Td>
              <Td>{group.description}</Td>
              <Td>{group.memberCount}</Td>
              <Td>
                {/* A directory-owned group is worth marking: an edit here
                    may be overwritten by the next sync. */}
                <Badge tone={group.source === "SCIM" ? "warning" : "neutral"}>
                  {t(`groups.source.${group.source}`)}
                </Badge>
              </Td>
              <Td>
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => void openMembers(group)}
                  >
                    {t("groups.members")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => openEdit(group)}
                  >
                    {t("common.edit")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost-danger"
                    onClick={() => setDeleting(group)}
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
        open={creating}
        title={editing ? t("groups.edit") : t("groups.new")}
        onClose={() => setCreating(false)}
        footer={
          <>
            <Button variant="secondary" onClick={() => setCreating(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" form="group-form" disabled={submitting}>
              {t("common.save")}
            </Button>
          </>
        }
      >
        <form id="group-form" onSubmit={save} className="flex flex-col gap-4">
          <Field label={t("groups.name")} required>
            <Input
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              required
              autoFocus
            />
          </Field>
          <Field label={t("groups.description")}>
            <Input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </Field>
        </form>
      </Modal>

      <Modal
        open={managing !== null}
        title={t("groups.membersOf", managing?.displayName ?? "")}
        onClose={() => setManaging(null)}
        footer={
          <Button onClick={() => setManaging(null)}>{t("common.close")}</Button>
        }
      >
        <div className="flex flex-col gap-4">
          <div className="flex items-end gap-2">
            <div className="flex-1">
              <Field label={t("groups.addMember")}>
                <Select
                  value={toAdd}
                  onChange={(e) => setToAdd(e.target.value)}
                >
                  <option value="">{t("groups.selectUser")}</option>
                  {addable.map((user) => (
                    <option key={user.id} value={user.id}>
                      {user.displayName} ({user.username})
                    </option>
                  ))}
                </Select>
              </Field>
            </div>
            <Button onClick={() => void addMember()} disabled={!toAdd}>
              {t("groups.add")}
            </Button>
          </div>

          <Table>
            <thead>
              <tr>
                <Th>{t("groups.colMemberName")}</Th>
                <Th>{t("groups.colUsername")}</Th>
                <Th>{t("common.actions")}</Th>
              </tr>
            </thead>
            <tbody>
              {members.length === 0 && <EmptyRow colSpan={3} />}
              {members.map((member) => (
                <tr key={member.userId}>
                  <Td>{member.displayName}</Td>
                  <Td>{member.username}</Td>
                  <Td>
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => void removeMember(member.userId)}
                    >
                      {t("groups.remove")}
                    </Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      </Modal>

      <ConfirmDialog
        open={deleting !== null}
        title={t("groups.confirmDeleteTitle")}
        message={t("groups.confirmDelete", deleting?.displayName ?? "")}
        destructive
        onConfirm={() => void remove()}
        onCancel={() => setDeleting(null)}
      />
    </>
  );
}
