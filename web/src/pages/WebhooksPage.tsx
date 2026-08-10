import { useCallback, useEffect, useState } from "react";

import { webhooksApi } from "../api/endpoints";
import type {
  CreatedWebhookSubscription,
  WebhookDelivery,
  WebhookSubscription,
} from "../api/types";
import { useErrorMessage, useT } from "../i18n";
import type { Translate } from "../i18n";
import type { TranslationKey } from "../i18n/en-US";
import {
  Alert,
  Badge,
  Button,
  ConfirmDialog,
  CopyField,
  EmptyRow,
  Field,
  GuidePanel,
  Input,
  LoadingRow,
  Modal,
  PageHeader,
  Table,
  Td,
  Th,
  DocsLink,
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

/**
 * The event's name in the reader's language, or the identifier itself.
 *
 * t falls back to the key it was given, which would put
 * "webhooks.event.user.whatever" on screen. This console will meet event
 * types it has no label for — the wildcard subscribes to the ones later
 * versions add, and a server can be newer than the page talking to it — so
 * the fallback has to be the identifier, which at least says what happened
 * and is what the receiver matches on anyway.
 */
function labelFor(t: Translate, prefix: string, value: string): string {
  const key = `${prefix}${value}` as TranslationKey;
  const translated = t(key);
  return translated === key ? value : translated;
}

/**
 * Groups event types by the thing they happen to, in the order the server
 * lists them.
 *
 * Fifteen checkboxes in one column is a list somebody scans rather than
 * reads, and the subject is already the first half of every name — so the
 * grouping is in the data and only the presentation was flattening it.
 *
 * Derived from the identifier rather than from a table kept here: a table
 * would be a second place to remember when an event is added, and the
 * failure would be a new event silently landing in the wrong group or in
 * none.
 */
function groupEvents(events: string[]): [string, string[]][] {
  const groups: [string, string[]][] = [];
  for (const event of events) {
    const subject = event.split(".")[0];
    const existing = groups.find(([name]) => name === subject);
    if (existing) existing[1].push(event);
    else groups.push([subject, [event]]);
  }
  return groups;
}

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
          <>
            <DocsLink page="webhooks/" />
            <Button onClick={() => setCreating(true)}>
              {t("webhooks.new")}
            </Button>
          </>
        }
      />

      <GuidePanel
        id="webhooks"
        title={t("webhooks.guideTitle")}
        docsPage="webhooks/"
      >
        {t("webhooks.guideBody")}
      </GuidePanel>

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
          {subscriptions === null && <LoadingRow colSpan={5} />}
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
                  : subscription.events
                      .map((event) => labelFor(t, "webhooks.event.", event))
                      .join(", ")}
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
              groupEvents(available).map(([subject, events]) => (
                <div key={subject} className="mt-1">
                  <div className="mb-1 text-[length:var(--font-size-xs)] font-[weight:var(--font-weight-medium)] text-[var(--color-fg-muted)]">
                    {labelFor(t, "webhooks.subject.", subject)}
                  </div>
                  {events.map((event) => (
                    <label
                      key={event}
                      className="flex items-baseline gap-2 py-0.5 text-[length:var(--font-size-sm)]"
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
                      {/* Both, and not one or the other. The reader choosing
                          these is an administrator who needs to know what
                          the event means; the person wiring up the receiver
                          matches on the literal string, and a translated
                          label alone would leave them guessing at it.

                          Unless there is no label — an event this build has
                          not been taught — in which case the identifier is
                          already standing in as the label and printing it a
                          second time beside itself says nothing. */}
                      <span>{labelFor(t, "webhooks.event.", event)}</span>
                      {labelFor(t, "webhooks.event.", event) !== event && (
                        <code className="text-[length:var(--font-size-xs)] text-[var(--color-fg-subtle)]">
                          {event}
                        </code>
                      )}
                    </label>
                  ))}
                </div>
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
