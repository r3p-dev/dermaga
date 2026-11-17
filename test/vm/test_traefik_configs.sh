#!/usr/bin/env bash
# Test Traefik golden configurations in an isolated throwaway Traefik container on dermaga-vm.
# Validates Traefik API router/service/middleware status, exercises real curl requests,
# and verifies failure on a negative control.
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GOLDEN_DIR="$PROJECT_ROOT/internal/traefik/testdata"

echo "=== Traefik Golden Configurations Verification on dermaga-vm ==="
echo "Timestamp: $(date -Is)"
echo "Git commit: $(git -C "$PROJECT_ROOT" rev-parse --short HEAD)"
STATUS_OUTPUT="$(git -C "$PROJECT_ROOT" status --short)"
if [[ -n "$STATUS_OUTPUT" ]]; then
    echo "Working tree status: dirty"
    echo "$STATUS_OUTPUT"
else
    echo "Working tree status: clean"
fi
echo "Host: $(ssh dermaga-vm hostname)"
echo

# 1. Query running Traefik container on dermaga-vm for exact image and digest
TRAEFIK_IMG=$(ssh dermaga-vm "podman inspect traefik --format '{{.Config.Image}}'")
TRAEFIK_DIGEST=$(ssh dermaga-vm "podman image inspect $TRAEFIK_IMG --format '{{.Digest}}'")
PINNED_IMAGE="${TRAEFIK_IMG}@${TRAEFIK_DIGEST}"
echo "Discovered live Traefik container image: $TRAEFIK_IMG"
echo "Discovered live Traefik image digest: $TRAEFIK_DIGEST"
echo "Using pinned Traefik image: $PINNED_IMAGE"
echo

# 2. Setup isolated remote environment on dermaga-vm
REMOTE_TMP=$(ssh dermaga-vm 'mktemp -d /tmp/traefik-verify-XXXXXX')
NET_NAME="test-traefik-net-$$"
NET_ALLOW="test-traefik-allow-$$"
TRAEFIK_CONT="test-traefik-$$"
BACKEND_MAIN="test-backend-main-$$"
BACKEND_CANDIDATE="test-backend-cand-$$"
BACKEND_PORT80="test-backend-p80-$$"
BACKEND_PORT3000="test-backend-p3000-$$"
BACKEND_PORT4000="test-backend-p4000-$$"
BACKEND_PORT9000="test-backend-p9000-$$"
BACKEND_PORT8443="test-backend-p8443-$$"

cleanup() {
    echo
    echo "Cleaning up throwaway test containers and network on dermaga-vm..."
    ssh dermaga-vm "
        podman rm -f $TRAEFIK_CONT $BACKEND_MAIN $BACKEND_CANDIDATE $BACKEND_PORT80 $BACKEND_PORT3000 $BACKEND_PORT4000 $BACKEND_PORT9000 $BACKEND_PORT8443 2>/dev/null || true
        podman network rm $NET_NAME $NET_ALLOW 2>/dev/null || true
        rm -rf $REMOTE_TMP
    "
    echo "Cleanup complete (§8: idempotent, no residue)."
}
trap cleanup EXIT

echo "Creating isolated test directory: $REMOTE_TMP"
ssh dermaga-vm "
    chmod 755 $REMOTE_TMP
    mkdir -p $REMOTE_TMP/dynamic $REMOTE_TMP/acme $REMOTE_TMP/tls
    touch $REMOTE_TMP/acme/acme.json
    chmod 600 $REMOTE_TMP/acme/acme.json
    openssl req -x509 -newkey rsa:2048 -nodes -keyout $REMOTE_TMP/tls/key.pem -out $REMOTE_TMP/tls/cert.pem -days 1 \
      -subj '/CN=example.com' \
      -addext 'subjectAltName=DNS:example.com,DNS:*.example.com,DNS:landing.com,DNS:*.landing.com,DNS:landing.net,DNS:*.landing.net' 2>/dev/null
    cat << 'EOF' > $REMOTE_TMP/dynamic/01-test-tls.yml
tls:
  certificates:
    - certFile: /etc/traefik/tls/cert.pem
      keyFile: /etc/traefik/tls/key.pem
EOF
"

# 3. Create responder script on remote VM
ssh dermaga-vm "cat << 'EOF' > $REMOTE_TMP/responder_release1.sh
#!/bin/sh
read -r req || true
path=\$(printf '%s' \"\$req\" | cut -d' ' -f2)
while IFS= read -r line; do
  line=\$(printf '%s' \"\$line\" | tr -d '\r')
  [ -z \"\$line\" ] && break
done
body=\"release 1 response for path=\$path\"
len=\$(printf '%s' \"\$body\" | wc -c)
printf \"HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nConnection: close\r\nX-Echo-Path: %s\r\nContent-Length: %d\r\n\r\n%s\" \"\$path\" \"\$len\" \"\$body\"
EOF
chmod +x $REMOTE_TMP/responder_release1.sh

cat << 'EOF' > $REMOTE_TMP/responder_candidate.sh
#!/bin/sh
read -r req || true
path=\$(printf '%s' \"\$req\" | cut -d' ' -f2)
while IFS= read -r line; do
  line=\$(printf '%s' \"\$line\" | tr -d '\r')
  [ -z \"\$line\" ] && break
done
body=\"candidate release 2 response for path=\$path\"
len=\$(printf '%s' \"\$body\" | wc -c)
printf \"HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nConnection: close\r\nX-Echo-Path: %s\r\nContent-Length: %d\r\n\r\n%s\" \"\$path\" \"\$len\" \"\$body\"
EOF
chmod +x $REMOTE_TMP/responder_candidate.sh
"

ssh dermaga-vm "cat > $REMOTE_TMP/check_router_error.py" << 'EOF'
import json, sys, time, urllib.request

router = sys.argv[1]
expected_substr = sys.argv[2]
url = f"http://127.0.0.1:18088/api/http/routers/{router}@file"

for _ in range(40):
    try:
        req = urllib.request.Request(url)
        with urllib.request.urlopen(req) as resp:
            data = json.loads(resp.read().decode())
            is_disabled = (data.get("status") == "disabled")
            errors = data.get("error", [])
            has_err = any(expected_substr in e for e in errors)
            if is_disabled and has_err:
                print("; ".join(errors))
                sys.exit(0)
    except Exception:
        pass
    time.sleep(0.1)

print(f"FAIL: router {router} was not disabled or did not report error containing {expected_substr}")
sys.exit(1)
EOF

# 4. Generate static config on non-colliding ports with local ACME stub (never real Let's Encrypt)
ssh dermaga-vm "cat << 'EOF' > $REMOTE_TMP/traefik.json
{
  \"global\": { \"checkNewVersion\": false, \"sendAnonymousUsage\": false },
  \"log\": { \"level\": \"INFO\", \"format\": \"json\" },
  \"entryPoints\": {
    \"traefik\": { \"address\": \":18088\" },
    \"web\": {
      \"address\": \":18080\",
      \"http\": {
        \"redirections\": {
          \"entryPoint\": { \"to\": \"websecure\", \"scheme\": \"https\", \"permanent\": true }
        }
      }
    },
    \"websecure\": {
      \"address\": \":18443\"
    }
  },
  \"providers\": {
    \"file\": { \"directory\": \"/etc/traefik/dynamic\", \"watch\": true }
  },
  \"certificatesResolvers\": {
    \"le\": {
      \"acme\": {
        \"email\": \"admin@example.com\",
        \"storage\": \"/acme/acme.json\",
        \"caServer\": \"https://127.0.0.1:14000/dir\",
        \"httpChallenge\": { \"entryPoint\": \"web\" }
      }
    }
  },
  \"api\": { \"insecure\": true }
}
EOF"

# 5. Stage all dynamic golden files into $REMOTE_TMP/dynamic/
echo "Staging dynamic golden configuration files..."
for f in "$GOLDEN_DIR"/*.golden; do
    base="$(basename "$f" .golden)"
    if [[ "$base" == "static-config" || "$base" == "vps-setup-headers" ]]; then
        continue
    fi
    echo "  Staging $base.golden -> $base.yml"
    scp -q "$f" "dermaga-vm:$REMOTE_TMP/dynamic/$base.yml"
done

# 6. Create test bridge networks on dermaga-vm
# Primary network uses 10.99.1.0/24 so host port forwarding (from gateway 10.99.1.1) is outside the allowlist.
echo "Creating isolated primary bridge network $NET_NAME (subnet 10.99.1.0/24)..."
ssh dermaga-vm "podman network create --subnet 10.99.1.0/24 $NET_NAME"

# Allowed-source network uses 192.168.1.0/24 to simulate an allowed client per allowlist.golden
echo "Creating isolated allowed-source network $NET_ALLOW (subnet 192.168.1.0/24)..."
ssh dermaga-vm "podman network create --subnet 192.168.1.0/24 $NET_ALLOW"

# 7. Start backend responders with network aliases matching the services in the golden files
echo "Starting throwaway backend echo responders on $NET_NAME..."
ssh dermaga-vm "
    podman run -d --rm --name $BACKEND_MAIN --network $NET_NAME \
      --network-alias app-portfolio-r1 \
      --network-alias app-api-service-r3 \
      --network-alias app-internal-portal-r1 \
      --network-alias app-custom-headers-app-r1 \
      --network-alias app-web-app-r1 \
      -v $REMOTE_TMP/responder_release1.sh:/responder.sh:ro \
      --entrypoint /bin/sh \
      docker.io/alpine/socat:1.8.1.3@sha256:71131702f671a88b3808a70b7330f9679cd9b9933adcafadc191508edfe55bba \
      -c 'socat TCP-LISTEN:8080,fork,reuseaddr EXEC:/responder.sh'

    podman run -d --rm --name $BACKEND_CANDIDATE --network $NET_NAME \
      --network-alias app-web-app-r2 \
      -v $REMOTE_TMP/responder_candidate.sh:/responder.sh:ro \
      --entrypoint /bin/sh \
      docker.io/alpine/socat:1.8.1.3@sha256:71131702f671a88b3808a70b7330f9679cd9b9933adcafadc191508edfe55bba \
      -c 'socat TCP-LISTEN:8080,fork,reuseaddr EXEC:/responder.sh'

    podman run -d --rm --name $BACKEND_PORT80 --network $NET_NAME \
      --network-alias app-landing-r1 \
      -v $REMOTE_TMP/responder_release1.sh:/responder.sh:ro \
      --entrypoint /bin/sh \
      docker.io/alpine/socat:1.8.1.3@sha256:71131702f671a88b3808a70b7330f9679cd9b9933adcafadc191508edfe55bba \
      -c 'socat TCP-LISTEN:80,fork,reuseaddr EXEC:/responder.sh'

    podman run -d --rm --name $BACKEND_PORT3000 --network $NET_NAME \
      --network-alias app-blog-r2 \
      -v $REMOTE_TMP/responder_release1.sh:/responder.sh:ro \
      --entrypoint /bin/sh \
      docker.io/alpine/socat:1.8.1.3@sha256:71131702f671a88b3808a70b7330f9679cd9b9933adcafadc191508edfe55bba \
      -c 'socat TCP-LISTEN:3000,fork,reuseaddr EXEC:/responder.sh'

    podman run -d --rm --name $BACKEND_PORT4000 --network $NET_NAME \
      --network-alias app-docs-service-r1 \
      -v $REMOTE_TMP/responder_release1.sh:/responder.sh:ro \
      --entrypoint /bin/sh \
      docker.io/alpine/socat:1.8.1.3@sha256:71131702f671a88b3808a70b7330f9679cd9b9933adcafadc191508edfe55bba \
      -c 'socat TCP-LISTEN:4000,fork,reuseaddr EXEC:/responder.sh'

    podman run -d --rm --name $BACKEND_PORT9000 --network $NET_NAME \
      --network-alias app-uploader-r1 \
      -v $REMOTE_TMP/responder_release1.sh:/responder.sh:ro \
      --entrypoint /bin/sh \
      docker.io/alpine/socat:1.8.1.3@sha256:71131702f671a88b3808a70b7330f9679cd9b9933adcafadc191508edfe55bba \
      -c 'socat TCP-LISTEN:9000,fork,reuseaddr EXEC:/responder.sh'

    podman run -d --rm --name $BACKEND_PORT8443 --network $NET_NAME \
      --network-alias app-secure-admin-r1 \
      -v $REMOTE_TMP/responder_release1.sh:/responder.sh:ro \
      --entrypoint /bin/sh \
      docker.io/alpine/socat:1.8.1.3@sha256:71131702f671a88b3808a70b7330f9679cd9b9933adcafadc191508edfe55bba \
      -c 'socat TCP-LISTEN:8443,fork,reuseaddr EXEC:/responder.sh'
"

# 8. Start throwaway Traefik container on $NET_NAME
echo "Starting throwaway Traefik container $TRAEFIK_CONT..."
ssh dermaga-vm "
    podman run -d --name $TRAEFIK_CONT \
      --network $NET_NAME \
      -v $REMOTE_TMP/traefik.json:/etc/traefik/traefik.json:ro \
      -v $REMOTE_TMP/dynamic:/etc/traefik/dynamic:ro \
      -v $REMOTE_TMP/tls:/etc/traefik/tls:ro \
      -v $REMOTE_TMP/acme/acme.json:/acme/acme.json:rw \
      -p 18080:18080 -p 18443:18443 -p 18088:18088 \
      $PINNED_IMAGE \
      --configfile=/etc/traefik/traefik.json

    podman network connect $NET_ALLOW $TRAEFIK_CONT
"

# 9. Poll API until configurations are loaded (polling, no arbitrary sleep)
echo
echo "Polling Traefik API (http://127.0.0.1:18088/api/http/routers) until dynamic configs load..."
ssh dermaga-vm "bash -c '
for i in {1..50}; do
  routers=\$(curl -s http://127.0.0.1:18088/api/http/routers || true)
  if echo \"\$routers\" | grep -q \"app-portfolio@file\" && \
     echo \"\$routers\" | grep -q \"app-web-app-verify@file\" && \
     echo \"\$routers\" | grep -q \"app-landing-redirect-2@file\"; then
    echo \"Traefik loaded all dynamic routers after \$((i * 100)) ms.\"
    exit 0
  fi
  sleep 0.1
done
echo \"ERROR: Timed out waiting for Traefik to load dynamic configurations!\"
exit 1
'"

# 10. Assert API router, service, and middleware status
echo
echo "--- Asserting API Router Status ---"
EXPECTED_ROUTERS=(
    "app-portfolio@file"
    "app-landing@file"
    "app-landing-redirect-2@file"
    "app-landing-redirect-3@file"
    "app-secure-admin@file"
    "app-internal-portal@file"
    "app-uploader@file"
    "app-custom-headers-app@file"
    "app-blog@file"
    "app-api-service@file"
    "app-docs-service@file"
    "app-web-app@file"
    "app-web-app-verify@file"
)

for r in "${EXPECTED_ROUTERS[@]}"; do
    STATUS=$(ssh dermaga-vm "curl -s http://127.0.0.1:18088/api/http/routers/$r")
    IS_ENABLED=$(echo "$STATUS" | jq -r '.status // empty')
    ERR_COUNT=$(echo "$STATUS" | jq -r '.error // [] | length')
    if [[ "$IS_ENABLED" == "enabled" && "$ERR_COUNT" == "0" ]]; then
        echo "Router $r: status=enabled, errors=0 -> OK"
    else
        echo "Router $r FAILED: status=$IS_ENABLED, errors=$STATUS"
        exit 1
    fi
done

echo
echo "--- Asserting Real HTTP Behavior via curl ---"

# A. Routing by Host
echo -n "1. Routing by Host (portfolio.example.com): "
CODE=$(ssh dermaga-vm "curl -k -s -o /dev/null -w '%{http_code}' --resolve portfolio.example.com:18443:127.0.0.1 https://portfolio.example.com:18443/")
if [[ "$CODE" == "200" ]]; then
    echo "PASS (HTTP 200 OK)"
else
    echo "FAIL (HTTP $CODE, expected 200)"
    ssh dermaga-vm "podman logs $TRAEFIK_CONT 2>&1 | tail -n 25"
    exit 1
fi

# B. Path prefix with strip
echo -n "2. Path prefix with strip (/api/v1/users -> backend sees /users): "
RESP_HEADERS=$(ssh dermaga-vm "curl -k -s -i --resolve example.com:18443:127.0.0.1 https://example.com:18443/api/v1/users")
if echo "$RESP_HEADERS" | grep -qi "X-Echo-Path: /users"; then
    echo "PASS (X-Echo-Path: /users received)"
else
    echo "FAIL: Expected X-Echo-Path: /users, got:"
    echo "$RESP_HEADERS"
    exit 1
fi

# C. Path prefix without strip
echo -n "3. Path prefix without strip (/docs/guide -> backend sees /docs/guide): "
RESP_HEADERS_NOSTRIP=$(ssh dermaga-vm "curl -k -s -i --resolve example.com:18443:127.0.0.1 https://example.com:18443/docs/guide")
if echo "$RESP_HEADERS_NOSTRIP" | grep -qi "X-Echo-Path: /docs/guide"; then
    echo "PASS (X-Echo-Path: /docs/guide received)"
else
    echo "FAIL: Expected X-Echo-Path: /docs/guide, got:"
    echo "$RESP_HEADERS_NOSTRIP"
    exit 1
fi

# D. Alias and www 301 redirects
echo -n "4. Alias www redirect (www.landing.com/about -> 301 to https://landing.com/about): "
REDIRECT_OUT=$(ssh dermaga-vm "curl -k -s -o /dev/null -w '%{http_code} %{redirect_url}' -H 'Host: www.landing.com' --resolve www.landing.com:18443:127.0.0.1 https://www.landing.com:18443/about")
if [[ "$REDIRECT_OUT" == *"301 https://landing.com/about"* ]]; then
    echo "PASS ($REDIRECT_OUT)"
else
    echo "FAIL: Expected 301 redirect to https://landing.com/about, got: $REDIRECT_OUT"
    exit 1
fi

# E. Basic Auth
echo -n "5a. Basic Auth without credentials: "
AUTH_NO_CRED=$(ssh dermaga-vm "curl -k -s -o /dev/null -w '%{http_code}' --resolve portal.example.com:18443:127.0.0.1 https://portal.example.com:18443/")
if [[ "$AUTH_NO_CRED" == "401" ]]; then
    echo "PASS (HTTP 401 Unauthorized)"
else
    echo "FAIL (HTTP $AUTH_NO_CRED, expected 401)"
    exit 1
fi

echo -n "5b. Basic Auth with valid credentials (admin:secret): "
AUTH_WITH_CRED=$(ssh dermaga-vm "curl -k -s -o /dev/null -w '%{http_code}' -u admin:secret --resolve portal.example.com:18443:127.0.0.1 https://portal.example.com:18443/")
if [[ "$AUTH_WITH_CRED" == "200" ]]; then
    echo "PASS (HTTP 200 OK)"
else
    echo "FAIL (HTTP $AUTH_WITH_CRED, expected 200)"
    exit 1
fi

# F. IP Allowlist
echo -n "6a. IP Allowlist rejection from non-allowed client (10.99.1.1 via host port): "
ALLOW_CODE=$(ssh dermaga-vm "curl -k -s -o /dev/null -w '%{http_code}' --resolve admin.example.com:18443:127.0.0.1 https://admin.example.com:18443/")
if [[ "$ALLOW_CODE" == "403" ]]; then
    echo "PASS (HTTP 403 Forbidden)"
else
    echo "FAIL (HTTP $ALLOW_CODE, expected 403)"
    exit 1
fi

echo -n "6b. IP Allowlist acceptance from allowed client (192.168.1.50 in 192.168.1.0/24): "
TRAEFIK_ALLOW_IP=$(ssh dermaga-vm "podman inspect $TRAEFIK_CONT --format '{{range \$net, \$conf := .NetworkSettings.Networks}}{{if eq \$net \"$NET_ALLOW\"}}{{\$conf.IPAddress}}{{end}}{{end}}'")
ALLOW_OK_CODE=$(ssh dermaga-vm "podman run --rm --network $NET_ALLOW --ip 192.168.1.50 docker.io/curlimages/curl:8.12.1@sha256:94e9e444bcba979c2ea12e27ae39bee4cd10bc7041a472c4727a558e213744e6 -k -s -o /dev/null -w '%{http_code}' --resolve admin.example.com:18443:$TRAEFIK_ALLOW_IP https://admin.example.com:18443/")
if [[ "$ALLOW_OK_CODE" == "200" ]]; then
    echo "PASS (HTTP 200 OK)"
else
    echo "FAIL (HTTP $ALLOW_OK_CODE, expected 200)"
    exit 1
fi

# G. Max Body Size Limit
echo -n "7a. Request buffering with allowed body size (1 KiB <= 10 MiB): "
BODY_OK=$(ssh dermaga-vm "curl -k -s -o /dev/null -w '%{http_code}' -X POST --data 'normal small body' --resolve upload.example.com:18443:127.0.0.1 https://upload.example.com:18443/")
if [[ "$BODY_OK" == "200" ]]; then
    echo "PASS (HTTP 200 OK)"
else
    echo "FAIL (HTTP $BODY_OK, expected 200)"
    exit 1
fi

echo -n "7b. Request buffering exceeding max request body size (11 MiB > 10 MiB limit): "
BODY_OVER=$(ssh dermaga-vm "bash -c '
OVER_FILE=\$(mktemp)
head -c 11534336 /dev/zero > \$OVER_FILE
curl -k -s -o /dev/null -w \"%{http_code}\" -X POST --data-binary @\$OVER_FILE --resolve upload.example.com:18443:127.0.0.1 https://upload.example.com:18443/
rm -f \$OVER_FILE
'")
if [[ "$BODY_OVER" == "413" ]]; then
    echo "PASS (HTTP 413 Payload Too Large)"
else
    echo "FAIL (HTTP $BODY_OVER, expected 413)"
    exit 1
fi

# H. Default Security Headers (REL-8 matching vps-setup.sh)
echo -n "8a. Default security headers verification (portfolio.example.com): "
DEF_HEADERS=$(ssh dermaga-vm "curl -k -s -I --resolve portfolio.example.com:18443:127.0.0.1 https://portfolio.example.com:18443/")
if echo "$DEF_HEADERS" | grep -qi "strict-transport-security: max-age=31536000; includeSubDomains" && \
   echo "$DEF_HEADERS" | grep -qi "x-content-type-options: nosniff" && \
   echo "$DEF_HEADERS" | grep -qi "x-frame-options: SAMEORIGIN" && \
   echo "$DEF_HEADERS" | grep -qi "referrer-policy: strict-origin-when-cross-origin" && \
   echo "$DEF_HEADERS" | grep -qi "permissions-policy: camera=(), microphone=(), geolocation=(), payment=()" && \
   ! echo "$DEF_HEADERS" | grep -qi "server:" && \
   ! echo "$DEF_HEADERS" | grep -qi "x-powered-by:"; then
    echo "PASS (all 5 default security headers present, Server and X-Powered-By stripped)"
else
    echo "FAIL: Default headers mismatch:"
    echo "$DEF_HEADERS"
    exit 1
fi

# I. Custom and Removed Headers
echo -n "8b. Custom and removed headers verification (headers.example.com): "
CUST_HEADERS=$(ssh dermaga-vm "curl -k -s -I --resolve headers.example.com:18443:127.0.0.1 https://headers.example.com:18443/")
if echo "$CUST_HEADERS" | grep -qi "content-security-policy: default-src 'self'" && \
   echo "$CUST_HEADERS" | grep -qi "x-custom-header: custom-value-123" && \
   ! echo "$CUST_HEADERS" | grep -qi "x-frame-options:"; then
    echo "PASS (CSP and custom header added, X-Frame-Options successfully removed)"
else
    echo "FAIL: Custom/removed headers mismatch:"
    echo "$CUST_HEADERS"
    exit 1
fi

# J. Switch Verify Router Behavior
echo -n "9a. Standard request routes to production release 1: "
VERIFY_STD=$(ssh dermaga-vm "curl -k -s --resolve webapp.example.com:18443:127.0.0.1 https://webapp.example.com:18443/")
if echo "$VERIFY_STD" | grep -q "release 1"; then
    echo "PASS (production release 1 served)"
else
    echo "FAIL (expected release 1, got: $VERIFY_STD)"
    exit 1
fi

echo -n "9b. Request with X-Dermaga-Verify token routes to candidate release 2: "
VERIFY_TOKEN=$(ssh dermaga-vm "curl -k -s -H 'X-Dermaga-Verify: test-switch-verify-token-999' --resolve webapp.example.com:18443:127.0.0.1 https://webapp.example.com:18443/")
if echo "$VERIFY_TOKEN" | grep -q "candidate release 2"; then
    echo "PASS (candidate release 2 served)"
else
    echo "FAIL (expected candidate release 2, got: $VERIFY_TOKEN)"
    exit 1
fi

# 11. Negative Controls: Missing Middleware and Missing Service Detection
echo
echo "--- Negative Controls: Missing Middleware and Missing Service Detection ---"

# Negative Case 1: Router referencing missing middleware with valid service
echo -n "Negative control 1 (missing middleware detection): "
ssh dermaga-vm "cat > $REMOTE_TMP/dynamic/broken-middleware.yml" << 'EOF'
# managed by dermaga; app=broken-middleware
{
  "http": {
    "routers": {
      "app-broken-middleware": {
        "entryPoints": ["websecure"],
        "rule": "Host(`broken-mid.example.com`)",
        "service": "app-portfolio",
        "middlewares": ["nonexistent-middleware"],
        "tls": {"certResolver": "le"}
      }
    }
  }
}
EOF

ERR1=$(ssh dermaga-vm "python3 $REMOTE_TMP/check_router_error.py app-broken-middleware nonexistent-middleware")
if [[ $? -eq 0 && -n "$ERR1" ]]; then
    echo "PASS (router disabled, error: $ERR1)"
else
    echo "FAIL: $ERR1"
    exit 1
fi

# Negative Case 2: Router referencing missing service with valid middleware
echo -n "Negative control 2 (missing service detection): "
ssh dermaga-vm "cat > $REMOTE_TMP/dynamic/broken-service.yml" << 'EOF'
# managed by dermaga; app=broken-service
{
  "http": {
    "routers": {
      "app-broken-service": {
        "entryPoints": ["websecure"],
        "rule": "Host(`broken-svc.example.com`)",
        "service": "nonexistent-service",
        "middlewares": ["app-portfolio-headers"],
        "tls": {"certResolver": "le"}
      }
    }
  }
}
EOF

ERR2=$(ssh dermaga-vm "python3 $REMOTE_TMP/check_router_error.py app-broken-service nonexistent-service")
if [[ $? -eq 0 && -n "$ERR2" ]]; then
    echo "PASS (router disabled, error: $ERR2)"
else
    echo "FAIL: $ERR2"
    exit 1
fi

echo
echo "=== All Traefik Golden Configurations Verified: PASS ==="
