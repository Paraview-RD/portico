import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";

import { renderWithLanguage } from "../test/render";

/**
 * The editor draws every field in the catalogue, and most of them will never
 * be configured. So the property worth pinning is what a save sends: only the
 * rows somebody said something about.
 *
 * A save replaces the whole set, which means a row sent by accident is not a
 * stray extra — it is a rule that now exists and did not before. And a row
 * left out by accident is a rule somebody wrote that quietly vanished.
 */

const listFields = vi.fn();
const listMappings = vi.fn();
const replace = vi.fn();

vi.mock("../api/endpoints", () => ({
  fieldsApi: { list: () => listFields() },
  fieldMappingsApi: {
    list: (kind: string, id: string) => listMappings(kind, id),
    replace: (kind: string, id: string, mappings: unknown) =>
      replace(kind, id, mappings),
  },
}));

const { FieldMappingEditor } = await import("./FieldMappingEditor");

const catalogue = [
  {
    key: "email",
    group: "identity",
    kind: "TEXT",
    custom: false,
    inbound: true,
  },
  {
    key: "phone",
    group: "identity",
    kind: "TEXT",
    custom: false,
    inbound: true,
  },
  {
    key: "department",
    group: "profile",
    kind: "TEXT",
    custom: false,
    inbound: true,
  },
];

beforeEach(() => {
  vi.clearAllMocks();
  listFields.mockResolvedValue(catalogue);
  listMappings.mockResolvedValue([]);
  replace.mockResolvedValue([]);
});

function render() {
  return renderWithLanguage(
    <FieldMappingEditor
      kind="webhook"
      recipientId="sub-1"
      recipientName="HR"
      onClose={() => {}}
    />,
    "en-US",
  );
}

// A save with nothing filled in sends an empty set, which restores the
// defaults. It must not send a row per field just because a row was drawn.
it("sends nothing for the fields nobody configured", async () => {
  render();
  await screen.findByText("email");

  await userEvent.click(screen.getByRole("button", { name: "Save" }));

  expect(replace).toHaveBeenCalledWith("webhook", "sub-1", []);
});

// A name typed into one row is the only rule sent.
it("sends only the row that was given a name", async () => {
  render();
  const rows = await screen.findAllByRole("textbox");

  await userEvent.type(rows[0], "mail");
  await userEvent.click(screen.getByRole("button", { name: "Save" }));

  expect(replace).toHaveBeenCalledWith("webhook", "sub-1", [
    { sourceKey: "email", targetName: "mail" },
  ]);
});

// Suppression is sent as a flag with no name, and it clears a name that was
// already typed — the two are different intentions and a row cannot hold both.
it("sends a suppression as a flag and drops any name beside it", async () => {
  render();
  const rows = await screen.findAllByRole("textbox");
  await userEvent.type(rows[1], "mobile");

  const boxes = screen.getAllByRole("checkbox");
  await userEvent.click(boxes[1]);
  await userEvent.click(screen.getByRole("button", { name: "Save" }));

  expect(replace).toHaveBeenCalledWith("webhook", "sub-1", [
    { sourceKey: "phone", suppressed: true },
  ]);
});

// Existing rules come back into the form, so a save that follows does not
// silently delete what somebody configured last time.
it("loads the rules already saved", async () => {
  listMappings.mockResolvedValue([
    { sourceKey: "department", targetName: "dept" },
  ]);
  render();

  await screen.findByDisplayValue("dept");
  await userEvent.click(screen.getByRole("button", { name: "Save" }));

  expect(replace).toHaveBeenCalledWith("webhook", "sub-1", [
    { sourceKey: "department", targetName: "dept" },
  ]);
});

// Typed, then thought better of it.
//
// The row still exists in the form's state — that is what makes this
// different from a row nobody touched — and sending it would be a rule with
// no name, which the server refuses. The person would see a validation error
// naming a field they had just decided not to configure.
it("does not send a row whose name was typed and then cleared", async () => {
  render();
  const rows = await screen.findAllByRole("textbox");

  await userEvent.type(rows[0], "mail");
  await userEvent.clear(rows[0]);
  await userEvent.click(screen.getByRole("button", { name: "Save" }));

  expect(replace).toHaveBeenCalledWith("webhook", "sub-1", []);
});
