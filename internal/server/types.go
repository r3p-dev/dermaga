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
