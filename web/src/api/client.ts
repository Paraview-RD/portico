/**
 * The single place that speaks to the Keylite API.
 *
 * Every response is the `{code, message, data}` envelope described in
 * docs/api-conventions.md. `request` unwraps it and throws on failure, so a
 * caller can never mistake an error for success by forgetting to check
 * `code` — the check lives here, not in each caller.
 */

export interface Envelope<T> {
  code: string;
  message: string;
  data: T;
}

/** A failed request, carrying the API's machine-readable code. */
export class ApiError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, message: string, status: number) {
    super(message || code);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

const TOKEN_KEY = "keylite.token";

/**
 * Error codes that mean the stored token is no longer usable. Any of them
 * clears the session rather than surfacing a confusing error mid-screen.
 */
const SESSION_ENDED_CODES = new Set([
  "MISSING_TOKEN",
  "INVALID_TOKEN",
  "TOKEN_EXPIRED",
  "TOKEN_REVOKED",
  "ACCOUNT_DISABLED",
  "MALFORMED_AUTHORIZATION",
]);

export const tokenStore = {
  get: (): string | null => localStorage.getItem(TOKEN_KEY),
  set: (token: string) => localStorage.setItem(TOKEN_KEY, token),
  clear: () => localStorage.removeItem(TOKEN_KEY),
};

/** Called when the session ends, so the app can route back to sign-in. */
type SessionEndedHandler = () => void;
let onSessionEnded: SessionEndedHandler = () => {};

export function setSessionEndedHandler(handler: SessionEndedHandler) {
  onSessionEnded = handler;
}

interface RequestOptions {
  // No DELETE: nothing is deleted, only disabled. See api-conventions.md.
  method?: "GET" | "POST" | "PUT";
  body?: unknown;
  /** Skips the bearer header, for the endpoints that run signed out. */
  anonymous?: boolean;
  signal?: AbortSignal;
}

export async function request<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  const { method = "GET", body, anonymous = false, signal } = options;

  const headers: Record<string, string> = {};
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  if (!anonymous) {
    const token = tokenStore.get();
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    }
  }

  const response = await fetch(`/api/v1${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  });

  let envelope: Envelope<T>;
  try {
    envelope = await response.json();
  } catch {
    // A non-JSON body means something upstream of the app answered — a
    // proxy, a crash page. Do not pretend it was a normal API failure.
    throw new ApiError(
      "UNEXPECTED_RESPONSE",
      `The server returned an unreadable response (HTTP ${response.status}).`,
      response.status,
    );
  }

  if (!response.ok || envelope.code !== "SUCCESS") {
    if (SESSION_ENDED_CODES.has(envelope.code)) {
      tokenStore.clear();
      onSessionEnded();
    }
    throw new ApiError(envelope.code, envelope.message, response.status);
  }

  return envelope.data;
}

/** Uploads a file as multipart/form-data, returning the unwrapped payload. */
export async function upload<T>(path: string, file: File): Promise<T> {
  const form = new FormData();
  form.append("file", file);

  const headers: Record<string, string> = {};
  const token = tokenStore.get();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const response = await fetch(`/api/v1${path}`, {
    method: "POST",
    headers,
    body: form,
  });

  let envelope: Envelope<T>;
  try {
    envelope = await response.json();
  } catch {
    throw new ApiError(
      "UNEXPECTED_RESPONSE",
      `The server returned an unreadable response (HTTP ${response.status}).`,
      response.status,
    );
  }

  if (!response.ok || envelope.code !== "SUCCESS") {
    if (SESSION_ENDED_CODES.has(envelope.code)) {
      tokenStore.clear();
      onSessionEnded();
    }
    throw new ApiError(envelope.code, envelope.message, response.status);
  }

  return envelope.data;
}

/** Triggers a browser download of an authenticated endpoint. */
export async function download(path: string, filename: string): Promise<void> {
  const token = tokenStore.get();
  const response = await fetch(`/api/v1${path}`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  });

  if (!response.ok) {
    throw new ApiError(
      "DOWNLOAD_FAILED",
      "The download could not be started.",
      response.status,
    );
  }

  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.click();
  URL.revokeObjectURL(url);
}
