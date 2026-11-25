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
  ActiveSessionsResponse,
  AccessSettingsResponse,
  ChangePasswordRequest,
  SettingsTOTPInitRequest,
  SettingsTOTPDisableRequest,
  HookDomainCheckRequest,
  HookDomainDNSInfo,
  HookDomainResponse,
  HookDomainSaveRequest,
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

export async function getSessions(): Promise<ActiveSessionsResponse> {
  const res = await fetch("/api/auth/sessions");
  if (!res.ok) {
    throw new Error(`Failed to fetch sessions: ${res.statusText}`);
  }
  return res.json();
}

export async function revokeSession(id: string): Promise<SetupStepResponse> {
  const res = await fetch(`/api/auth/sessions/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  return res.json();
}

export async function revokeOtherSessions(): Promise<SetupStepResponse> {
  const res = await fetch("/api/auth/sessions/revoke-others", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({}),
  });
  return res.json();
}

export async function getAccessSettings(): Promise<AccessSettingsResponse> {
  const res = await fetch("/api/settings/access");
  if (!res.ok) {
    throw new Error(`Failed to fetch access settings: ${res.statusText}`);
  }
  return res.json();
}

export async function changePassword(
  req: ChangePasswordRequest,
): Promise<SetupStepResponse> {
  const res = await fetch("/api/settings/password", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  return res.json();
}

export async function initSettingsTOTP(
  req: SettingsTOTPInitRequest,
): Promise<SetupTOTPInitResponse> {
  const res = await fetch("/api/settings/totp/init", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(
      data.error || `Failed to start TOTP setup: ${res.statusText}`,
    );
  }
  return res.json();
}

export async function enableSettingsTOTP(
  req: SetupTOTPVerifyRequest,
): Promise<SetupStepResponse> {
  const res = await fetch("/api/settings/totp/enable", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  return res.json();
}

export async function disableSettingsTOTP(
  req: SettingsTOTPDisableRequest,
): Promise<SetupStepResponse> {
  const res = await fetch("/api/settings/totp/disable", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  return res.json();
}

export async function getHookDomain(): Promise<HookDomainResponse> {
  const res = await fetch("/api/settings/hook-domain");
  if (!res.ok) {
    throw new Error(`Failed to fetch hook domain: ${res.statusText}`);
  }
  return res.json();
}

export async function checkHookDomainDNS(
  req: HookDomainCheckRequest,
): Promise<HookDomainDNSInfo & { error?: string }> {
  const res = await fetch("/api/settings/hook-domain/check", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  return res.json();
}

export async function saveHookDomain(
  req: HookDomainSaveRequest,
): Promise<HookDomainResponse & { error?: string }> {
  const res = await fetch("/api/settings/hook-domain", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  return res.json();
}
