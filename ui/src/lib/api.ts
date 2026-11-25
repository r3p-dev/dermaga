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
  GitHubStatusResponse,
  GitHubManifestStartResponse,
  GitHubCallbackRequest,
  GitHubCallbackResponse,
  GitHubDisconnectRequest,
  GitHubDisconnectResponse,
  RepoBranchesResponse,
  RepoInspectResponse,
  DomainCheckRequest,
  DomainCheckResponse,
  AppsListResponse,
  AppDetailResponse,
  CreateAppRequest,
  CreateAppResponse,
  UpdateAppSettingsRequest,
  UpdateAppEnvRequest,
  EnvRevealRequest,
  EnvRevealResponse,
  EnvVarMetadata,
  DeleteAppRequest,
  DeploymentInfo,
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

export async function getGitHubStatus(): Promise<GitHubStatusResponse> {
  const res = await fetch("/api/settings/github");
  if (!res.ok) {
    throw new Error(`Failed to fetch GitHub status: ${res.statusText}`);
  }
  return res.json();
}

export async function startGitHubManifest(): Promise<GitHubManifestStartResponse> {
  const res = await fetch("/api/settings/github/manifest", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(
      data.error || `Failed to start GitHub connection: ${res.statusText}`,
    );
  }
  return data;
}

export async function completeGitHubCallback(
  req: GitHubCallbackRequest,
): Promise<GitHubCallbackResponse> {
  const res = await fetch("/api/settings/github/callback", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  return res.json();
}

export async function syncGitHub(): Promise<
  GitHubStatusResponse & { error?: string }
> {
  const res = await fetch("/api/settings/github/sync", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
  });
  return res.json();
}

export async function disconnectGitHub(
  req: GitHubDisconnectRequest,
): Promise<GitHubDisconnectResponse> {
  const res = await fetch("/api/settings/github", {
    method: "DELETE",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  return res.json();
}

export async function listRepoBranches(
  owner: string,
  repo: string,
): Promise<RepoBranchesResponse> {
  const res = await fetch(
    `/api/github/repos/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/branches`,
  );
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || `Failed to load branches: ${res.statusText}`);
  }
  return data;
}

export async function inspectRepo(
  owner: string,
  repo: string,
  branch: string,
): Promise<RepoInspectResponse> {
  const params = new URLSearchParams();
  if (branch) params.set("branch", branch);
  const res = await fetch(
    `/api/github/repos/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/inspect?${params.toString()}`,
  );
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(
      data.error || `Failed to inspect repository: ${res.statusText}`,
    );
  }
  return data;
}

export async function checkDomainDNS(
  req: DomainCheckRequest,
): Promise<DomainCheckResponse & { error?: string }> {
  const res = await fetch("/api/domains/check", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  return res.json();
}

export async function listApps(): Promise<AppsListResponse> {
  const res = await fetch("/api/apps");
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || `Failed to load apps: ${res.statusText}`);
  }
  return data;
}

export async function createApp(req: CreateAppRequest): Promise<{
  ok: boolean;
  data?: CreateAppResponse;
  error?: string;
  fields?: Record<string, string>;
}> {
  const res = await fetch("/api/apps", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    return {
      ok: false,
      error: data.error || `Failed to create app: ${res.statusText}`,
      fields: data.fields || {},
    };
  }
  return { ok: true, data };
}

export async function getApp(id: string): Promise<AppDetailResponse> {
  const res = await fetch(`/api/apps/${encodeURIComponent(id)}`);
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || `Failed to load app: ${res.statusText}`);
  }
  return data;
}

export async function updateAppSettings(
  id: string,
  req: UpdateAppSettingsRequest,
): Promise<AppDetailResponse> {
  const res = await fetch(`/api/apps/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(
      data.error || `Failed to update settings: ${res.statusText}`,
    );
  }
  return data;
}

export async function updateAppEnv(
  id: string,
  req: UpdateAppEnvRequest,
): Promise<{ env: EnvVarMetadata[] }> {
  const res = await fetch(`/api/apps/${encodeURIComponent(id)}/env`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(
      data.error || `Failed to update environment: ${res.statusText}`,
    );
  }
  return data;
}

export async function revealAppEnv(
  id: string,
  req: EnvRevealRequest,
): Promise<EnvRevealResponse> {
  const res = await fetch(`/api/apps/${encodeURIComponent(id)}/env/reveal`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(
      data.error || `Failed to reveal variable: ${res.statusText}`,
    );
  }
  return data;
}

export async function triggerDeploy(id: string): Promise<DeploymentInfo> {
  const res = await fetch(`/api/apps/${encodeURIComponent(id)}/deploy`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(
      data.error || `Failed to trigger deploy: ${res.statusText}`,
    );
  }
  return data;
}

export async function cancelDeployment(depId: string): Promise<void> {
  const res = await fetch(
    `/api/deployments/${encodeURIComponent(depId)}/cancel`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
    },
  );
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(
      data.error || `Failed to cancel deployment: ${res.statusText}`,
    );
  }
}

export async function deleteApp(
  id: string,
  req: DeleteAppRequest,
): Promise<void> {
  const res = await fetch(`/api/apps/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || `Failed to delete app: ${res.statusText}`);
  }
}

export function streamBuildLogs(
  depId: string,
  handlers: {
    onLog: (chunk: string) => void;
    onStatus?: (statusLabel: string) => void;
    onDone?: (errorMessage: string) => void;
    onError?: () => void;
  },
): () => void {
  const es = new EventSource(
    `/api/deployments/${encodeURIComponent(depId)}/logs/stream`,
  );
  es.addEventListener("log", (ev) => {
    try {
      handlers.onLog(JSON.parse((ev as MessageEvent).data));
    } catch {
      handlers.onLog((ev as MessageEvent).data);
    }
  });
  es.addEventListener("status", (ev) => {
    try {
      handlers.onStatus?.(JSON.parse((ev as MessageEvent).data));
    } catch {
      handlers.onStatus?.((ev as MessageEvent).data);
    }
  });
  es.addEventListener("done", (ev) => {
    let errMsg = "";
    try {
      errMsg = JSON.parse((ev as MessageEvent).data);
    } catch {
      errMsg = "";
    }
    es.close();
    handlers.onDone?.(errMsg);
  });
  es.onerror = () => {
    es.close();
    handlers.onError?.();
  };
  return () => es.close();
}

export function streamRuntimeLogs(
  appId: string,
  lines: number,
  follow: boolean,
  handlers: {
    onLog: (line: string) => void;
    onError?: (msg: string) => void;
    onDone?: () => void;
  },
): () => void {
  const params = new URLSearchParams({
    lines: lines.toString(),
    follow: follow ? "true" : "false",
  });
  const es = new EventSource(
    `/api/apps/${encodeURIComponent(appId)}/logs/stream?${params.toString()}`,
  );
  es.addEventListener("log", (ev) => {
    try {
      handlers.onLog(JSON.parse((ev as MessageEvent).data));
    } catch {
      handlers.onLog((ev as MessageEvent).data);
    }
  });
  es.addEventListener("error", (ev) => {
    const me = ev as MessageEvent;
    if (me.data) {
      try {
        handlers.onError?.(JSON.parse(me.data));
      } catch {
        handlers.onError?.(me.data);
      }
    }
    es.close();
  });
  es.addEventListener("done", () => {
    es.close();
    handlers.onDone?.();
  });
  return () => es.close();
}
