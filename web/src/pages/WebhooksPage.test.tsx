import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithLanguage } from "../test/render";

/**
 * The event picker, which used to be a column of bare wire identifiers.
 *
 * Two properties, and the second is the one that will break quietly. An
 * administrator has to be able to read what they are subscribing to, in
 * their own language — and whoever writes the receiver still has to see the
 * literal string, so the label cannot replace it.
 *
 * The other is that this list is open-ended by design: the wildcard says
 * "including event types later versions add", and a server can be newer
 * than the console talking to it. t falls back to the key it was handed, so
 * without a fallback of our own an unknown event renders as
 * "webhooks.event.something.new" — worse than the identifier it replaced.
 */

const list = vi.fn();
const events = vi.fn();
const snapshot = vi.fn();

vi.mock("../api/endpoints", () => ({
  webhooksApi: {
    list: () => list(),
    events: () => events(),
    create: vi.fn(),
    remove: vi.fn(),
    enable: vi.fn(),
    disable: vi.fn(),
    deliveries: vi.fn(),
    snapshot: (id: string) => snapshot(id),
  },
}));

const { WebhooksPage } = await import("./WebhooksPage");

beforeEach(() => {
  vi.clearAllMocks();
  list.mockResolvedValue([]);
  events.mockResolvedValue([
    "user.created",
    "user.locked",
    "organization.disabled",
  ]);
});

async function openTheDialog(language: "en-US" | "zh-CN") {
  renderWithLanguage(<WebhooksPage />, language);
  await userEvent.click(
    await screen.findByRole("button", {
      name: language === "zh-CN" ? /新建订阅/ : /New subscription/i,
    }),
  );
}

describe("choosing which events to receive", () => {
  it("says what each event means, in the reader's language", async () => {
    await openTheDialog("zh-CN");

    expect(await screen.findByText("连续登录失败被锁定")).toBeTruthy();
    expect(screen.getByText("组织停用")).toBeTruthy();
  });

  it("still shows the identifier the receiver has to match on", async () => {
    await openTheDialog("zh-CN");

    // Beside the translation, not instead of it: a reader who only saw
    // "连续登录失败被锁定" would have to guess at the string their code
    // compares against.
    expect(await screen.findByText("user.locked")).toBeTruthy();
    expect(screen.getByText("连续登录失败被锁定")).toBeTruthy();
  });

  it("falls back to the identifier for an event it has no label for", async () => {
    events.mockResolvedValue(["user.created", "future.thing_we_added_later"]);
    await openTheDialog("en-US");

    expect(await screen.findByText("future.thing_we_added_later")).toBeTruthy();
    expect(
      screen.queryByText("webhooks.event.future.thing_we_added_later"),
      "an unlabelled event rendered its translation key",
    ).toBeNull();
  });

  it("groups the events by what they happen to", async () => {
    await openTheDialog("zh-CN");

    expect(await screen.findByText("账号")).toBeTruthy();
    expect(screen.getByText("组织")).toBeTruthy();
  });
});

describe("sending a full sync", () => {
  const subscription = {
    id: "sub-1",
    name: "mirror",
    url: "https://203.0.113.10/portico",
    events: "*",
    status: "ACTIVE",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  };

  it("asks first, because this is the largest delivery the product makes", async () => {
    list.mockResolvedValue([subscription]);
    renderWithLanguage(<WebhooksPage />, "zh-CN");

    await userEvent.click(
      await screen.findByRole("button", { name: /全量同步/ }),
    );

    // Nothing has been queued yet. A button that fired on the first click
    // would send every account in the tenant to somebody's endpoint because
    // an operator's mouse slipped.
    expect(snapshot).not.toHaveBeenCalled();
    expect(
      await screen.findByText(/全部账号、组织与用户组的副本/),
    ).toBeTruthy();
    // What it asks of the receiver belongs here, before the button, rather
    // than only on the screen that reports success — it is a thing the
    // receiver has to already be able to do.
    expect(screen.getByText(/按 id 对账/)).toBeTruthy();
  });

  it("reports what it queued, and what the receiver has to do about it", async () => {
    list.mockResolvedValue([subscription]);
    snapshot.mockResolvedValue({
      syncId: "run-1",
      scope: ["user", "group"],
      counts: { user: 55, group: 4 },
      pages: 2,
    });
    renderWithLanguage(<WebhooksPage />, "zh-CN");

    await userEvent.click(
      await screen.findByRole("button", { name: /全量同步/ }),
    );
    await userEvent.click(
      await screen.findByRole("button", { name: /确认|确定/ }),
    );

    expect(snapshot).toHaveBeenCalledWith("sub-1");
    expect(await screen.findByText(/已排队 2 次分页投递/)).toBeTruthy();
    // Said again on the screen that reports it went, because this is the
    // moment somebody is about to tell their counterpart what to expect.
    expect(screen.getByText(/按 id 对账/)).toBeTruthy();
  });
});
