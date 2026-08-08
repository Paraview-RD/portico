import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithLanguage } from "../test/render";

/**
 * Regressions from the browser pass, pinned so they stay fixed.
 *
 * Neither of these was caught by `tsc` or by any Go test, and both were the
 * kind of thing a browser shows once and nobody thinks to check again:
 *
 *  - the tab pattern was half-built — `role="tab"` with no panel, which
 *    announces itself as a tab and then leads nowhere;
 *  - the status buttons passed the SAML entity id where the API expects the
 *    registration's own id, which type-checks cleanly because both are
 *    strings and 404s at runtime.
 */

const listOAuth = vi.fn();
const listSAML = vi.fn();
const listCAS = vi.fn();
const enableSAML = vi.fn();
const disableSAML = vi.fn();
const disableCAS = vi.fn();
const integrationEndpoints = vi.fn();

vi.mock("../api/endpoints", () => ({
  applicationApi: {
    integrationEndpoints: () => integrationEndpoints(),
    oauth: { list: () => listOAuth() },
    saml: {
      list: () => listSAML(),
      enable: (id: string) => enableSAML(id),
      disable: (id: string) => disableSAML(id),
    },
    cas: {
      list: () => listCAS(),
      disable: (id: string) => disableCAS(id),
    },
  },
}));

const { ApplicationsPage } = await import("./ApplicationsPage");

const serviceProvider = {
  id: "6f1c1f4e-0000-4000-8000-000000000001",
  tenantId: "t",
  entityId: "https://sp.example.com/saml/metadata",
  name: "Example SP",
  metadataXml: "<EntityDescriptor/>",
  acsUrls: ["https://sp.example.com/acs"],
  status: "ACTIVE" as const,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const casService = {
  id: "6f1c1f4e-0000-4000-8000-000000000002",
  tenantId: "t",
  name: "Wiki",
  urlPrefix: "https://wiki.example.com/",
  status: "ACTIVE" as const,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

beforeEach(() => {
  vi.clearAllMocks();
  listOAuth.mockResolvedValue([]);
  listSAML.mockResolvedValue([serviceProvider]);
  listCAS.mockResolvedValue([casService]);
  enableSAML.mockResolvedValue(serviceProvider);
  disableSAML.mockResolvedValue({ ...serviceProvider, status: "DISABLED" });
  disableCAS.mockResolvedValue({ ...casService, status: "DISABLED" });
  integrationEndpoints.mockRejectedValue(new Error("not needed here"));
});

/** Opens a protocol tab and waits for its table to arrive. */
async function openTab(name: RegExp) {
  const user = userEvent.setup();
  const tab = await screen.findByRole("tab", { name });
  await user.click(tab);
  return { user, tab };
}

/** Clicks a row action and confirms the dialog it opens. */
async function actAndConfirm(
  user: ReturnType<typeof userEvent.setup>,
  label: RegExp,
) {
  await user.click(await screen.findByRole("button", { name: label }));
  await user.click(await screen.findByRole("button", { name: /^Confirm$/ }));
}

describe("ApplicationsPage tabs", () => {
  it("gives every tab a panel that names it back", async () => {
    renderWithLanguage(<ApplicationsPage />);

    const tabs = await screen.findAllByRole("tab");
    expect(tabs).toHaveLength(3);

    for (const tab of tabs) {
      const controls = tab.getAttribute("aria-controls");
      expect(controls, `${tab.textContent} has no aria-controls`).toBeTruthy();

      // A role="tab" pointing at nothing is worse than a plain button: it
      // announces itself as a tab and then leads nowhere.
      const panel = document.getElementById(controls as string);
      expect(
        panel,
        `${tab.textContent} points at a panel that does not exist`,
      ).not.toBeNull();
      expect(panel).toHaveAttribute("role", "tabpanel");
    }

    // And the panel is labelled by whichever tab is currently selected.
    const selected = tabs.find(
      (t) => t.getAttribute("aria-selected") === "true",
    );
    const panel = screen.getByRole("tabpanel");
    expect(panel).toHaveAttribute("aria-labelledby", selected?.id);
  });

  it("keeps the count out of the accessible name", async () => {
    renderWithLanguage(<ApplicationsPage />);

    // The visible count sits next to the label. Left in the accessible name
    // it reads as "SAML 2.01", which is a different string every time a
    // registration is added.
    expect(await screen.findByRole("tab", { name: "SAML 2.0" })).toBeVisible();
    expect(await screen.findByRole("tab", { name: "CAS" })).toBeVisible();
  });
});

describe("ApplicationsPage status changes", () => {
  it("addresses a SAML service provider by its id, not its entity id", async () => {
    renderWithLanguage(<ApplicationsPage />);

    const { user } = await openTab(/SAML/);
    await actAndConfirm(user, /^Disable$/);

    await waitFor(() => expect(disableSAML).toHaveBeenCalledTimes(1));

    // The entity id is a URI. Sent in a path it has to be percent-encoded,
    // and a reverse proxy that normalizes paths decodes the %2F and splits
    // it — 404 in production, nowhere else. Both are strings, so passing the
    // wrong one type-checks.
    expect(disableSAML).toHaveBeenCalledWith(serviceProvider.id);
    expect(disableSAML).not.toHaveBeenCalledWith(serviceProvider.entityId);
  });

  it("addresses a CAS service by its id, not its URL prefix", async () => {
    renderWithLanguage(<ApplicationsPage />);

    const { user } = await openTab(/^CAS$/);
    await actAndConfirm(user, /^Disable$/);

    await waitFor(() => expect(disableCAS).toHaveBeenCalledTimes(1));
    expect(disableCAS).toHaveBeenCalledWith(casService.id);
    expect(disableCAS).not.toHaveBeenCalledWith(casService.urlPrefix);
  });

  it("asks before changing anything", async () => {
    renderWithLanguage(<ApplicationsPage />);

    const { user } = await openTab(/SAML/);
    await user.click(await screen.findByRole("button", { name: /^Disable$/ }));

    // The dialog is open and nothing has happened yet.
    expect(
      await screen.findByRole("button", { name: /^Confirm$/ }),
    ).toBeVisible();
    expect(disableSAML).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: /^Cancel$/ }));
    expect(disableSAML).not.toHaveBeenCalled();
  });
});
