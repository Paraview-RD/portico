import { useCallback, useEffect, useState } from "react";

import { webhooksApi } from "../api/endpoints";
import type {
  CreatedWebhookSubscription,
  WebhookDelivery,
  WebhookSubscription,
} from "../api/types";
import { useErrorMessage, useT } from "../i18n";
import {
  Alert,
  Badge,
  Button,
  ConfirmDialog,
  CopyField,
  EmptyRow,
  Field,
  Input,
  Modal,
  PageHeader,
  Table,
  Td,
  Th,
} from "../components/ui";

/**
 * Outbound event subscriptions.
 *
 * Two things this screen exists to make visible. The signing secret, which
 * appears once at creation and is never served again. And the delivery
 * history — the answer to "we are not receiving your events", which is
 * otherwise a question only the receiver's logs can settle.
 *
 * Its own screen rather than a section of the settings page, for the same
 * reason as provisioning: this is a connection to another system, not a
 * preference, and the delivery history is something an operator comes here
 * to read rather than something they set once.
 */
export function WebhooksPage() {
  const t = useT();
  const describeError = useErrorMessage();

  const [subscriptions, setSubscriptions] = useState<
    WebhookSubscription[] | null
  >(null);
  const [available, setAvailable] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [created, setCreated] = useState<CreatedWebhookSubscription | null>(
    null,
  );
  const [deleting, setDeleting] = useState<WebhookSubscription | null>(null);
  const [inspecting, setInspecting] = useState<WebhookSubscription | null>(
    null,
  );
  const [deliveries, setDeliveries] = useState<WebhookDelivery[]>([]);

  const load = useCallback(async () => {
    try {
      setSubscriptions(await webhooksApi.list());
    } catch (err) {
      setError(describeError(err));
    }
  }, [describeError]);

  useEffect(() => {
    void load();
    // The event list comes from the server so this screen cannot drift from
    // what can actually be subscribed to.
    webhooksApi
      .events()
      .then(setAvailable)
      .catch(() => setAvailable([]));
  }, [load]);

  async function create(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      const subscription = await webhooksApi.create({
        name,
        url,
        events: selected,
      });
      setCreating(false);
      setName("");
      setUrl("");
      setSelected([]);
      setCreated(subscription);
      await load();
    } catch (err) {
      setError(describeError(err));
    } finally {
      setSubmitting(false);
    }
  }

  async function toggle(subscription: WebhookSubscription) {
    setError("");
    try {
      if (subscription.status === "ACTIVE") {
        await webhooksApi.disable(subscription.id);
      } else {
        await webhooksApi.enable(subscription.id);
      }
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  async function inspect(subscription: WebhookSubscription) {
    setError("");
    setInspecting(subscription);
    try {
      setDeliveries(await webhooksApi.deliveries(subscription.id));
    } catch (err) {
      setError(describeError(err));
    }
  }

  async function remove() {
    if (!deleting) return;
    setError("");
    try {
      await webhooksApi.remove(deleting.id);
      setDeleting(null);
      await load();
    } catch (err) {
      setError(describeError(err));
    }
  }

  return (
    <>
      <PageHeader
        title={t("webhooks.title")}
        subtitle={t("webhooks.subtitle")}
        actions={
          <Button onClick={() => setCreating(true)}>{t("webhooks.new")}</Button>
        }
      />

      {error && (
        <div className="mb-4">
          <Alert tone="danger">{error}</Alert>
        </div>
      )}

      <Table>
        <thead>
          <tr>
            <Th>{t("webhooks.colName")}</Th>
            <Th>{t("webhooks.colUrl")}</Th>
            <Th>{t("webhooks.colEvents")}</Th>
            <Th>{t("webhooks.colStatus")}</Th>
            <Th>{t("common.actions")}</Th>
          </tr>
        </thead>
        <tbody>
          {subscriptions?.length === 0 && <EmptyRow colSpan={5} />}
          {subscriptions?.map((subscription) => (
            <tr key={subscription.id}>
              <Td>{subscription.name}</Td>
              <Td>
                <code className="text-[length:var(--font-size-sm)]">
                  {subscription.url}
                </code>
              </Td>
              <Td>
                {subscription.events.includes("*")
                  ? t("webhooks.allEvents")
                  : subscription.events.join(", ")}
              </Td>
              <Td>
                <Badge
                  tone={
                    subscription.status === "ACTIVE" ? "success" : "neutral"
                  }
                >
                  {t(`status.${subscription.status}`)}
                </Badge>
              </Td>
              <Td>
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => void inspect(subscription)}
                  >
                    {t("webhooks.deliveries")}
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => void toggle(subscription)}
                  >
                    {subscription.status === "ACTIVE"
                      ? t("common.disable")
                      : t("common.enable")}
                  </Button>
                  <Button
                    size="sm"
                    variant="danger"
                    onClick={() => setDeleting(subscription)}
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
        title={t("webhooks.new")}
        onClose={() => setCreating(false)}
        footer={
          <>
            <Button variant="secondary" onClick={() => setCreating(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" form="webhook-create" disabled={submitting}>
              {t("common.create")}
            </Button>
          </>
        }
      >
        <form
          id="webhook-create"
          onSubmit={create}
          className="flex flex-col gap-4"
        >
          <Field label={t("webhooks.name")} required>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              autoFocus
            />
          </Field>
          <Field
            label={t("webhooks.url")}
            hint={t("webhooks.urlHint")}
            required
          >
            <Input
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://example.com/hooks/portico"
              required
            />
          </Field>
          <fieldset className="flex flex-col gap-2">
            <legend className="text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-medium)]">
              {t("webhooks.events")}
            </legend>
            <label className="flex items-center gap-2 text-[length:var(--font-size-sm)]">
              <input
                type="checkbox"
                checked={selected.includes("*")}
                onChange={(e) => setSelected(e.target.checked ? ["*"] : [])}
              />
              {t("webhooks.allEventsHint")}
            </label>
            {!selected.includes("*") &&
              available.map((event) => (
                <label
                  key={event}
                  className="flex items-center gap-2 text-[length:var(--font-size-sm)]"
                >
                  <input
                    type="checkbox"
                    checked={selected.includes(event)}
                    onChange={(e) =>
                      setSelected(
                        e.target.checked
                          ? [...selected, event]
                          : selected.filter((s) => s !== event),
                      )
                    }
                  />
                  <code>{event}</code>
                </label>
              ))}
          </fieldset>
        </form>
      </Modal>

      <Modal
        open={created !== null}
        title={t("webhooks.created")}
        onClose={() => setCreated(null)}
        footer={
          <Button onClick={() => setCreated(null)}>{t("common.done")}</Button>
        }
      >
        <div className="flex flex-col gap-4">
          <Alert tone="warning">{t("webhooks.secretWarning")}</Alert>
          <CopyField
            label={t("webhooks.secret")}
            value={created?.secret ?? ""}
          />
        </div>
      </Modal>

      <Modal
        open={inspecting !== null}
        title={t("webhooks.deliveriesFor", inspecting?.name ?? "")}
        onClose={() => setInspecting(null)}
        footer={
          <Button onClick={() => setInspecting(null)}>
            {t("common.close")}
          </Button>
        }
      >
        <Table>
          <thead>
            <tr>
              <Th>{t("webhooks.colEvent")}</Th>
              <Th>{t("webhooks.colDeliveryStatus")}</Th>
              <Th>{t("webhooks.colAttempts")}</Th>
              <Th>{t("webhooks.colResponse")}</Th>
            </tr>
          </thead>
          <tbody>
            {deliveries.length === 0 && <EmptyRow colSpan={4} />}
            {deliveries.map((delivery) => (
              <tr key={delivery.id}>
                <Td>
                  <code className="text-[length:var(--font-size-sm)]">
                    {delivery.eventType}
                  </code>
                </Td>
                <Td>
                  <Badge tone={deliveryTone(delivery.status)}>
                    {t(`webhooks.status.${delivery.status}`)}
                  </Badge>
                </Td>
                <Td>{delivery.attempts}</Td>
                <Td>
                  {/* The last error rather than the last status when there
                      is one: "connection refused" says more than a blank. */}
                  {delivery.lastStatus ?? delivery.lastError ?? ""}
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>
      </Modal>

      <ConfirmDialog
        open={deleting !== null}
        title={t("webhooks.confirmDeleteTitle")}
        message={t("webhooks.confirmDelete", deleting?.name ?? "")}
        destructive
        onConfirm={() => void remove()}
        onCancel={() => setDeleting(null)}
      />
    </>
  );
}

function deliveryTone(
  status: WebhookDelivery["status"],
): "success" | "danger" | "neutral" {
  switch (status) {
    case "DELIVERED":
      return "success";
    case "FAILED":
      return "danger";
    default:
      return "neutral";
  }
}
