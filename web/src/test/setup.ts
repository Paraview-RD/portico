/**
 * Test setup, loaded before every component test file.
 *
 * jest-dom adds the assertions that make a failure readable — "expected
 * element to be visible" rather than "expected null not to be null".
 */
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// React Testing Library mounts into a container it appends to document.body.
// Without this every test after the first sees the previous test's DOM as
// well as its own, and a query that should be unambiguous finds two matches.
afterEach(cleanup);
