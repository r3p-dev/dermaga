package traefik

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"dermaga/internal/name"
)

// RenderStatic renders Traefik's static startup configuration as pretty-printed JSON.
func RenderStatic(spec StaticSpec) ([]byte, error) {
	dynamicDir := spec.DynamicDir
	if dynamicDir == "" {
		dynamicDir = "/etc/traefik/dynamic"
	}
	acmeStorage := spec.ACMEStorage
	if acmeStorage == "" {
		acmeStorage = "/acme/acme.json"
	}

	cfg := StaticConfig{
		Global: StaticGlobal{
			CheckNewVersion:    false,
			SendAnonymousUsage: false,
		},
		Log: LogConfig{
			Level:  "WARN",
			Format: "json",
		},
		EntryPoints: map[string]EntryPoint{
			"web": {
				Address: ":80",
				HTTP: EntryPointHTTP{
					Redirections: &Redirections{
						EntryPoint: RedirectEntryPoint{
							To:        "websecure",
							Scheme:    "https",
							Permanent: true,
						},
					},
					AliasHeadersStrategy: "delete",
					EncodedCharacters: EncodedCharacters{
						AllowEncodedBackSlash:     false,
						AllowEncodedNullCharacter: false,
					},
				},
			},
			"websecure": {
				Address: ":443",
				HTTP3:   &struct{}{},
				HTTP: EntryPointHTTP{
					TLS: &RouterTLS{
						CertResolver: CertResolverName,
					},
					AliasHeadersStrategy: "delete",
					EncodedCharacters: EncodedCharacters{
						AllowEncodedBackSlash:     false,
						AllowEncodedNullCharacter: false,
					},
				},
			},
		},
		Providers: ProvidersConfig{
			File: FileProvider{
				Directory: dynamicDir,
				Watch:     true,
			},
		},
		CertificatesResolvers: map[string]CertificatesResolver{
			CertResolverName: {
				ACME: ACMEConfig{
					Email:   spec.ACMEEmail,
					Storage: acmeStorage,
					HTTPChallenge: &HTTPChallenge{
						EntryPoint: "web",
					},
				},
			},
		},
		API: APIConfig{
			Dashboard: false,
		},
	}

	if spec.AccessLog {
		cfg.AccessLog = &AccessLogConfig{
			Format:        "json",
			BufferingSize: 64,
		}
	}

	if len(spec.TrustedProxies) > 0 {
		fwd := &ForwardedHeaders{TrustedIPs: spec.TrustedProxies}
		web := cfg.EntryPoints["web"]
		web.ForwardedHeaders = fwd
		cfg.EntryPoints["web"] = web

		websec := cfg.EntryPoints["websecure"]
		websec.ForwardedHeaders = fwd
		cfg.EntryPoints["websecure"] = websec
	}

	if spec.CAServer != "" {
		resolver := cfg.CertificatesResolvers[CertResolverName]
		resolver.ACME.CAServer = spec.CAServer
		cfg.CertificatesResolvers[CertResolverName] = resolver
	} else if spec.ACMEStaging {
		resolver := cfg.CertificatesResolvers[CertResolverName]
		resolver.ACME.CAServer = "https://acme-staging-v02.api.letsencrypt.org/directory"
		cfg.CertificatesResolvers[CertResolverName] = resolver
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("%s\n%s\n", StaticMarker(), string(data))), nil
}

// RenderGlobal renders the 00-global.yml dynamic configuration establishing server-wide default middlewares and TLS.
func RenderGlobal(spec GlobalSpec) ([]byte, error) {
	rateAvg := spec.DefaultRateLimitAverage
	if rateAvg <= 0 {
		rateAvg = 100
	}
	rateBurst := spec.DefaultRateLimitBurst
	if rateBurst <= 0 {
		rateBurst = 200
	}
	compressMin := spec.DefaultCompressMinBytes
	if compressMin <= 0 {
		compressMin = 1024
	}

	rateLimitMid := &RateLimit{
		Average: rateAvg,
		Burst:   rateBurst,
		Period:  "1s",
	}
	if len(spec.TrustedProxies) > 0 {
		rateLimitMid.SourceCriterion = &SourceCriterion{
			IPStrategy: IPStrategy{Depth: 1},
		}
	}

	middlewares := map[string]Middleware{
		"default-security-headers": {
			Headers: &Headers{
				STSSeconds:              31536000,
				STSIncludeSubdomains:    true,
				ContentTypeNosniff:      true,
				CustomFrameOptionsValue: "SAMEORIGIN",
				ReferrerPolicy:          "strict-origin-when-cross-origin",
				CustomResponseHeaders: map[string]string{
					"Server":       "",
					"X-Powered-By": "",
				},
			},
		},
		"default-rate-limit": {
			RateLimit: rateLimitMid,
		},
		"default-compress": {
			Compress: &Compress{
				MinResponseBodyBytes: compressMin,
			},
		},
		"secure": {
			Chain: &Chain{
				Middlewares: []string{
					"default-security-headers",
					"default-rate-limit",
					"default-compress",
				},
			},
		},
	}

	tlsOptions := map[string]TLSOptions{
		"default": {
			MinVersion: "VersionTLS12",
			SNIStrict:  true,
			CipherSuites: []string{
				"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
				"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
				"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305",
				"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305",
			},
		},
	}

	doc := Dynamic{
		HTTP: &HTTPConfig{
			Middlewares: middlewares,
		},
		TLS: &TLSConfig{
			Options: tlsOptions,
		},
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("%s\n%s\n", GlobalMarker(), string(data))), nil
}

// RenderApp renders an application's dynamic configuration file (app-<name>.yml) as pretty-printed JSON.
func RenderApp(spec AppSpec) ([]byte, error) {
	if err := ValidateAppSpec(spec); err != nil {
		return nil, fmt.Errorf("validating app spec: %w", err)
	}

	appIdentifier := "app-" + spec.AppName
	routers := make(map[string]Router)
	services := make(map[string]Service)
	middlewares := make(map[string]Middleware)

	var appMiddlewareChain []string

	// 1. Headers Middleware
	headersMiddlewareName := appIdentifier + "-headers"
	respHeaders := map[string]string{
		"Server":       "",
		"X-Powered-By": "",
	}

	// Permissions Policy: vps-setup default unless overridden or removed
	if spec.Headers.PermissionsPolicy != "" {
		respHeaders["Permissions-Policy"] = spec.Headers.PermissionsPolicy
	} else if spec.Headers.CustomResponseHeaders != nil && spec.Headers.CustomResponseHeaders["Permissions-Policy"] == "" {
		// explicitly removed by app
		respHeaders["Permissions-Policy"] = ""
	} else {
		respHeaders["Permissions-Policy"] = DefaultPermissionsPolicy
	}

	for k, v := range spec.Headers.CustomResponseHeaders {
		respHeaders[k] = v
	}

	headersPayload := &Headers{
		CustomResponseHeaders: respHeaders,
	}
	if !spec.Headers.DisableSecurityHeaders {
		headersPayload.STSSeconds = 31536000
		headersPayload.STSIncludeSubdomains = true
		headersPayload.ContentTypeNosniff = true
		headersPayload.CustomFrameOptionsValue = "SAMEORIGIN"
		headersPayload.ReferrerPolicy = "strict-origin-when-cross-origin"
	}
	middlewares[headersMiddlewareName] = Middleware{Headers: headersPayload}
	appMiddlewareChain = append(appMiddlewareChain, headersMiddlewareName)

	// 2. IP AllowList Middleware
	if len(spec.Middleware.IPAllowList) > 0 {
		allowMidName := appIdentifier + "-allowlist"
		middlewares[allowMidName] = Middleware{
			IPAllowList: &IPAllowList{
				SourceRange: spec.Middleware.IPAllowList,
			},
		}
		appMiddlewareChain = append(appMiddlewareChain, allowMidName)
	}

	// 3. Basic Auth Middleware
	if len(spec.Middleware.BasicAuth) > 0 {
		authMidName := appIdentifier + "-auth"
		var users []string
		for _, u := range spec.Middleware.BasicAuth {
			users = append(users, fmt.Sprintf("%s:%s", u.Username, u.Hash))
		}
		middlewares[authMidName] = Middleware{
			BasicAuth: &BasicAuth{Users: users},
		}
		appMiddlewareChain = append(appMiddlewareChain, authMidName)
	}

	// 4. Rate Limit Middleware (if custom configured)
	if spec.Middleware.RateLimit != nil {
		rateMidName := appIdentifier + "-ratelimit"
		middlewares[rateMidName] = Middleware{
			RateLimit: &RateLimit{
				Average: spec.Middleware.RateLimit.Average,
				Burst:   spec.Middleware.RateLimit.Burst,
				Period:  "1s",
			},
		}
		appMiddlewareChain = append(appMiddlewareChain, rateMidName)
	}

	// 5. Compression Middleware
	if spec.Middleware.Compress != nil && *spec.Middleware.Compress {
		compMidName := appIdentifier + "-compress"
		middlewares[compMidName] = Middleware{
			Compress: &Compress{
				MinResponseBodyBytes: 1024,
			},
		}
		appMiddlewareChain = append(appMiddlewareChain, compMidName)
	}

	// 6. Request Body Buffering Limit
	if spec.Middleware.MaxRequestBodyBytes > 0 {
		buffMidName := appIdentifier + "-buffering"
		middlewares[buffMidName] = Middleware{
			Buffering: &Buffering{
				MaxRequestBodyBytes: spec.Middleware.MaxRequestBodyBytes,
			},
		}
		appMiddlewareChain = append(appMiddlewareChain, buffMidName)
	}

	// 7. Path Prefix Stripping
	var stripPrefixes []string
	for _, d := range spec.Domains {
		if d.PathPrefix != "" && d.StripPrefix {
			stripPrefixes = append(stripPrefixes, d.PathPrefix)
		}
	}
	if len(stripPrefixes) > 0 {
		sort.Strings(stripPrefixes)
		stripMidName := appIdentifier + "-stripprefix"
		middlewares[stripMidName] = Middleware{
			StripPrefix: &StripPrefix{Prefixes: stripPrefixes},
		}
		appMiddlewareChain = append(appMiddlewareChain, stripMidName)
	}

	// 8. Construct Routing Rules
	var activeHosts []string
	var primaryDomain string
	var pathPrefix string

	for idx, d := range spec.Domains {
		if d.IsPrimary {
			primaryDomain = d.Domain
		}
		if d.PathPrefix != "" && pathPrefix == "" {
			pathPrefix = d.PathPrefix
		}

		if d.RedirectTo != "" {
			// Domain has a 301 redirect configured
			redirectMidName := fmt.Sprintf("%s-redirect-%d", appIdentifier, idx)
			middlewares[redirectMidName] = Middleware{
				RedirectRegex: &RedirectRegex{
					Regex:       fmt.Sprintf(`^https?://%s/(.*)`, regexp.QuoteMeta(d.Domain)),
					Replacement: fmt.Sprintf("https://%s/${1}", d.RedirectTo),
					Permanent:   true,
				},
			}
			redirectRouterName := fmt.Sprintf("%s-redirect-%d", appIdentifier, idx)
			routers[redirectRouterName] = Router{
				EntryPoints: []string{"websecure"},
				Rule:        fmt.Sprintf("Host(`%s`)", d.Domain),
				Service:     appIdentifier,
				Middlewares: []string{redirectMidName},
				TLS:         &RouterTLS{CertResolver: CertResolverName},
			}
		} else {
			activeHosts = append(activeHosts, d.Domain)
		}
	}

	sort.Strings(activeHosts)

	// Main Application Router
	if len(activeHosts) > 0 {
		var hostRules []string
		for _, h := range activeHosts {
			hostRules = append(hostRules, fmt.Sprintf("Host(`%s`)", h))
		}
		mainRule := strings.Join(hostRules, " || ")
		if len(hostRules) > 1 {
			mainRule = "(" + mainRule + ")"
		}
		if pathPrefix != "" {
			mainRule = fmt.Sprintf("%s && PathPrefix(`%s`)", mainRule, pathPrefix)
		}

		routers[appIdentifier] = Router{
			EntryPoints: []string{"websecure"},
			Rule:        mainRule,
			Service:     appIdentifier,
			Middlewares: appMiddlewareChain,
			TLS:         &RouterTLS{CertResolver: CertResolverName},
		}
	}

	// Main Service (points to active release container)
	services[appIdentifier] = Service{
		LoadBalancer: LoadBalancer{
			Servers: []Server{
				{
					URL: fmt.Sprintf("http://%s:%d", name.AppContainerName(spec.AppName, spec.ReleaseNumber), spec.Port),
				},
			},
		},
	}

	// 9. Switch Rendering (Verify Router and Candidate Service)
	if spec.Switch != nil {
		candidateServiceName := appIdentifier + "-candidate"
		services[candidateServiceName] = Service{
			LoadBalancer: LoadBalancer{
				Servers: []Server{
					{
						URL: fmt.Sprintf("http://%s:%d", name.AppContainerName(spec.AppName, spec.Switch.CandidateReleaseNumber), spec.Switch.CandidatePort),
					},
				},
			},
		}

		verifyRouterName := appIdentifier + "-verify"
		verifyRule := fmt.Sprintf("Host(`%s`) && Header(`%s`, `%s`)", primaryDomain, VerifyHeader, spec.Switch.VerifyToken)
		if pathPrefix != "" {
			verifyRule = fmt.Sprintf("Host(`%s`) && PathPrefix(`%s`) && Header(`%s`, `%s`)", primaryDomain, pathPrefix, VerifyHeader, spec.Switch.VerifyToken)
		}

		// The token comes back as a response header only through this router, so seeing it proves
		// Traefik loaded this exact file version (DEC-33). First in the chain: added even on 401/403.
		ackMiddlewareName := appIdentifier + "-verify-ack"
		middlewares[ackMiddlewareName] = Middleware{Headers: &Headers{
			CustomResponseHeaders: map[string]string{VerifyHeader: spec.Switch.VerifyToken},
		}}
		routers[verifyRouterName] = Router{
			EntryPoints: []string{"websecure"},
			Rule:        verifyRule,
			Service:     candidateServiceName,
			Middlewares: append([]string{ackMiddlewareName}, appMiddlewareChain...),
			TLS:         &RouterTLS{CertResolver: CertResolverName},
			Priority:    10000,
		}
	}

	doc := Dynamic{
		HTTP: &HTTPConfig{
			Routers:     routers,
			Services:    services,
			Middlewares: middlewares,
		},
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("%s\n%s\n", AppMarker(spec.AppName), string(data))), nil
}

// AppMarker returns the explicit ownership marker comment for an app dynamic file.
func AppMarker(appName string) string {
	return fmt.Sprintf("# managed by dermaga; app=%s", appName)
}

// GlobalMarker returns the explicit ownership marker comment for the global dynamic config.
func GlobalMarker() string {
	return "# managed by dermaga; global"
}

// StaticMarker returns the explicit ownership marker comment for the static config.
func StaticMarker() string {
	return "# managed by dermaga; static"
}

// HasAppMarker reports whether content contains the expected ownership marker for appName.
func HasAppMarker(content, appName string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return false
	}
	expected := AppMarker(appName)
	for i := 0; i < len(lines) && i < 3; i++ {
		line := strings.TrimSpace(lines[i])
		if line == expected || strings.HasPrefix(line, "# managed by dermaga; app="+appName) {
			return true
		}
	}
	return false
}

// HasGlobalMarker reports whether content contains the global ownership marker.
func HasGlobalMarker(content string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return false
	}
	for i := 0; i < len(lines) && i < 3; i++ {
		if strings.TrimSpace(lines[i]) == GlobalMarker() {
			return true
		}
	}
	return false
}

// HasAnyMarker reports whether content contains any dermaga management marker.
func HasAnyMarker(content string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return false
	}
	for i := 0; i < len(lines) && i < 3; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "# managed by dermaga") {
			return true
		}
	}
	return false
}

// StripHeaderComment strips any leading comment lines (e.g. # managed by dermaga)
// so the remaining bytes can be parsed by encoding/json.
func StripHeaderComment(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	start := 0
	for start < len(lines) {
		trimmed := strings.TrimSpace(lines[start])
		if len(trimmed) > 0 && trimmed[0] == '#' {
			start++
		} else {
			break
		}
	}
	return []byte(strings.Join(lines[start:], "\n"))
}

// AppConfigFileName returns the dynamic configuration filename for an app (e.g. "app-demo.yml").
func AppConfigFileName(appName string) string {
	return fmt.Sprintf("app-%s.yml", appName)
}

// GlobalConfigFileName returns the filename for global dynamic configuration.
func GlobalConfigFileName() string {
	return "00-global.yml"
}

// LiveRelease reads which release an app's route file sends traffic to (the main service), and
// whether a verify router is still present. Recovery after a crash mid-switch uses it (DEP-10).
func LiveRelease(data []byte, appName string) (release int, hasVerify bool, ok bool) {
	var doc Dynamic
	if err := json.Unmarshal(StripHeaderComment(data), &doc); err != nil || doc.HTTP == nil {
		return 0, false, false
	}
	svc, found := doc.HTTP.Services["app-"+appName]
	if !found || len(svc.LoadBalancer.Servers) != 1 {
		return 0, false, false
	}
	prefix := "http://" + name.AppPrefix + appName + "-r" // name.AppContainerName without the number
	rest, cut := strings.CutPrefix(svc.LoadBalancer.Servers[0].URL, prefix)
	if !cut {
		return 0, false, false
	}
	num, _, _ := strings.Cut(rest, ":")
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 {
		return 0, false, false
	}
	_, hasVerify = doc.HTTP.Routers["app-"+appName+"-verify"]
	return n, hasVerify, true
}
