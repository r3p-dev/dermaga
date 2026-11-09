package traefik

import (
	"strconv"
)

// Static is the part of Traefik's static configuration dermaga sets.
type Static struct {
	Global                StaticGlobal                    `json:"global"`
	Log                   Log                             `json:"log"`
	AccessLog             *AccessLog                      `json:"accessLog,omitempty"`
	EntryPoints           map[string]EntryPoint           `json:"entryPoints"`
	Providers             Providers                       `json:"providers"`
	CertificatesResolvers map[string]CertificatesResolver `json:"certificatesResolvers"`
	API                   API                             `json:"api"`
}

type AccessLog struct {
	FilePath      string `json:"filePath"`
	Format        string `json:"format"`
	BufferingSize int    `json:"bufferingSize,omitempty"`
}

type StaticGlobal struct {
	CheckNewVersion    bool `json:"checkNewVersion"`
	SendAnonymousUsage bool `json:"sendAnonymousUsage"`
}

type Log struct {
	Level  string `json:"level"`
	Format string `json:"format"`
}

type EntryPoint struct {
	Address          string            `json:"address"`
	HTTP             EntryPointHTTP    `json:"http"`
	HTTP3            *struct{}         `json:"http3,omitempty"`
	ForwardedHeaders *ForwardedHeaders `json:"forwardedHeaders,omitempty"`
}

type EntryPointHTTP struct {
	Redirections         *Redirections     `json:"redirections,omitempty"`
	TLS                  *RouterTLS        `json:"tls,omitempty"`
	AliasHeadersStrategy string            `json:"aliasHeadersStrategy"`
	EncodedCharacters    EncodedCharacters `json:"encodedCharacters"`
}

type Redirections struct {
	EntryPoint RedirectEntryPoint `json:"entryPoint"`
}

type RedirectEntryPoint struct {
	To        string `json:"to"`
	Scheme    string `json:"scheme"`
	Permanent bool   `json:"permanent"`
}

type EncodedCharacters struct {
	AllowEncodedBackSlash     bool `json:"allowEncodedBackSlash"`
	AllowEncodedNullCharacter bool `json:"allowEncodedNullCharacter"`
}

type ForwardedHeaders struct {
	TrustedIPs []string `json:"trustedIPs"`
}

type Providers struct {
	File FileProvider `json:"file"`
}

type FileProvider struct {
	Directory string `json:"directory"`
	Watch     bool   `json:"watch"`
}

type CertificatesResolver struct {
	ACME ACME `json:"acme"`
}

type ACME struct {
	Email         string        `json:"email,omitempty"`
	Storage       string        `json:"storage"`
	CAServer      string        `json:"caServer,omitempty"`
	HTTPChallenge HTTPChallenge `json:"httpChallenge"`
}

type HTTPChallenge struct {
	EntryPoint string `json:"entryPoint"`
}

type API struct {
	Dashboard bool `json:"dashboard"`
}

// StaticConfig renders traefik.yml: vps-setup.sh's render_traefik_static
// without the docker provider (file provider only, BRIEF §2), without
// secure@file on websecure (each app router carries its own chain, so apps can
// change or drop parts of it, D27), and with the internal verify entrypoint (D7).
// Entry point names web/websecure match FileDescriptorName= of the systemd sockets.
func StaticConfig(site Site) ([]byte, error) {
	if err := site.validate(); err != nil {
		return nil, err
	}
	// drop request headers that alias managed ones (X_Forwarded_For), refuse encoded \ and NUL
	hardening := EntryPointHTTP{AliasHeadersStrategy: "delete"}
	var fwd *ForwardedHeaders
	if len(site.TrustedProxies) > 0 {
		fwd = &ForwardedHeaders{TrustedIPs: site.TrustedProxies}
	}
	web := hardening
	web.Redirections = &Redirections{EntryPoint: RedirectEntryPoint{To: "websecure", Scheme: "https", Permanent: true}}
	websecure := hardening
	websecure.TLS = &RouterTLS{CertResolver: certResolver}
	acme := ACME{Email: site.ACMEEmail, Storage: "/acme/acme.json", HTTPChallenge: HTTPChallenge{EntryPoint: "web"}}
	if site.ACMEStaging {
		acme.CAServer = "https://acme-staging-v02.api.letsencrypt.org/directory"
	}
	return marshal(Static{
		Global:    StaticGlobal{},
		Log:       Log{Level: "WARN", Format: "json"},
		AccessLog: &AccessLog{FilePath: "/var/log/traefik/access.log", Format: "json", BufferingSize: 100},
		EntryPoints: map[string]EntryPoint{
			"web":       {Address: ":80", HTTP: web, ForwardedHeaders: fwd},
			"websecure": {Address: ":443", HTTP: websecure, HTTP3: &struct{}{}, ForwardedHeaders: fwd},
			// plain HTTP inside the container, never published: only token routers use it
			VerifyEntryPoint: {Address: ":" + strconv.Itoa(VerifyPort), HTTP: hardening},
		},
		Providers:             Providers{File: FileProvider{Directory: "/etc/traefik/dynamic", Watch: true}},
		CertificatesResolvers: map[string]CertificatesResolver{certResolver: {ACME: acme}},
		API:                   API{Dashboard: false},
	})
}
