// Package traefik renders Traefik v3 static configuration and dynamic file-provider
// configurations as JSON inside .yml files (ARCHITECTURE DEC-01).
package traefik

// CertResolverName is the default Let's Encrypt certificate resolver identifier.
const CertResolverName = "le"

// Image is the pinned Traefik image: tag plus the multi-arch index digest, so amd64 and arm64
// servers pull the same release (registry query and result line: docs/DEPENDENCIES.md §3).
const Image = "docker.io/library/traefik:v3.7.19@sha256:aa942a00bce033c909bf1f7e861d93761be4b31a035f10d2410d33e38a8f41cf"

// VerifyHeader is the HTTP header inspected by the temporary switch router.
const VerifyHeader = "X-Dermaga-Verify"

// DefaultPermissionsPolicy matches vps-setup.sh's REL-8 per-app baseline.
const DefaultPermissionsPolicy = "camera=(), microphone=(), geolocation=(), payment=()"

// Dynamic represents a Traefik dynamic file-provider configuration document.
type Dynamic struct {
	HTTP *HTTPConfig `json:"http,omitempty"`
	TLS  *TLSConfig  `json:"tls,omitempty"`
}

// HTTPConfig contains HTTP routers, services, and middlewares.
type HTTPConfig struct {
	Routers     map[string]Router     `json:"routers,omitempty"`
	Services    map[string]Service    `json:"services,omitempty"`
	Middlewares map[string]Middleware `json:"middlewares,omitempty"`
}

// Router represents a Traefik HTTP router.
type Router struct {
	EntryPoints []string   `json:"entryPoints"`
	Rule        string     `json:"rule"`
	Service     string     `json:"service"`
	Middlewares []string   `json:"middlewares,omitempty"`
	TLS         *RouterTLS `json:"tls,omitempty"`
	Priority    int        `json:"priority,omitempty"`
}

// RouterTLS defines TLS configuration on a router.
type RouterTLS struct {
	CertResolver string `json:"certResolver"`
}

// Service represents a Traefik service with load-balanced servers.
type Service struct {
	LoadBalancer LoadBalancer `json:"loadBalancer"`
}

// LoadBalancer holds a list of upstream backend servers.
type LoadBalancer struct {
	Servers []Server `json:"servers"`
}

// Server defines an upstream server URL.
type Server struct {
	URL string `json:"url"`
}

// Middleware defines Traefik middleware options. Exactly one payload is populated per middleware.
type Middleware struct {
	Headers       *Headers       `json:"headers,omitempty"`
	RateLimit     *RateLimit     `json:"rateLimit,omitempty"`
	Compress      *Compress      `json:"compress,omitempty"`
	BasicAuth     *BasicAuth     `json:"basicAuth,omitempty"`
	IPAllowList   *IPAllowList   `json:"ipAllowList,omitempty"`
	Buffering     *Buffering     `json:"buffering,omitempty"`
	StripPrefix   *StripPrefix   `json:"stripPrefix,omitempty"`
	RedirectRegex *RedirectRegex `json:"redirectRegex,omitempty"`
	Chain         *Chain         `json:"chain,omitempty"`
}

// Headers defines security and custom response headers.
type Headers struct {
	STSSeconds              int64             `json:"stsSeconds,omitempty"`
	STSIncludeSubdomains    bool              `json:"stsIncludeSubdomains,omitempty"`
	ContentTypeNosniff      bool              `json:"contentTypeNosniff,omitempty"`
	CustomFrameOptionsValue string            `json:"customFrameOptionsValue,omitempty"`
	ReferrerPolicy          string            `json:"referrerPolicy,omitempty"`
	CustomResponseHeaders   map[string]string `json:"customResponseHeaders,omitempty"`
}

// RateLimit defines client request rate limiting.
type RateLimit struct {
	Average         int              `json:"average"`
	Burst           int              `json:"burst"`
	Period          string           `json:"period"`
	SourceCriterion *SourceCriterion `json:"sourceCriterion,omitempty"`
}

// SourceCriterion configures IP extraction strategy for rate limiting.
type SourceCriterion struct {
	IPStrategy IPStrategy `json:"ipStrategy"`
}

// IPStrategy defines depth for extracting client IP behind proxies.
type IPStrategy struct {
	Depth int `json:"depth"`
}

// Compress enables response gzip/zstd/brotli compression.
type Compress struct {
	MinResponseBodyBytes int `json:"minResponseBodyBytes"`
}

// BasicAuth configures HTTP Basic Authentication using htpasswd credentials.
type BasicAuth struct {
	Users []string `json:"users"`
}

// IPAllowList configures CIDR access filtering.
type IPAllowList struct {
	SourceRange []string    `json:"sourceRange"`
	IPStrategy  *IPStrategy `json:"ipStrategy,omitempty"`
}

// Buffering configures maximum request body limit.
type Buffering struct {
	MaxRequestBodyBytes int64 `json:"maxRequestBodyBytes"`
}

// StripPrefix removes path prefixes before forwarding to backend apps.
type StripPrefix struct {
	Prefixes []string `json:"prefixes"`
}

// RedirectRegex configures 301 redirects using regex replacement.
type RedirectRegex struct {
	Regex       string `json:"regex"`
	Replacement string `json:"replacement"`
	Permanent   bool   `json:"permanent"`
}

// Chain groups a sequence of middlewares.
type Chain struct {
	Middlewares []string `json:"middlewares"`
}

// TLSConfig defines TLS options and certificates.
type TLSConfig struct {
	Options map[string]TLSOptions `json:"options,omitempty"`
}

// TLSOptions defines strict TLS parameters.
type TLSOptions struct {
	MinVersion   string   `json:"minVersion"`
	SNIStrict    bool     `json:"sniStrict"`
	CipherSuites []string `json:"cipherSuites,omitempty"`
}

// StaticConfig represents Traefik's static startup configuration.
type StaticConfig struct {
	Global                StaticGlobal                    `json:"global"`
	Log                   LogConfig                       `json:"log"`
	AccessLog             *AccessLogConfig                `json:"accessLog,omitempty"`
	EntryPoints           map[string]EntryPoint           `json:"entryPoints"`
	Providers             ProvidersConfig                 `json:"providers"`
	CertificatesResolvers map[string]CertificatesResolver `json:"certificatesResolvers"`
	API                   APIConfig                       `json:"api"`
}

// StaticGlobal defines global Traefik telemetry flags.
type StaticGlobal struct {
	CheckNewVersion    bool `json:"checkNewVersion"`
	SendAnonymousUsage bool `json:"sendAnonymousUsage"`
}

// LogConfig defines logging verbosity and format.
type LogConfig struct {
	Level  string `json:"level"`
	Format string `json:"format"`
}

// AccessLogConfig defines access log formatting.
type AccessLogConfig struct {
	Format        string `json:"format"`
	BufferingSize int    `json:"bufferingSize,omitempty"`
}

// EntryPoint defines network listening endpoints.
type EntryPoint struct {
	Address          string            `json:"address"`
	HTTP             EntryPointHTTP    `json:"http"`
	HTTP3            *struct{}         `json:"http3,omitempty"`
	ForwardedHeaders *ForwardedHeaders `json:"forwardedHeaders,omitempty"`
}

// EntryPointHTTP configures HTTP redirection and security defaults.
type EntryPointHTTP struct {
	Redirections         *Redirections     `json:"redirections,omitempty"`
	TLS                  *RouterTLS        `json:"tls,omitempty"`
	AliasHeadersStrategy string            `json:"aliasHeadersStrategy"`
	EncodedCharacters    EncodedCharacters `json:"encodedCharacters"`
}

// Redirections defines automatic entrypoint redirection.
type Redirections struct {
	EntryPoint RedirectEntryPoint `json:"entryPoint"`
}

// RedirectEntryPoint targets destination entrypoint and scheme.
type RedirectEntryPoint struct {
	To        string `json:"to"`
	Scheme    string `json:"scheme"`
	Permanent bool   `json:"permanent"`
}

// EncodedCharacters configures URL path security decoding.
type EncodedCharacters struct {
	AllowEncodedBackSlash     bool `json:"allowEncodedBackSlash"`
	AllowEncodedNullCharacter bool `json:"allowEncodedNullCharacter"`
}

// ForwardedHeaders defines trusted reverse proxy IP CIDRs.
type ForwardedHeaders struct {
	TrustedIPs []string `json:"trustedIPs"`
}

// ProvidersConfig configures file and external providers.
type ProvidersConfig struct {
	File FileProvider `json:"file"`
}

// FileProvider watches a directory of dynamic configurations.
type FileProvider struct {
	Directory string `json:"directory"`
	Watch     bool   `json:"watch"`
}

// CertificatesResolver defines ACME automated certificate issuance.
type CertificatesResolver struct {
	ACME ACMEConfig `json:"acme"`
}

// ACMEConfig defines Let's Encrypt / ACME client parameters.
type ACMEConfig struct {
	Email         string         `json:"email,omitempty"`
	Storage       string         `json:"storage"`
	CAServer      string         `json:"caServer,omitempty"`
	HTTPChallenge *HTTPChallenge `json:"httpChallenge,omitempty"`
}

// HTTPChallenge configures the HTTP-01 challenge solver entrypoint.
type HTTPChallenge struct {
	EntryPoint string `json:"entryPoint"`
}

// APIConfig configures the Traefik dashboard.
type APIConfig struct {
	Dashboard bool `json:"dashboard"`
}
