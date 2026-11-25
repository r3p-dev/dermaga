import type {
  LoginRequest,
  LoginResponse,
  SessionInfo,
  ServerStatus,
  AuditLogsResponse,
  SetupVerifyTokenRequest,
  SetupPasswordRequest,
  SetupTOTPInitResponse,
  SetupTOTPVerifyRequest,
  SetupStepResponse,
} from "./types/api.js";

export async function verifySetupToken(
  req: SetupVerifyTokenRequest,
): Promise<SetupStepResponse> {
  const res = await fetch("/api/setup/verify-token", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(req),
  });
  return res.json();
}

export async function setSetupPassword(
  req: SetupPasswordRequest,
): Promise<SetupStepResponse> {
  const res = await fetch("/api/setup/password", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(req),
  });
  return res.json();
}

export async function initSetupTOTP(): Promise<SetupTOTPInitResponse> {
  const res = await fetch("/api/setup/totp/init", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({}),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(
      data.error || `Failed to start TOTP setup: ${res.statusText}`,
    );
  }
  return res.json();
}

export async function verifySetupTOTP(
  req: SetupTOTPVerifyRequest,
): Promise<SetupStepResponse> {
  const res = await fetch("/api/setup/totp/verify", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(req),
  });
  return res.json();
}

export async function skipSetupTOTP(): Promise<SetupStepResponse> {
  const res = await fetch("/api/setup/totp/skip", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({}),
  });
  return res.json();
}

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
  app?: string,
  since?: string,
  until?: string,
  limit = 50,
): Promise<AuditLogsResponse> {
  const params = new URLSearchParams();
  if (origin) params.set("origin", origin);
  if (app) params.set("app", app);
  if (since) params.set("since", since);
  if (until) params.set("until", until);
  params.set("limit", limit.toString());

  const res = await fetch(`/api/audit-logs?${params.toString()}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch audit logs: ${res.statusText}`);
  }
  return res.json();
}
