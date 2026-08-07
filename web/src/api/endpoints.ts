/** Typed wrappers around the API. One function per endpoint. */

import { download, request, upload } from "./client";
import type {
  AuditLog,
  ImportResult,
  LogKind,
  Organization,
  PageResult,
  RecoveryChannel,
  RegistrationStatus,
  Role,
  Session,
  Settings,
  Status,
  User,
} from "./types";

/** Builds a query string, omitting empty values. */
function query(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") {
      search.set(key, String(value));
    }
  }
  const encoded = search.toString();
  return encoded ? `?${encoded}` : "";
}

export const authApi = {
  /**
   * Signs in. The identifier is a username, email address, or phone number
   * — the server works out which. An empty tenant means the default one,
   * which is what a single-tenant deployment always sends.
   */
  login: (tenant: string, identifier: string, password: string) =>
    request<Session>("/auth/login", {
      method: "POST",
      body: { tenant, identifier, password },
      anonymous: true,
    }),

  logout: () => request<null>("/auth/logout", { method: "POST" }),

  register: (input: {
    username: string;
    displayName: string;
    password: string;
    phone?: string;
    email?: string;
  }) =>
    request<User>("/auth/register", {
      method: "POST",
      body: input,
      anonymous: true,
    }),

  /**
   * Whether sign-up is open, and what the tenant calls itself. The signal
   * lets the sign-in screen drop a lookup for a tenant the user has since
   * changed, so a slow earlier response cannot overwrite a newer one.
   */
  registrationStatus: (signal?: AbortSignal) =>
    request<RegistrationStatus>("/auth/registration-status", {
      anonymous: true,
      signal,
    }),

  /**
   * Which recovery channels this deployment can actually use, and how long
   * a link lasts. The lifetime comes from the server so the copy on screen
   * cannot drift from the constant that enforces it.
   */
  recoveryChannels: (signal?: AbortSignal) =>
    request<{ channels: RecoveryChannel[]; tokenTtlMinutes: number }>(
      "/auth/recovery-channels",
      { anonymous: true, signal },
    ),

  /**
   * Asks for a reset link. The response is the same whether or not an
   * account matched — the server will not say, and neither should the UI.
   */
  requestPasswordRecovery: (channel: RecoveryChannel, destination: string) =>
    request<{ message: string }>("/auth/password-recovery", {
      method: "POST",
      body: { channel, destination },
      anonymous: true,
    }),

  /** Redeems a reset link. The tenant travels in the link, not here. */
  confirmPasswordRecovery: (token: string, newPassword: string) =>
    request<{ reauthenticationRequired: boolean }>(
      "/auth/password-recovery/confirm",
      { method: "POST", body: { token, newPassword }, anonymous: true },
    ),
};

export const userApi = {
  me: () => request<User>("/users/me"),

  /**
   * Updates the caller's own details. Role, status, organization, and
   * username are absent on purpose — the server rejects them outright.
   */
  updateOwnProfile: (input: {
    displayName: string;
    phone: string;
    email: string;
  }) => request<User>("/users/me", { method: "PUT", body: input }),

  changeOwnPassword: (currentPassword: string, newPassword: string) =>
    request<{ reauthenticationRequired: boolean }>("/users/me/password", {
      method: "POST",
      body: { currentPassword, newPassword },
    }),

  list: (params: {
    page?: number;
    pageSize?: number;
    keyword?: string;
    status?: Status | "";
    role?: Role | "";
    organizationId?: string;
  }) => request<PageResult<User>>(`/users${query(params)}`),

  get: (id: string) => request<User>(`/users/${id}`),

  create: (input: {
    username: string;
    displayName: string;
    password: string;
    phone?: string;
    email?: string;
    role: Role;
    organizationId?: string;
  }) => request<User>("/users", { method: "POST", body: input }),

  update: (
    id: string,
    input: {
      displayName: string;
      phone?: string;
      email?: string;
      role: Role;
      organizationId?: string;
    },
  ) => request<User>(`/users/${id}`, { method: "PUT", body: input }),

  enable: (id: string) =>
    request<User>(`/users/${id}/enable`, { method: "POST" }),
  disable: (id: string) =>
    request<User>(`/users/${id}/disable`, { method: "POST" }),

  resetPassword: (id: string, newPassword: string) =>
    request<null>(`/users/${id}/password`, {
      method: "POST",
      body: { newPassword },
    }),

  importUsers: (file: File) => upload<ImportResult>("/users/import", file),

  downloadTemplate: () =>
    download("/users/import/template", "portico-user-import-template.xlsx"),
};

export const organizationApi = {
  list: (activeOnly = false) =>
    request<Organization[]>(
      `/organizations${activeOnly ? "?activeOnly=true" : ""}`,
    ),

  create: (input: {
    name: string;
    code: string;
    remark?: string;
    sortOrder?: number;
  }) =>
    request<Organization>("/organizations", { method: "POST", body: input }),

  update: (
    id: string,
    input: { name: string; remark?: string; sortOrder?: number },
  ) =>
    request<Organization>(`/organizations/${id}`, {
      method: "PUT",
      body: input,
    }),

  enable: (id: string) =>
    request<Organization>(`/organizations/${id}/enable`, { method: "POST" }),
  disable: (id: string) =>
    request<Organization>(`/organizations/${id}/disable`, { method: "POST" }),
};

export const auditApi = {
  list: (params: {
    page?: number;
    pageSize?: number;
    kind?: LogKind | "";
    action?: string;
    keyword?: string;
    from?: string;
    to?: string;
  }) => request<PageResult<AuditLog>>(`/audit-logs${query(params)}`),
};

export const settingsApi = {
  get: () => request<Settings>("/settings"),
  update: (settings: Settings) =>
    request<Settings>("/settings", { method: "PUT", body: settings }),
};
