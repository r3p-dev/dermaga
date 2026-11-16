import type {
  LoginRequest,
  LoginResponse,
  SessionInfo,
  ServerStatus,
  AuditLogsResponse,
} from "./types/api.js";

export async function getSession(): Promise<SessionInfo> {
  const res = await fetch("/api/auth/session");
  if (!res.ok) {
    throw new Error(`Failed to fetch session: ${res.statusText}`);
  }
  return res.json();
}

export async function login(req: LoginRequest): Promise<LoginResponse> {
  const res = await fetch("/api/auth/login", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(req),
  });
  return res.json();
}

export async function logout(): Promise<{ success: boolean }> {
  const res = await fetch("/api/auth/logout", {
    method: "POST",
  });
  return res.json();
}

export async function getServerStatus(): Promise<ServerStatus> {
  const res = await fetch("/api/server/status");
  if (!res.ok) {
    throw new Error(`Failed to fetch status: ${res.statusText}`);
  }
  return res.json();
}

export async function getAuditLogs(
  origin?: string,
  limit = 50,
): Promise<AuditLogsResponse> {
  const params = new URLSearchParams();
  if (origin) params.set("origin", origin);
  params.set("limit", limit.toString());

  const res = await fetch(`/api/audit-logs?${params.toString()}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch audit logs: ${res.statusText}`);
  }
  return res.json();
}
