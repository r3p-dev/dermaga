package server

import (
	"encoding/json"
	"time"
)

// LoginRequest defines credentials submitted for admin login (ADM-1).
type LoginRequest struct {
	Password string `json:"password"`
	TOTPCode string `json:"totp_code,omitempty"`
}

// LoginResponse defines the outcome of a login attempt.
type LoginResponse struct {
	Success      bool   `json:"success"`
	TOTPRequired bool   `json:"totp_required,omitempty"`
	Error        string `json:"error,omitempty"`
}

// SessionInfo provides the client's current session state.
type SessionInfo struct {
	Authenticated      bool      `json:"authenticated"`
	UserID             string    `json:"user_id,omitempty"`
	ExpiresAt          time.Time `json:"expires_at,omitempty"`
	SetupRequired      bool      `json:"setup_required"`
	SetupAuthenticated bool      `json:"setup_authenticated"`
	SetupState         string    `json:"setup_state,omitempty"`
	TOTPEnabled        bool      `json:"totp_enabled"`
}

// SetupVerifyTokenRequest exchanges a single-use setup token for a setup session cookie (SETUP-1, SEC-8).
type SetupVerifyTokenRequest struct {
	Token string `json:"token"`
}

// SetupPasswordRequest sets the initial admin password during first-run setup (SETUP-2).
type SetupPasswordRequest struct {
	Password string `json:"password"`
}

// SetupTOTPInitResponse returns the one-time TOTP enrolment secret, URI, and QR SVG (SETUP-2).
type SetupTOTPInitResponse struct {
	Secret     string `json:"secret"`
	OTPAuthURI string `json:"otpauth_uri"`
	QRSVG      string `json:"qr_svg"`
}

// SetupTOTPVerifyRequest verifies a 6-digit TOTP code during setup enrolment (SETUP-2).
type SetupTOTPVerifyRequest struct {
	Code string `json:"code"`
}

// SetupStepResponse reports the outcome of a setup mutation.
type SetupStepResponse struct {
	Success  bool   `json:"success"`
	NextStep string `json:"next_step,omitempty"`
	Error    string `json:"error,omitempty"`
}

// ServerStatus provides operational status of the server and daemon (DASH-1).
type ServerStatus struct {
	Version     string    `json:"version"`
	Commit      string    `json:"commit"`
	StartedAt   time.Time `json:"started_at"`
	UptimeSec   int64     `json:"uptime_sec"`
	DatabaseOK  bool      `json:"database_ok"`
	SchemaVer   int       `json:"schema_version"`
	TotalMemory uint64    `json:"total_memory,omitempty"`
	UsedMemory  uint64    `json:"used_memory,omitempty"`
	DaemonRSS   uint64    `json:"daemon_rss,omitempty"`
}

// AuditLogEntry is the API representation of an audit event (AUD-2).
type AuditLogEntry struct {
	ID          int64           `json:"id"`
	Origin      string          `json:"origin"`
	AppID       *string         `json:"app_id,omitempty"`
	Event       string          `json:"event"`
	Actor       string          `json:"actor"`
	Description string          `json:"description"`
	Details     json.RawMessage `json:"details"`
	CreatedAt   string          `json:"created_at"`
}

// AuditLogsResponse contains a page of audit logs.
type AuditLogsResponse struct {
	Logs   []AuditLogEntry `json:"logs"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

// RecoveryRequest is sent over the local daemon.sock for CLI commands (SRV-2).
type RecoveryRequest struct {
	Action   string `json:"action"` // "set-password", "totp-disable", "status", "setup-token"
	Password string `json:"password,omitempty"`
}

// RecoveryResponse is returned over daemon.sock.
type RecoveryResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Token   string `json:"token,omitempty"`
}

// ActiveSessionEntry describes an active admin session (ADM-5).
type ActiveSessionEntry struct {
	ID         string    `json:"id"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

// ActiveSessionsResponse lists all active sessions (ADM-5).
type ActiveSessionsResponse struct {
	Sessions []ActiveSessionEntry `json:"sessions"`
}

// AccessSettingsResponse provides SSH tunnel and laptop setup instructions (LAP-3).
type AccessSettingsResponse struct {
	Address       string `json:"address"`
	Port          int    `json:"port"`
	LaptopCommand string `json:"laptop_command"`
	DirectSSH     string `json:"direct_ssh"`
	PanelURL      string `json:"panel_url"`
	WindowsHint   string `json:"windows_hint"`
	MobileHint    string `json:"mobile_hint"`
}

// ChangePasswordRequest updates the admin password after re-authentication (ADM-4, SEC-10).
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	TOTPCode        string `json:"totp_code,omitempty"`
}

// SettingsTOTPInitRequest requires current password before generating a new TOTP secret (SEC-10).
type SettingsTOTPInitRequest struct {
	CurrentPassword string `json:"current_password"`
}

// SettingsTOTPDisableRequest requires both current password and current TOTP code (ADM-4, SEC-10).
type SettingsTOTPDisableRequest struct {
	CurrentPassword string `json:"current_password"`
	TOTPCode        string `json:"totp_code"`
}

// HookDomainCheckRequest checks DNS A/AAAA records for a candidate hook domain (SETUP-2).
type HookDomainCheckRequest struct {
	Domain string `json:"domain"`
}

// HookDomainDNSInfo mirrors access.DNSCheckResult for tygo generation (SETUP-2).
type HookDomainDNSInfo struct {
	Domain       string   `json:"domain"`
	ServerIP     string   `json:"server_ip"`
	ARecords     []string `json:"a_records"`
	AAAARecords  []string `json:"aaaa_records"`
	AMatches     bool     `json:"a_matches"`
	AAAAMismatch bool     `json:"aaaa_mismatch"`
	Warning      string   `json:"warning,omitempty"`
}

// HookDomainResponse returns the current hook domain, server IP, and DNS status (SETUP-2).
type HookDomainResponse struct {
	HookDomain string             `json:"hook_domain"`
	ServerIP   string             `json:"server_ip"`
	SetupState string             `json:"setup_state"`
	DNSCheck   *HookDomainDNSInfo `json:"dns_check,omitempty"`
}

// HookDomainSaveRequest persists the reserved hook domain and renders its Traefik router (SETUP-2, TLS-5).
type HookDomainSaveRequest struct {
	Domain string `json:"domain"`
}

// GitHubAppInfo summarizes the connected GitHub App metadata (GHA-6).
type GitHubAppInfo struct {
	ID         int64  `json:"id"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	HTMLURL    string `json:"html_url"`
	InstallURL string `json:"install_url"`
}

// GitHubInstallationInfo represents a GitHub App installation for UI display (GHA-6, GHA-7).
type GitHubInstallationInfo struct {
	ID                  int64   `json:"id"`
	AccountLogin        string  `json:"account_login"`
	AccountType         string  `json:"account_type"`
	RepositorySelection string  `json:"repository_selection"`
	SuspendedAt         *string `json:"suspended_at,omitempty"`
	HTMLURL             string  `json:"html_url"`
}

// GitHubRepositoryInfo represents a repository visible to the GitHub App (GHA-6).
type GitHubRepositoryInfo struct {
	ID             int64  `json:"id"`
	InstallationID int64  `json:"installation_id"`
	FullName       string `json:"full_name"`
	Owner          string `json:"owner"`
	Name           string `json:"name"`
	Private        bool   `json:"private"`
	DefaultBranch  string `json:"default_branch"`
	HTMLURL        string `json:"html_url"`
}

// GitHubDeliveryInfo summarizes the most recent webhook delivery (GHA-8).
type GitHubDeliveryInfo struct {
	DeliveryID         string `json:"delivery_id"`
	Event              string `json:"event"`
	Action             string `json:"action"`
	RepositoryFullName string `json:"repository_full_name"`
	Status             string `json:"status"`
	ReceivedAt         string `json:"received_at"`
}

// GitHubStatusResponse returns the current GitHub App connection state, installations, and repos (GHA-6, GHA-8).
type GitHubStatusResponse struct {
	Connected     bool                     `json:"connected"`
	HookDomain    string                   `json:"hook_domain"`
	App           *GitHubAppInfo           `json:"app,omitempty"`
	Installations []GitHubInstallationInfo `json:"installations"`
	Repositories  []GitHubRepositoryInfo   `json:"repositories"`
	LastDelivery  *GitHubDeliveryInfo      `json:"last_delivery,omitempty"`
}

// GitHubManifestStartResponse returns the GitHub form action URL and manifest JSON (GHA-1).
type GitHubManifestStartResponse struct {
	ActionURL    string `json:"action_url"`
	ManifestJSON string `json:"manifest_json"`
}

// GitHubCallbackRequest exchanges the temporary manifest code with CSRF state verification (GHA-1).
type GitHubCallbackRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

// GitHubCallbackResponse returns the connected App metadata and installation URL (GHA-1, GHA-3).
type GitHubCallbackResponse struct {
	Success    bool           `json:"success"`
	App        *GitHubAppInfo `json:"app,omitempty"`
	InstallURL string         `json:"install_url,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// GitHubDisconnectRequest requires typed confirmation to disconnect GitHub (GHA-6).
type GitHubDisconnectRequest struct {
	Confirm string `json:"confirm"`
}

// GitHubDisconnectResponse confirms disconnection and provides GitHub App deletion guidance (GHA-6).
type GitHubDisconnectResponse struct {
	Success      bool   `json:"success"`
	DeleteAppURL string `json:"delete_app_url,omitempty"`
	Error        string `json:"error,omitempty"`
}
