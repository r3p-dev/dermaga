package traefik

// StaticSpec specifies parameters for generating Traefik's static configuration.
type StaticSpec struct {
	ACMEEmail      string
	ACMEStaging    bool
	CAServer       string
	AccessLog      bool
	TrustedProxies []string
	DynamicDir     string
	ACMEStorage    string
}

// GlobalSpec specifies parameters for generating 00-global.yml.
type GlobalSpec struct {
	TrustedProxies          []string
	DefaultRateLimitAverage int
	DefaultRateLimitBurst   int
	DefaultCompressMinBytes int
	DisableDefaultSecurity  bool
}

// AppSpec specifies all parameters required to generate an app's dynamic configuration file (app-<name>.yml).
type AppSpec struct {
	AppName       string
	ReleaseNumber int
	Port          int
	Domains       []DomainSpec
	Headers       HeaderSpec
	Middleware    MiddlewareSpec
	Switch        *SwitchSpec
}

// DomainSpec defines a single domain routing configuration.
type DomainSpec struct {
	Domain      string
	IsPrimary   bool
	RedirectTo  string
	PathPrefix  string
	StripPrefix bool
	// Pending: DNS not verified yet or the certificate order failed; no router until it clears
	// (DEC-38). The primary domain still names the verify router, which listens only on :8099.
	Pending bool
}

// HeaderSpec defines security and custom response headers for an app.
type HeaderSpec struct {
	DisableSecurityHeaders bool
	CustomResponseHeaders  map[string]string
	PermissionsPolicy      string
}

// MiddlewareSpec defines middleware options for an app.
type MiddlewareSpec struct {
	Compress            *bool
	RateLimit           *RateLimitSpec
	BasicAuth           []BasicAuthUser
	IPAllowList         []string
	MaxRequestBodyBytes int64
}

// RateLimitSpec specifies custom rate limit rules.
type RateLimitSpec struct {
	Average int
	Burst   int
}

// BasicAuthUser defines a single basic auth credential.
type BasicAuthUser struct {
	Username string
	Hash     string // bcrypt hash
}

// SwitchSpec defines candidate release routing during a blue-green deploy verification switch.
type SwitchSpec struct {
	CandidateReleaseNumber int
	CandidatePort          int
	VerifyToken            string
}
