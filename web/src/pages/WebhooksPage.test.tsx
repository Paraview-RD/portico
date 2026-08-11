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

vi.mock("../api/endpoints", () => ({
  webhooksApi: {
    list: () => list(),
    events: () => events(),
    create: vi.fn(),
    remove: vi.fn(),
    enable: vi.fn(),
    disable: vi.fn(),
    deliveries: vi.fn(),
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
