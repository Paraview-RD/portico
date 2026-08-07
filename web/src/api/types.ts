/** Wire types, mirroring the Go models in internal/model. */

export type Role = "SUPER_ADMIN" | "USER";
export type Status = "ACTIVE" | "DISABLED";
export type UserSource = "ADMIN" | "IMPORT" | "REGISTRATION";

export interface User {
  id: string;
  /** The tenant the account belongs to. Reported, never sent. */
  tenantId: string;
  username: string;
  displayName: string;
  phone: string;
  email: string;
  role: Role;
  status: Status;
  source: UserSource;
  organizationId: string;
  organizationName: string;
  createdAt: string;
  updatedAt: string;
}

export interface Organization {
  id: string;
  name: string;
  code: string;
  remark: string;
  status: Status;
  sortOrder: number;
  userCount: number;
  createdAt: string;
  updatedAt: string;
}

export type LogKind =
  "LOGIN" | "OPERATION" | "AUTH" | "REGISTRATION" | "ORGANIZATION";

export interface AuditLog {
  id: string;
  kind: LogKind;
  action: string;
  result: "SUCCESS" | "FAILURE";
  actorId: string;
  actorName: string;
  targetType: string;
  targetId: string;
  targetName: string;
  detail: string;
  ip: string;
  createdAt: string;
}

export interface Settings {
  tokenTtlMinutes: number;
  registrationEnabled: boolean;
  systemName: string;
}

export interface Session {
  token: string;
  expiresAt: string;
  user: User;
}

export interface PageResult<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
}

export interface ImportRowError {
  row: number;
  username: string;
  code: string;
  message: string;
}

export interface ImportResult {
  total: number;
  imported: number;
  failed: number;
  errors: ImportRowError[];
}

/** How a password-reset link reaches its owner. */
export type RecoveryChannel = "EMAIL" | "SMS";

export interface RegistrationStatus {
  registrationEnabled: boolean;
  systemName: string;
  /** The tenant the answer is about, resolved from the request. */
  tenant: string;
  tenantName: string;
}
