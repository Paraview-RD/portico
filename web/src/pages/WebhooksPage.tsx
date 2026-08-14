import { useCallback, useEffect, useState } from "react";

import { webhooksApi } from "../api/endpoints";
import type {
  CreatedWebhookSubscription,
  DeliveryFilter,
  WebhookDelivery,
  WebhookDeliveryDetail,
  WebhookSnapshot,
  WebhookSubscription,
} from "../api/types";
import { FieldMappingEditor } from "../components/FieldMappingEditor";
import { useErrorMessage, useT } from "../i18n";
import { formatInstant } from "../i18n/format";
import type { Translate } from "../i18n";
import type { TranslationKey } from "../i18n/en-US";
import {
  Alert,
  Badge,
  Button,
  Code,
  ConfirmDialog,
  CopyField,
  DocsLink,
  EmptyRow,
  Field,
  GuidePanel,
  Input,
  LoadingRow,
  Modal,
  PageHeader,
  StatusBadge,
  Table,
  Td,
  Th,
  Timestamp,
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
/**
 * "55 accounts, 4 groups" — the counts as a sentence rather than a map.
 *
 * Named kinds rather than the raw keys, because "user: 55" is the API's
 * vocabulary and this is the sentence somebody reads before deciding to send
 * fifty thousand records to a receiver.
 */
function describeCounts(t: Translate, counts: Record<string, number>): string {
  return Object.entries(counts)
    .map(([kind, count]) =>
      t(
        "webhooks.snapshotCount",
        String(count),
        labelFor(t, "webhooks.kind.", kind),
      ),
    )
    .join("、");
}

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
  // Rows rather than a JSON box. The values are credentials somebody pastes
  // in, and a mistyped brace turning a bearer token into a parse error is a
  // worse first experience than two fields.
  const [headerRows, setHeaderRows] = useState<
    { name: string; value: string }[]
  >([]);
  const [submitting, setSubmitting] = useState(false);
  const [created, setCreated] = useState<CreatedWebhookSubscription | null>(
    null,
  );
  const [deleting, setDeleting] = useState<WebhookSubscription | null>(null);
  const [rotating, setRotating] = useState<WebhookSubscription | null>(null);
  // Behind a confirmation, and reported afterwards. This queues the largest
  // delivery the product makes — every account, organization and group the
  // tenant has — and an operator who pressed it by accident should learn
  // that from a dialog rather than from their receiver falling over.
  const [snapshotting, setSnapshotting] = useState<WebhookSubscription | null>(
    null,
  );
  const [snapshotResult, setSnapshotResult] = useState<WebhookSnapshot | null>(
    null,
  );
  const [inspecting, setInspecting] = useState<WebhookSubscription | null>(
    null,
  );
  const [deliveries, setDeliveries] = useState<WebhookDelivery[]>([]);
  const [deliveryCursor, setDeliveryCursor] = useState("");
  const [deliveryFilter, setDeliveryFilter] = useState<DeliveryFilter>("live");
  const [loadingMore, setLoadingMore] = useState(false);
  // Which row is open, and what it holds. Fetched on expand rather than with
  // the list: a full sync page's payload is five hundred objects.
  const [expanded, setExpanded] = useState<string>("");
  const [detail, setDetail] = useState<WebhookDeliveryDetail | null>(null);
  // Ticked in the create form, acted on after the secret has been read.
  // Not a second way to queue a snapshot: it routes into the same
  // confirmation the list-page button uses, so the counts and the warnings
  // are the same ones, arrived at from a different door.
  const [snapshotOnCreate, setSnapshotOnCreate] = useState(false);
  const [snapshotPreview, setSnapshotPreview] =
    useState<WebhookSnapshot | null>(null);
  // What this subscriber receives, and under what name. Its own dialog rather
  // than a section of the create form: it is edited long after registration,
  // usually because the receiving end asked for a different name.
  const [mapping, setMapping] = useState<WebhookSubscription | null>(null);

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
      const headers: Record<string, string> = {};
      for (const row of headerRows) {
        if (row.name.trim() !== "") headers[row.name.trim()] = row.value;
      }

      const subscription = await webhooksApi.create({
        name,
        url,
        events: selected,
        headers: Object.keys(headers).length > 0 ? headers : undefined,
      });
      setCreating(false);
      setName("");
      setUrl("");
      setSelected([]);
      setHeaderRows([]);
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
    setExpanded("");
    setDetail(null);
    await loadDeliveries(subscription.id, deliveryFilter);
  }

  // The first page. A filter change is the same thing — the cursor from one
  // filter means nothing under another, so it starts over rather than
  // continuing from a marker that describes a different query.
  async function loadDeliveries(
    subscriptionID: string,
    filter: DeliveryFilter,
  ) {
    setError("");
    setExpanded("");
    try {
      const page = await webhooksApi.deliveries(subscriptionID, { filter });
      setDeliveries(page.items);
      setDeliveryCursor(page.nextCursor);
    } catch (err) {
      setError(describeError(err));
    }
  }

  async function loadMoreDeliveries() {
    if (!inspecting || !deliveryCursor) return;
    setLoadingMore(true);
    try {
      const page = await webhooksApi.deliveries(inspecting.id, {
        cursor: deliveryCursor,
        filter: deliveryFilter,
      });
      setDeliveries((current) => [...current, ...page.items]);
      setDeliveryCursor(page.nextCursor);
    } catch (err) {
      setError(describeError(err));
    } finally {
      setLoadingMore(false);
    }
  }

  async function toggleDetail(deliveryID: string) {
    if (!inspecting) return;
    if (expanded === deliveryID) {
      setExpanded("");
      return;
    }
    setExpanded(deliveryID);
    setDetail(null);
    try {
      setDetail(await webhooksApi.delivery(inspecting.id, deliveryID));
    } catch (err) {
      setError(describeError(err));
    }
  }

  // Asked before the question, not after the answer: "queue a copy of
  // everything?" is a different decision at fifty accounts and at fifty
  // thousand, and the count used to appear only on the screen that reports
  // it was already queued.
  async function askForSnapshot(subscription: WebhookSubscription) {
    setSnapshotting(subscription);
    setSnapshotPreview(null);
    try {
      setSnapshotPreview(await webhooksApi.snapshotPreview(subscription.id));
    } catch {
      // Silent: the preview is context for a decision, and failing to count
      // must not stop somebody making it. The dialog simply says less.
      setSnapshotPreview(null);
    }
  }

  // Closing the dialog that showed the signing secret, and asking the
  // snapshot question if the create form asked for it.
  //
  // In that order, and not both at once. The secret appears exactly once and
  // is never served again; stacking a second dialog over it is how somebody
  // closes both and finds they never copied it.
  function closeCreated() {
    const subscription = created;
    setCreated(null);
    if (subscription && snapshotOnCreate) {
      setSnapshotOnCreate(false);
      void askForSnapshot(subscription);
    }
  }

  async function snapshot() {
    if (!snapshotting) return;
    setError("");
    const subscription = snapshotting;
    setSnapshotting(null);
    try {
      setSnapshotResult(await webhooksApi.snapshot(subscription.id));
      // The delivery list is where the pages can be watched, so the result
      // dialog is a summary rather than a progress bar: nothing here polls.
      if (inspecting?.id === subscription.id) {
        await loadDeliveries(subscription.id, deliveryFilter);
      }
    } catch (err) {
      setError(describeError(err));
    }
  }

  async function rotate() {
    if (!rotating) return;
    setError("");
    try {
      const result = await webhooksApi.rotateSecret(rotating.id);
      setRotating(null);
      // Shown through the same dialog a new subscription uses: it is the
      // same fact — a secret that appears once and is never served again.
      setCreated(result);
      await load();
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
                <Code>{subscription.url}</Code>
              </Td>
              <Td>
                {subscription.events.includes("*")
                  ? t("webhooks.allEvents")
                  : subscription.events
                      .map((event) => labelFor(t, "webhooks.event.", event))
                      .join(", ")}
              </Td>
              <Td>
                <StatusBadge status={subscription.status} />
              </Td>
              <Td>
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => void inspect(subscription)}
                  >
                    {t("webhooks.deliveries")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => setMapping(subscription)}
                  >
                    {t("fieldMappings.open")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => void toggle(subscription)}
                  >
                    {subscription.status === "ACTIVE"
                      ? t("common.disable")
                      : t("common.enable")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => void askForSnapshot(subscription)}
                  >
                    {t("webhooks.snapshot")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => setRotating(subscription)}
                  >
                    {t("webhooks.rotate")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost-danger"
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
                        <Code size="xs">{event}</Code>
                      )}
                    </label>
                  ))}
                </div>
              ))}
          </fieldset>

          <fieldset className="flex flex-col gap-2">
            <legend className="text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-medium)]">
              {t("webhooks.headers")}
            </legend>
            <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
              {t("webhooks.headersHelp")}
            </p>
            {headerRows.map((row, index) => (
              <div key={index} className="flex gap-2">
                <Input
                  aria-label={t("webhooks.headerName")}
                  placeholder={t("webhooks.headerName")}
                  value={row.name}
                  onChange={(e) =>
                    setHeaderRows(
                      headerRows.map((r, i) =>
                        i === index ? { ...r, name: e.target.value } : r,
                      ),
                    )
                  }
                />
                {/* type=password so a token pasted here is not left on
                    screen behind whoever is doing the configuring. */}
                <Input
                  type="password"
                  aria-label={t("webhooks.headerValue")}
                  placeholder={t("webhooks.headerValue")}
                  value={row.value}
                  onChange={(e) =>
                    setHeaderRows(
                      headerRows.map((r, i) =>
                        i === index ? { ...r, value: e.target.value } : r,
                      ),
                    )
                  }
                />
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  onClick={() =>
                    setHeaderRows(headerRows.filter((_, i) => i !== index))
                  }
                >
                  {t("common.delete")}
                </Button>
              </div>
            ))}
            <div>
              <Button
                type="button"
                size="sm"
                variant="secondary"
                onClick={() =>
                  setHeaderRows([...headerRows, { name: "", value: "" }])
                }
              >
                {t("webhooks.headerAdd")}
              </Button>
            </div>
          </fieldset>

          {/* Last, and unticked.

              A subscription created today has missed every change before
              it, and this is where somebody realises that — so the question
              belongs here rather than only on a button they have to know to
              look for afterwards.

              Unticked because ticking it queues the largest delivery this
              product makes, and a default that did so would make "create a
              subscription" mean something different from what it says.

              It does not queue anything on its own either. What it does is
              ask, after the signing secret has been read, through the same
              confirmation the list-page button uses — with the real counts
              in it. So this is a shortcut to a decision, not a way around
              one. */}
          <label className="flex items-start gap-2 text-[length:var(--font-size-sm)]">
            <input
              type="checkbox"
              className="mt-1"
              checked={snapshotOnCreate}
              onChange={(e) => setSnapshotOnCreate(e.target.checked)}
            />
            <span>
              <span className="font-[weight:var(--font-weight-medium)]">
                {t("webhooks.snapshotOnCreate")}
              </span>
              <br />
              <span className="text-[var(--color-fg-muted)]">
                {t("webhooks.snapshotOnCreateHint")}
              </span>
            </span>
          </label>
        </form>
      </Modal>

      <ConfirmDialog
        open={snapshotting !== null}
        title={t("webhooks.snapshotTitle")}
        message={t("webhooks.snapshotConfirm", snapshotting?.name ?? "")}
        // What it will do, and what it asks of the receiver — before the
        // button rather than after it. The reconciling requirement used to
        // appear on the screen that reports success, which is one screen too
        // late: it is a thing the receiver has to already be able to do.
        details={
          <ul className="flex flex-col gap-2 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {snapshotPreview && (
              <li className="text-[var(--color-fg)]">
                {t(
                  "webhooks.snapshotSize",
                  describeCounts(t, snapshotPreview.counts),
                  String(snapshotPreview.pages),
                )}
              </li>
            )}
            <li>{t("webhooks.snapshotWhat")}</li>
            <li>{t("webhooks.snapshotSequence")}</li>
            <li>{t("webhooks.snapshotReconcile")}</li>
            <li>{t("webhooks.snapshotCost")}</li>
          </ul>
        }
        onCancel={() => setSnapshotting(null)}
        onConfirm={() => void snapshot()}
      />

      <Modal
        open={snapshotResult !== null}
        title={t("webhooks.snapshotQueued")}
        onClose={() => setSnapshotResult(null)}
        footer={
          <Button onClick={() => setSnapshotResult(null)}>
            {t("common.close")}
          </Button>
        }
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm">
            {t(
              "webhooks.snapshotSummary",
              String(snapshotResult?.pages ?? 0),
              (snapshotResult?.scope ?? []).join(", "),
            )}
          </p>
          <Alert tone="warning">{t("webhooks.snapshotReconcile")}</Alert>
        </div>
      </Modal>

      <ConfirmDialog
        open={rotating !== null}
        title={t("webhooks.rotateTitle")}
        message={t("webhooks.rotateConfirm", rotating?.name ?? "")}
        onCancel={() => setRotating(null)}
        onConfirm={() => void rotate()}
      />

      <Modal
        open={created !== null}
        title={t("webhooks.created")}
        onClose={closeCreated}
        footer={<Button onClick={closeCreated}>{t("common.close")}</Button>}
      >
        <div className="flex flex-col gap-4">
          <Alert tone="warning">{t("webhooks.secretWarning")}</Alert>
          <CopyField
            label={t("webhooks.secret")}
            value={created?.secret ?? ""}
          />
          {created?.previousSecretExpiresAt && (
            <Alert tone="warning">
              {t(
                "webhooks.rotateOverlap",
                formatInstant(created.previousSecretExpiresAt),
              )}
            </Alert>
          )}
        </div>
      </Modal>

      <Modal
        open={inspecting !== null}
        title={t("webhooks.deliveriesFor", inspecting?.name ?? "")}
        onClose={() => setInspecting(null)}
        // Wide: this is a table of five columns, one of which is a URL and
        // another an error from somebody else's server. At the default
        // width the error wrapped to four lines and the event name to two.
        size="xl"
        footer={
          <Button onClick={() => setInspecting(null)}>
            {t("common.close")}
          </Button>
        }
      >
        <div className="mb-3 flex items-center gap-2">
          <span className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
            {t("webhooks.deliveryFilter")}
          </span>
          {/* Events first and selected by default. A full sync queues a
              hundred pages in a few seconds, and somebody opening this
              screen is almost always looking for the one event that did not
              arrive rather than for those. */}
          {(["live", "sync", "all"] as DeliveryFilter[]).map((option) => (
            <Button
              key={option}
              size="sm"
              variant={deliveryFilter === option ? "secondary" : "ghost"}
              onClick={() => {
                setDeliveryFilter(option);
                if (inspecting) void loadDeliveries(inspecting.id, option);
              }}
            >
              {t(`webhooks.filter.${option}`)}
            </Button>
          ))}
        </div>

        <Table>
          <thead>
            <tr>
              <Th>{t("webhooks.colQueuedAt")}</Th>
              <Th>{t("webhooks.colEvent")}</Th>
              <Th>{t("webhooks.colDeliveryStatus")}</Th>
              <Th>{t("webhooks.colAttempts")}</Th>
              <Th>{t("webhooks.colResponse")}</Th>
              <Th>{t("webhooks.colDetail")}</Th>
            </tr>
          </thead>
          <tbody>
            {deliveries.length === 0 && <EmptyRow colSpan={6} />}
            {deliveries.map((delivery) => (
              <tr key={delivery.id}>
                <Td>
                  <div className="whitespace-nowrap">
                    <Timestamp value={delivery.createdAt} />
                  </div>
                  {/* When it actually landed, and only when that is a
                      different fact from when it was queued: a delivery
                      that succeeded first time says nothing new here, and
                      one that succeeded on the fifth attempt an hour later
                      says quite a lot. */}
                  {delivery.deliveredAt && (
                    <div className="whitespace-nowrap text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                      {t(
                        "webhooks.deliveredAt",
                        formatInstant(delivery.deliveredAt),
                      )}
                    </div>
                  )}
                </Td>
                <Td>
                  {/* The name the subscription form and the list use. It was
                      the raw type here, which meant the one screen where
                      somebody is chasing a failure was the one screen that
                      spoke in identifiers. */}
                  <div>
                    {labelFor(t, "webhooks.event.", delivery.eventType)}
                  </div>
                  <Code>{delivery.eventType}</Code>
                </Td>
                <Td>
                  <Badge tone={deliveryTone(delivery.status)}>
                    {t(`webhooks.status.${delivery.status}`)}
                  </Badge>
                </Td>
                <Td>{delivery.attempts}</Td>
                <Td>
                  {/* The status code is the answer when there is one. The
                      transport error is what there is instead when the
                      request never reached a server, and it is somebody
                      else's sentence — clamped to two lines here rather
                      than allowed to set the height of the row. */}
                  {delivery.lastStatus !== null ? (
                    <span>{delivery.lastStatus}</span>
                  ) : (
                    <span
                      className="line-clamp-2 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]"
                      title={delivery.lastError}
                    >
                      {delivery.lastError}
                    </span>
                  )}
                </Td>
                <Td>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => void toggleDetail(delivery.id)}
                  >
                    {expanded === delivery.id
                      ? t("webhooks.detailClose")
                      : t("webhooks.detailOpen")}
                  </Button>
                </Td>
              </tr>
            ))}
          </tbody>
        </Table>

        {/* Below the table rather than inside it. A row that expands into a
            second row of the same table has to fake a colspan and loses the
            column alignment for everything under it; this keeps the table a
            table and puts the bodies where there is room for them. */}
        {expanded !== "" && (
          <div className="mt-4 rounded-[var(--radius-sm)] border border-[var(--color-border)] p-3">
            {detail === null ? (
              <p className="text-[var(--color-fg-muted)]">
                {t("common.loading")}
              </p>
            ) : (
              <div className="flex flex-col gap-3">
                <div>
                  <div className="mb-1 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                    {t("webhooks.detailRequest")}
                  </div>
                  <pre className="max-h-60 overflow-auto rounded-[var(--radius-sm)] bg-[var(--color-bg-soft)] p-2 text-[length:var(--font-size-sm)]">
                    {detail.payload}
                  </pre>
                </div>
                <div>
                  <div className="mb-1 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                    {t("webhooks.detailResponse")}
                  </div>
                  {detail.response === "" ? (
                    <p className="text-[length:var(--font-size-sm)]">
                      {t("webhooks.detailResponseEmpty")}
                    </p>
                  ) : (
                    <>
                      <pre className="max-h-60 overflow-auto rounded-[var(--radius-sm)] bg-[var(--color-bg-soft)] p-2 text-[length:var(--font-size-sm)]">
                        {detail.response}
                      </pre>
                      {detail.response.length >= detail.responseCap && (
                        <p className="mt-1 text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
                          {t(
                            "webhooks.detailTruncated",
                            String(detail.responseCap),
                          )}
                        </p>
                      )}
                    </>
                  )}
                </div>
              </div>
            )}
          </div>
        )}

        {deliveryCursor !== "" && (
          <div className="mt-3">
            <Button
              variant="secondary"
              disabled={loadingMore}
              onClick={() => void loadMoreDeliveries()}
            >
              {loadingMore ? t("common.loading") : t("webhooks.loadMore")}
            </Button>
          </div>
        )}
      </Modal>

      <ConfirmDialog
        open={deleting !== null}
        title={t("webhooks.confirmDeleteTitle")}
        message={t("webhooks.confirmDelete", deleting?.name ?? "")}
        destructive
        onConfirm={() => void remove()}
        onCancel={() => setDeleting(null)}
      />

      {mapping ? (
        <FieldMappingEditor
          kind="webhook"
          recipientId={mapping.id}
          recipientName={mapping.name}
          onClose={() => setMapping(null)}
        />
      ) : null}
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
