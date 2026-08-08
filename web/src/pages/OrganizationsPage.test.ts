import { describe, expect, it } from "vitest";

import type { Organization } from "../api/types";
import { arrangeAsTree } from "./OrganizationsPage";

/**
 * Flattening a tree for a table is the kind of thing that looks obviously
 * right and quietly drops rows. These pin the two ways it can:
 * an organization whose parent is not in the list, and a cycle the server
 * should never send but which must not hang the browser if it does.
 */

function org(id: string, parentId = ""): Organization {
  return {
    id,
    name: id,
    code: id,
    remark: "",
    parentId,
    status: "ACTIVE",
    sortOrder: 0,
    userCount: 0,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  };
}

describe("arrangeAsTree", () => {
  it("puts every child directly after its parent, indented", () => {
    const rows = arrangeAsTree([
      org("hq"),
      org("eng", "hq"),
      org("platform", "eng"),
      org("sales", "hq"),
    ]);

    expect(rows.map((r) => [r.org.id, r.depth])).toEqual([
      ["hq", 0],
      ["eng", 1],
      ["platform", 2],
      ["sales", 1],
    ]);
  });

  it("keeps a row whose parent is missing, as a root", () => {
    // A parent can be absent because it was filtered out. Dropping the child
    // would leave an administrator unable to find an organization that
    // plainly exists, with nothing on screen to say why.
    const rows = arrangeAsTree([org("orphan", "not-in-this-list"), org("hq")]);

    expect(rows.map((r) => r.org.id).sort()).toEqual(["hq", "orphan"]);
    expect(rows.every((r) => r.depth === 0)).toBe(true);
  });

  it("returns every row exactly once", () => {
    const input = [org("a"), org("b", "a"), org("c", "b"), org("d")];
    const rows = arrangeAsTree(input);

    expect(rows).toHaveLength(input.length);
    expect(new Set(rows.map((r) => r.org.id)).size).toBe(input.length);
  });

  it("does not hang on a cycle the server should never send", () => {
    // The server refuses to write one, and a foreign key cannot catch one,
    // so "should never" is doing real work. If it ever arrives, the list
    // must render something rather than spin: a blank screen with a pegged
    // CPU is the worst way for this to fail.
    const rows = arrangeAsTree([org("a", "b"), org("b", "a")]);

    // Neither is reachable from a root, so neither is emitted — but the call
    // returns, which is the property under test.
    expect(rows.length).toBeLessThanOrEqual(2);
  });
});
