import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AppIcon, Field, Input } from "./ui";

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

/**
 * The application tile's picture.
 *
 * The security property is the rendering, not the value: a logo may be an
 * SVG, and an SVG is a document that can carry script. A browser does not run
 * that script when the file is loaded through `<img>`, which is the entire
 * reason the server is willing to accept a whole SVG document here.
 *
 * So this asserts the element type. It looks like a test of an implementation
 * detail and is not: a later change that inlines the file to recolour it with
 * CSS would keep every visible behaviour and turn every registered logo into
 * stored cross-site scripting on everybody's home screen.
 */
describe("AppIcon", () => {
  it("renders a registered logo through an image element", () => {
    const { container } = render(
      <AppIcon name="Internal Wiki" src="/icons/wiki.svg" />,
    );

    const img = container.querySelector("img");
    expect(img, "a logo is not being rendered through <img>").not.toBeNull();
    expect(img).toHaveAttribute("src", "/icons/wiki.svg");
    // An external logo would otherwise report the address of the page every
    // visitor opened it from to whoever hosts it.
    expect(img).toHaveAttribute("referrerpolicy", "no-referrer");
  });

  it("falls back to the first character when there is no logo", () => {
    const { container } = render(<AppIcon name="内部 Wiki" />);

    expect(container.querySelector("img")).toBeNull();
    expect(container.textContent).toBe("内");
  });

  it("gives the same name the same colour every time", () => {
    // Not decoration: people navigate a wall of tiles by colour, so one that
    // changes between visits or when the list is re-sorted is worse than no
    // colour at all.
    const first = render(<AppIcon name="Helpdesk" />).container.innerHTML;
    const second = render(<AppIcon name="Helpdesk" />).container.innerHTML;
    expect(first).toBe(second);
  });
});
