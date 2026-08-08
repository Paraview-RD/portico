import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Field, Input } from "./ui";

/**
 * Field's accessible name and description.
 *
 * These are here rather than in the browser suite because the browser suite
 * addresses fields by a substring of their label — it has to, since the
 * rendered label carries a required marker — and so it would keep passing if
 * the marker crept back into the accessible name. This is the assertion that
 * fails in that case.
 *
 * Both defects below were real, and both were found by pointing a browser at
 * the running application rather than by reading the component.
 */
describe("Field", () => {
  it("keeps the required marker out of the accessible name", () => {
    const { container } = render(
      <Field label="Password" required>
        <Input type="password" />
      </Field>,
    );

    // Queried by selector rather than by label because a password input has
    // no ARIA role, and because getByLabelText matches the label's text
    // content — which does contain the asterisk, since aria-hidden removes
    // an element from the accessibility tree without removing its text.
    // The accessible name is the thing under test, so it is asserted
    // directly: exactly "Password", not "Password *".
    const input = container.querySelector('input[type="password"]');
    expect(input).toHaveAccessibleName("Password");
  });

  it("attaches the hint as a description, not as part of the name", () => {
    render(
      <Field label="Username" hint="Any of the three reaches the same account.">
        <Input />
      </Field>,
    );

    const input = screen.getByRole("textbox", { name: "Username" });
    // A wrapping label contributes everything inside it to the accessible
    // name, so the hint used to be read out as though it were the field's
    // name, on every focus.
    expect(input).toHaveAccessibleName("Username");
    expect(input).toHaveAccessibleDescription(
      "Any of the three reaches the same account.",
    );
  });

  it("describes by the error instead of the hint when both are given", () => {
    render(
      <Field label="Email" hint="Optional." error="That address is not valid.">
        <Input />
      </Field>,
    );

    const input = screen.getByRole("textbox", { name: "Email" });
    // The hint is not rendered when there is an error, so describing by it
    // would point assistive technology at an element that is not on the page
    // — which is announced as nothing at all, losing the error too.
    expect(input).toHaveAccessibleDescription("That address is not valid.");
  });
});
