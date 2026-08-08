import { defineConfig, devices } from "@playwright/test";

/**
 * Browser tests, run against the real binary.
 *
 * The distinction from the component tests in `src/**​/*.test.tsx` is not
 * "slower and more thorough". It is that these run in a browser against the
 * Go server, and there is a class of defect that only exists there:
 *
 *  - The Content-Security-Policy is set by Go middleware. The Vite dev
 *    server does not send it, and jsdom does not enforce it. A policy that
 *    blocks the entire page passed eleven Go tests and every component test,
 *    and broke every SAML sign-in in a real browser. That is the reason this
 *    harness exists, and the reason it must run against the binary rather
 *    than against `npm run dev` — pointing it at the dev server would
 *    reproduce exactly the blind spot it is here to remove.
 *  - The frontend is embedded into the binary with go:embed. Testing the
 *    dev server tests a bundle nobody deploys.
 *  - Whether a form actually submits, a control actually points where it
 *    reads as pointing, and a page renders anything at all are all questions
 *    a DOM implementation answers optimistically.
 *
 * The server is started by Playwright, so a run needs a built binary and a
 * database. See CONTRIBUTING.md; the short version is:
 *
 *   npm run build && cd .. && go build -o portico ./cmd/server
 *   PORTICO_E2E_DB_DSN=postgres://...  npm run e2e
 */

const port = Number(process.env.PORTICO_E2E_PORT ?? 8411);
const baseURL = `http://127.0.0.1:${port}`;

// Fixed so the tests can sign in. This is a throwaway instance against a
// throwaway database; the value never leaves this file and never reaches a
// deployment, which is why it is allowed to be a literal here and nowhere
// else.
const adminPassword = "e2e-admin-password";

export default defineConfig({
  testDir: "./e2e",
  // A failing browser test is nearly always a real failure or a missing
  // wait. Retries turn the second kind into a slow pass and hide it.
  retries: 0,
  // The suite drives one server with one database, so the tests are not
  // independent of each other's writes.
  workers: 1,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL,
    // Pinned so the selectors can name one language. The interface picks its
    // language from the browser, so without this the suite passes or fails
    // depending on the locale of the machine running it.
    locale: "en-US",
    // Kept only for failures: a trace per test would make CI artifacts
    // enormous, and the passing ones are not read.
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    // The binary at the repository root, built from the same tree. Not `go
    // run`: that rebuilds without the embedded frontend if the dist
    // directory is stale, and the failure looks like a routing bug.
    command: "../portico",
    url: `${baseURL}/api/v1/ready`,
    // Never reuse: a server already running on this port is someone else's,
    // possibly pointing at a real database, and these tests write.
    reuseExistingServer: false,
    stdout: "pipe",
    stderr: "pipe",
    timeout: 60_000,
    env: {
      PORTICO_ADDR: `:${port}`,
      PORTICO_PUBLIC_URL: baseURL,
      PORTICO_DB_DSN: process.env.PORTICO_E2E_DB_DSN ?? "",
      // Not a secret in any meaningful sense — a fixed value for a throwaway
      // instance — but it has to clear the 32-byte minimum the server
      // enforces at startup.
      PORTICO_JWT_SECRET:
        process.env.PORTICO_E2E_JWT_SECRET ??
        "e2e-only-jwt-secret-not-for-any-deployment",
      PORTICO_INITIAL_ADMIN_USERNAME: "admin",
      PORTICO_INITIAL_ADMIN_PASSWORD: adminPassword,
      PORTICO_LOG_LEVEL: "warn",
    },
  },
});

export { adminPassword, baseURL };
