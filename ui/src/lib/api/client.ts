import type {
	Status,
	AppView,
	DatabaseView,
	CreateDatabaseRequest,
	LinkDatabaseRequest,
	RunBackupRequest,
	Settings as AppSettings,
	GitHubSettings,
	NotificationSettingsView,
	UpdateNotificationSettingsRequest,
	TestNotificationRequest,
	TestNotificationResponse,
	TelemetryOverview
} from '../types/api';
import type * as spec from '../types/spec';
import type * as apps from '../types/apps';
import type * as databases from '../types/databases';
import type * as backup from '../types/backup';
import type * as audit from '../types/audit';
import type * as doctor from '../types/doctor';
import type * as telemetry from '../types/telemetry';

export class APIError extends Error {
	constructor(
		public status: number,
		message: string,
		public retryAfter?: number
	) {
		super(message);
		this.name = 'APIError';
	}
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
	const headers = new Headers(options.headers || {});
	if (options.body && !(options.body instanceof FormData)) {
		headers.set('Content-Type', 'application/json');
	}

	const res = await fetch(path, {
		...options,
		headers
	});

	if (res.status === 401 && !path.startsWith('/api/login') && typeof window !== 'undefined') {
		if (window.location.pathname !== '/login') {
			window.location.href = '/login';
		}
		throw new APIError(401, 'Unauthorized');
	}

	if (!res.ok) {
		let errMsg = res.statusText || 'Request failed';
		try {
			const data = await res.json();
			if (data && typeof data.error === 'string') {
				errMsg = data.error;
			}
		} catch {
			// ignore json parse error
		}

		let retryAfter: number | undefined;
		const ra = res.headers.get('Retry-After');
		if (ra) {
			const parsed = parseInt(ra, 10);
			if (!isNaN(parsed)) retryAfter = parsed;
		}

		throw new APIError(res.status, errMsg, retryAfter);
	}

	if (res.status === 204) {
		return null as T;
	}

	return (await res.json()) as T;
}

export const api = {
	// Auth & Status
	async login(password: string, totp?: string): Promise<void> {
		return request<void>('/api/login', {
			method: 'POST',
			body: JSON.stringify({ password, totp: totp || '' })
		});
	},

	async logout(): Promise<void> {
		return request<void>('/api/logout', { method: 'POST' });
	},

	async getStatus(): Promise<Status> {
		return request<Status>('/api/status');
	},

	async getDoctor(): Promise<doctor.Check[]> {
		return request<doctor.Check[]>('/api/doctor');
	},

	async getAudit(limit = 100): Promise<audit.Entry[]> {
		return request<audit.Entry[]>(`/api/audit?limit=${limit}`);
	},

	// Apps
	async listApps(): Promise<AppView[]> {
		return request<AppView[]>('/api/apps');
	},

	async getApp(name: string): Promise<AppView> {
		return request<AppView>(`/api/apps/${encodeURIComponent(name)}`);
	},

	async createApp(req: { name: string; domains?: spec.Domain[]; spec: spec.App }): Promise<AppView> {
		return request<AppView>('/api/apps', {
			method: 'POST',
			body: JSON.stringify(req)
		});
	},

	async updateApp(
		name: string,
		req: { name: string; domains?: spec.Domain[]; spec: spec.App }
	): Promise<AppView> {
		return request<AppView>(`/api/apps/${encodeURIComponent(name)}`, {
			method: 'PUT',
			body: JSON.stringify(req)
		});
	},

	async deleteApp(name: string, purge = false): Promise<void> {
		const q = purge ? '?purge=1' : '';
		return request<void>(`/api/apps/${encodeURIComponent(name)}${q}`, {
			method: 'DELETE'
		});
	},

	async deployApp(name: string, ref = ''): Promise<apps.Release> {
		return request<apps.Release>(`/api/apps/${encodeURIComponent(name)}/deploy`, {
			method: 'POST',
			body: JSON.stringify({ ref })
		});
	},

	async rollbackApp(name: string, to = ''): Promise<apps.Release> {
		return request<apps.Release>(`/api/apps/${encodeURIComponent(name)}/rollback`, {
			method: 'POST',
			body: JSON.stringify({ to })
		});
	},

	async getEnv(name: string, reveal = false): Promise<Record<string, string> | string[]> {
		const q = reveal ? '?reveal=1' : '';
		return request<Record<string, string> | string[]>(`/api/apps/${encodeURIComponent(name)}/env${q}`);
	},

	async updateEnv(
		name: string,
		set: Record<string, string> = {},
		unset: string[] = []
	): Promise<void> {
		return request<void>(`/api/apps/${encodeURIComponent(name)}/env`, {
			method: 'PUT',
			body: JSON.stringify({ set, unset })
		});
	},

	async revealWebhook(name: string): Promise<GitHubSettings> {
		return request<GitHubSettings>(`/api/apps/${encodeURIComponent(name)}/webhook`);
	},

	async syncKnownHosts(): Promise<{ file: string; fingerprints: string[] }> {
		return request<{ file: string; fingerprints: string[] }>('/api/github/known-hosts', {
			method: 'POST'
		});
	},

	// Databases
	async listDatabases(): Promise<DatabaseView[]> {
		return request<DatabaseView[]>('/api/databases');
	},

	async getDatabase(name: string, reveal = false): Promise<DatabaseView> {
		const q = reveal ? '?reveal=true' : '';
		return request<DatabaseView>(`/api/databases/${encodeURIComponent(name)}${q}`);
	},

	async createDatabase(req: CreateDatabaseRequest): Promise<DatabaseView> {
		return request<DatabaseView>('/api/databases', {
			method: 'POST',
			body: JSON.stringify(req)
		});
	},

	async deleteDatabase(name: string, force = false, purge = false): Promise<void> {
		const params = new URLSearchParams();
		if (force) params.set('force', 'true');
		if (purge) params.set('purge', 'true');
		const qs = params.toString() ? `?${params.toString()}` : '';
		return request<void>(`/api/databases/${encodeURIComponent(name)}${qs}`, {
			method: 'DELETE'
		});
	},

	async restartDatabase(name: string): Promise<{ status: string }> {
		return request<{ status: string }>(`/api/databases/${encodeURIComponent(name)}/restart`, {
			method: 'POST'
		});
	},

	async linkDatabase(name: string, app: string, envNames?: Record<string, string>): Promise<any> {
		return request<any>(`/api/databases/${encodeURIComponent(name)}/link`, {
			method: 'POST',
			body: JSON.stringify({ app, env_names: envNames })
		});
	},

	async unlinkDatabase(name: string, app: string): Promise<any> {
		return request<any>(`/api/databases/${encodeURIComponent(name)}/unlink`, {
			method: 'POST',
			body: JSON.stringify({ app })
		});
	},

	async backupDatabase(name: string): Promise<backup.Snapshot> {
		return request<backup.Snapshot>(`/api/databases/${encodeURIComponent(name)}/backup`, {
			method: 'POST'
		});
	},

	async testRestoreDatabase(name: string): Promise<any> {
		return request<any>(`/api/databases/${encodeURIComponent(name)}/restore-test`, {
			method: 'POST'
		});
	},

	async restoreDatabase(name: string, snapshotId?: string): Promise<any> {
		return request<any>(`/api/databases/${encodeURIComponent(name)}/restore`, {
			method: 'POST',
			body: JSON.stringify({ snapshotId, confirm: true })
		});
	},

	// Backups
	async getBackupSettings(): Promise<backup.Settings> {
		return request<backup.Settings>('/api/backup/settings');
	},

	async updateBackupSettings(settings: backup.Settings): Promise<void> {
		return request<void>('/api/backup/settings', {
			method: 'PUT',
			body: JSON.stringify(settings)
		});
	},

	async listSnapshots(tag = ''): Promise<backup.Snapshot[]> {
		const q = tag ? `?tag=${encodeURIComponent(tag)}` : '';
		return request<backup.Snapshot[]>(`/api/backup/snapshots${q}`);
	},

	async runBackup(target: string): Promise<backup.Snapshot> {
		return request<backup.Snapshot>('/api/backup/run', {
			method: 'POST',
			body: JSON.stringify({ target })
		});
	},

	async checkBackup(readDataSubset = ''): Promise<any> {
		return request<any>('/api/backup/check', {
			method: 'POST',
			body: JSON.stringify({ readDataSubset })
		});
	},

	async systemBackup(): Promise<backup.Snapshot> {
		return request<backup.Snapshot>('/api/backup/system', { method: 'POST' });
	},

	async restoreSystem(snapshotId = ''): Promise<any> {
		return request<any>('/api/backup/restore-system', {
			method: 'POST',
			body: JSON.stringify({ snapshotId, confirm: true })
		});
	},

	async exportKeys(): Promise<{ masterKeyB64: string; instructions: string }> {
		return request<{ masterKeyB64: string; instructions: string }>('/api/keys/export');
	},

	// Settings
	async getSettings(): Promise<AppSettings> {
		return request<AppSettings>('/api/settings');
	},

	async updateSettings(settings: AppSettings): Promise<AppSettings> {
		return request<AppSettings>('/api/settings', {
			method: 'PUT',
			body: JSON.stringify(settings)
		});
	},

	// Telemetry & Observability (M9)
	async getTelemetryOverview(): Promise<TelemetryOverview> {
		return request<TelemetryOverview>('/api/telemetry/overview');
	},

	async getTelemetryMetrics(
		container = '',
		since = ''
	): Promise<Record<string, telemetry.ContainerMetric[]>> {
		const params = new URLSearchParams();
		if (container) params.set('container', container);
		if (since) params.set('since', since);
		const qs = params.toString();
		return request<Record<string, telemetry.ContainerMetric[]>>(
			`/api/telemetry/metrics${qs ? '?' + qs : ''}`
		);
	},

	async getTelemetryTraffic(app = '', since = ''): Promise<telemetry.RequestBucket[]> {
		const params = new URLSearchParams();
		if (app) params.set('app', app);
		if (since) params.set('since', since);
		const qs = params.toString();
		return request<telemetry.RequestBucket[]>(`/api/telemetry/traffic${qs ? '?' + qs : ''}`);
	},

	// Notifications (M9)
	async getNotificationSettings(): Promise<NotificationSettingsView> {
		return request<NotificationSettingsView>('/api/settings/notifications');
	},

	async updateNotificationSettings(req: UpdateNotificationSettingsRequest): Promise<void> {
		return request<void>('/api/settings/notifications', {
			method: 'POST',
			body: JSON.stringify(req)
		});
	},

	async testNotification(req?: TestNotificationRequest): Promise<TestNotificationResponse> {
		return request<TestNotificationResponse>('/api/notifications/test', {
			method: 'POST',
			body: JSON.stringify(req || {})
		});
	}
};

/**
 * streamSSE connects to a Server-Sent Events endpoint and streams incoming data lines.
 */
export function streamSSE(
	url: string,
	onMessage: (line: string) => void,
	onEnd?: (status?: string) => void,
	onError?: (err: Error) => void
): () => void {
	if (typeof window === 'undefined') return () => {};

	const es = new EventSource(url);
	es.onmessage = (e) => {
		onMessage(e.data);
	};

	es.addEventListener('end', (e: any) => {
		if (onEnd) onEnd(e.data);
		es.close();
	});

	es.onerror = () => {
		if (onError) onError(new Error('SSE connection failed or terminated'));
		es.close();
	};

	return () => es.close();
}
