import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithLanguage } from "../test/render";

/**
 * The organization filter, which is a tree now.
 *
 * What is asserted here is the argument that leaves for the server, not the
 * markup — because both ways this control can be wrong are invisible on
 * screen. It renders identically whether it sends the right organization or
 * its parent's id, and identically whether it sends the reserved value the
 * server actually reserves or a near miss. The second is the worse one: a
 * drifted sentinel comes back as an empty page, which reads as "nobody is
 * unfiled" rather than as a bug, and there is no error anywhere to find.
 *
 * This is the same shape as the defect ApplicationsPage.test.tsx pins — a
 * control passing an identifier that type-checks perfectly and means
 * something else.
 */

const listUsers = vi.fn();
const listOrganizations = vi.fn();
const listGroups = vi.fn();

vi.mock("../api/endpoints", async () => {
  // The sentinel is imported by the page from this module, and mocking the
  // module away would take the real value with it — leaving a test that
  // asserts the page agrees with its own mock. The real one is re-exported
  // so the assertion below is against the value the application ships.
  const actual =
    await vi.importActual<typeof import("../api/endpoints")>(
      "../api/endpoints",
    );
  return {
    UNASSIGNED_ORGANIZATION: actual.UNASSIGNED_ORGANIZATION,
    userApi: {
      list: (params: unknown) => listUsers(params),
    },
    organizationApi: {
      list: () => listOrganizations(),
    },
    groupsApi: {
      ofUser: () => listGroups(),
    },
  };
});

const { UsersPage } = await import("./UsersPage");

const ENGINEERING = "6f1c1f4e-0000-4000-8000-000000000001";
const PLATFORM = "6f1c1f4e-0000-4000-8000-000000000002";

function organization(id: string, name: string, parentId: string) {
  return {
    id,
    name,
    code: name.toUpperCase(),
    remark: "",
    status: "ACTIVE" as const,
    sortOrder: 0,
    userCount: 0,
    parentId,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  listUsers.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 20 });
  listOrganizations.mockResolvedValue([
    organization(ENGINEERING, "Engineering", ""),
    organization(PLATFORM, "Platform", ENGINEERING),
  ]);
  listGroups.mockResolvedValue([]);
});

/** The organizationId the most recent listing was asked for. */
function lastRequestedOrganization(): unknown {
  const calls = listUsers.mock.calls;
  return (calls[calls.length - 1][0] as { organizationId?: string })
    .organizationId;
}

async function renderPage() {
  renderWithLanguage(<UsersPage />);
  // The chart arrives on its own request, so the tree is not there on the
  // first paint.
  await screen.findByRole("button", { name: /Engineering/ });
}

describe("the organization filter", () => {
  it("asks for the organization that was clicked, not its parent", async () => {
    await renderPage();

    await userEvent.click(screen.getByRole("button", { name: /Platform/ }));

    await waitFor(() => {
      expect(lastRequestedOrganization()).toBe(PLATFORM);
    });
  });

  it("asks for the unfiled with the value the server reserves", async () => {
    const { UNASSIGNED_ORGANIZATION } = await import("../api/endpoints");
    await renderPage();

    await userEvent.click(screen.getByRole("button", { name: /Not in one/ }));

    await waitFor(() => {
      expect(lastRequestedOrganization()).toBe(UNASSIGNED_ORGANIZATION);
    });
    // Named outright as well as compared. If the constant is ever changed on
    // this side alone, the comparison above still passes and this does not —
    // and "none" is what internal/service/user.go reserves.
    expect(UNASSIGNED_ORGANIZATION).toBe("none");
  });

  it("shows the chart as a chart, with a child under its parent", async () => {
    await renderPage();

    const nodes = screen
      .getAllByRole("button")
      .map((button) => button.textContent ?? "");
    const engineering = nodes.findIndex((text) => text.includes("Engineering"));
    const platform = nodes.findIndex((text) => text.includes("Platform"));

    expect(engineering).toBeGreaterThanOrEqual(0);
    expect(platform).toBeGreaterThan(engineering);
  });

  it("goes back to every organization, which is not the same as unfiled", async () => {
    await renderPage();

    await userEvent.click(screen.getByRole("button", { name: /Platform/ }));
    await waitFor(() => expect(lastRequestedOrganization()).toBe(PLATFORM));

    await userEvent.click(screen.getByRole("button", { name: /^All$/ }));

    await waitFor(() => {
      // Empty, not the sentinel: "all" and "none" are opposite questions and
      // the control has to be able to say both.
      expect(lastRequestedOrganization()).toBe("");
    });
  });
});
