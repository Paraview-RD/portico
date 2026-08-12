import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import { renderWithLanguage } from "../test/render";
import { UserAttributeValues } from "./UserAttributeValues";
import type { UserAttributeDefinition } from "../api/types";

/**
 * The account form is the only place a tenant's own attributes are collected,
 * and the only place two of their properties exist at all: `required` is not
 * enforced by the server, and the canonical form of a boolean or a date is
 * decided by the control that gathers it. Both are asserted here because
 * nothing downstream would notice them missing — a form that quietly drops
 * `required` looks exactly like one that has none.
 */

function definition(
  overrides: Partial<UserAttributeDefinition>,
): UserAttributeDefinition {
  return {
    id: `id-${overrides.key}`,
    tenantId: "t",
    key: "badge",
    label: "Badge",
    kind: "TEXT",
    required: false,
    sortOrder: 0,
    ...overrides,
  };
}

it("says so rather than showing an empty box when nothing is defined", () => {
  renderWithLanguage(
    <UserAttributeValues definitions={[]} values={{}} onChange={vi.fn()} />,
  );

  expect(
    screen.getByText(/has not defined any attributes/i),
  ).toBeInTheDocument();
});

it("carries required through to the control, since only the form enforces it", () => {
  renderWithLanguage(
    <UserAttributeValues
      definitions={[
        definition({ key: "badge", label: "Badge", required: true }),
        definition({ key: "desk", label: "Desk", required: false }),
      ]}
      values={{}}
      onChange={vi.fn()}
    />,
  );

  expect(screen.getByLabelText(/Badge/)).toBeRequired();
  expect(screen.getByLabelText(/Desk/)).not.toBeRequired();
});

it("offers a boolean as three states, because two cannot hold 'never asked'", async () => {
  const onChange = vi.fn();
  renderWithLanguage(
    <UserAttributeValues
      definitions={[
        definition({ key: "on_call", label: "On call", kind: "BOOLEAN" }),
      ]}
      values={{}}
      onChange={onChange}
    />,
  );

  const control = screen.getByLabelText(/On call/);
  // Not set, yes, no — a checkbox would collapse the first two into one.
  expect(control).toHaveValue("");
  expect(screen.getAllByRole("option")).toHaveLength(3);

  // And the value sent is the canonical one the server stores, not the word
  // on screen: "Yes" in one language and "是" in another must arrive as the
  // same fact.
  await userEvent.selectOptions(control, "true");
  expect(onChange).toHaveBeenCalledWith("on_call", "true");
});

it("collects a date as a date and a number as a number", () => {
  renderWithLanguage(
    <UserAttributeValues
      definitions={[
        definition({ key: "starts_on", label: "Starts on", kind: "DATE" }),
        definition({ key: "headcount", label: "Headcount", kind: "NUMBER" }),
      ]}
      values={{ starts_on: "2026-03-01", headcount: "3" }}
      onChange={vi.fn()}
    />,
  );

  // The server stores a date only, and this is what keeps two tenants from
  // recording the same fact in two formats.
  expect(screen.getByLabelText(/Starts on/)).toHaveAttribute("type", "date");
  const number = screen.getByLabelText(/Headcount/);
  expect(number).toHaveAttribute("type", "number");
  // Any decimal: the default step of 1 would have the browser refuse "1.5"
  // with nothing on the page to say why.
  expect(number).toHaveAttribute("step", "any");
});

it("restricts a select to the values the definition permits", () => {
  renderWithLanguage(
    <UserAttributeValues
      definitions={[
        definition({
          key: "shift",
          label: "Shift",
          kind: "SELECT",
          allowedValues: ["Early", "Late"],
        }),
      ]}
      values={{ shift: "Late" }}
      onChange={vi.fn()}
    />,
  );

  expect(screen.getByLabelText(/Shift/)).toHaveValue("Late");
  // The two permitted values and the way back out of them.
  expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual([
    "Not set",
    "Early",
    "Late",
  ]);
});
