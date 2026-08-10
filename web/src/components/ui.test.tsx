import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { LanguageProvider } from "../i18n";
import { AppIcon, Field, GuidePanel, Input, Modal } from "./ui";

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
 * The dialog's chrome.
 *
 * Both assertions below describe the same defect seen from two sides. The
 * dialog used to carry `overflow-y-auto` on its outermost element, so a form
 * longer than the viewport scrolled its own title bar and its own footer off
 * the top and bottom: the person filling in a long registration form could
 * see neither what they were filling in nor the button that saves it.
 *
 * That is also why the close button is tested for by role rather than merely
 * rendered. A close button that lives in a header which scrolls away is not a
 * way out of the dialog; it is a way out only while nobody has scrolled.
 */
describe("Modal", () => {
  function renderModal(onClose = vi.fn()) {
    const result = render(
      <LanguageProvider>
        <Modal
          open
          title="Register client"
          onClose={onClose}
          footer={<button>Save</button>}
        >
          <p>Body</p>
        </Modal>
      </LanguageProvider>,
    );
    return { ...result, onClose };
  }

  it("closes when the close button is pressed", async () => {
    const { onClose } = renderModal();

    // Matched in either language: which one the provider picks depends on the
    // environment's locale, and this test is about the button, not the wording.
    await userEvent.click(screen.getByRole("button", { name: /关闭|Close/ }));

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("scrolls the body, not the whole dialog", () => {
    renderModal();

    const dialog = screen.getByRole("dialog");
    // The outermost element scrolling is what took the header with it.
    expect(
      dialog.className,
      "the dialog itself scrolls, so its header and footer scroll away with the content",
    ).not.toContain("overflow-y-auto");

    const scroller = dialog.querySelector(".overflow-y-auto");
    expect(
      scroller,
      "nothing in the dialog scrolls, so long forms are unreachable",
    ).not.toBeNull();
    // The scrolling region holds the content and nothing else: if it also
    // contained the header, the header would still scroll away.
    expect(scroller).toHaveTextContent("Body");
    expect(scroller).not.toHaveTextContent("Register client");
    expect(scroller).not.toHaveTextContent("Save");
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

/**
 * A guide panel's text is one string with a shape: the first line is the
 * lead, every line after it is "label::text". The shape is what makes the
 * panel scannable — a paragraph cut into three bullets is still three
 * things to read before finding out whether any is the one you came for.
 *
 * Pinned because both failures are silent. A missing "::" renders the whole
 * line as unlabelled body, which looks like prose somebody wrote that way;
 * and a body with no lines after the first has to keep working, or a panel
 * with nothing worth itemizing is forced to invent points.
 */
describe("a guide panel's structure", () => {
  it("gives every point a label that can be scanned", () => {
    render(
      <LanguageProvider>
        <GuidePanel id="test-guide" title="Guide">
          {
            "The lead sentence.\nFirst label::what it means\nSecond label::and this"
          }
        </GuidePanel>
      </LanguageProvider>,
    );

    expect(screen.getByText("The lead sentence.")).toBeTruthy();
    // The label is its own element, which is what carries the weight that
    // makes it skippable. Text alone would pass with the label inlined.
    expect(screen.getByText("First label").tagName).toBe("STRONG");
    expect(screen.getByText("Second label").tagName).toBe("STRONG");
    // Each point's text sits beside its label, one list item per point.
    expect(screen.getByText(/what it means/)).toBeTruthy();
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
  });

  it("keeps a line that has no label rather than dropping it", () => {
    render(
      <LanguageProvider>
        <GuidePanel id="test-guide-2" title="Guide">
          {"Lead.\nA line somebody forgot to label"}
        </GuidePanel>
      </LanguageProvider>,
    );

    expect(screen.getByText("A line somebody forgot to label")).toBeTruthy();
  });

  it("still renders a body that is only a lead", () => {
    render(
      <LanguageProvider>
        <GuidePanel id="test-guide-3" title="Guide">
          {"Just one sentence, no points."}
        </GuidePanel>
      </LanguageProvider>,
    );

    expect(screen.getByText("Just one sentence, no points.")).toBeTruthy();
    expect(screen.queryByRole("list")).toBeNull();
  });
});
