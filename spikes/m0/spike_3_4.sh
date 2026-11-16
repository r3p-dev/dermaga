#!/usr/bin/env bash
set -euo pipefail

echo "=== SPIKE 3.4: Traefik ACME with Socket Activation, HTTP-01 vs TLS-ALPN-01, and HTTP/3 ==="
echo "Date: $(date -Is)"
echo "Host: $(hostname)"
echo "User: $(whoami)"

ACME_DATA_DIR="${HOME}/.local/share/containers/storage/volumes/traefik-acme/_data"
TRAEFIK_CFG_DIR="${HOME}/.config/traefik"
QUADLET_DIR="${HOME}/.config/containers/systemd"
RUN_DIR="${XDG_RUNTIME_DIR}/dermaga"

# Ensure cleanup on exit
cleanup() {
  echo "--- Cleanup & Restoring Environment ---"
  kill -9 "${SOCAT_PID:-}" 2>/dev/null || true
  podman rm -f spike-challtestsrv spike-pebble-server pebble spike-target-acme 2>/dev/null || true
  rm -f "${RUN_DIR}/pebble.sock"
  
  # Restore Quadlet
  if [ -f "${QUADLET_DIR}/traefik.container.bak" ]; then
    mv "${QUADLET_DIR}/traefik.container.bak" "${QUADLET_DIR}/traefik.container"
    systemctl --user daemon-reload
  fi
  # Restore traefik.yml
  if [ -f "${TRAEFIK_CFG_DIR}/traefik.yml.bak" ]; then
    mv "${TRAEFIK_CFG_DIR}/traefik.yml.bak" "${TRAEFIK_CFG_DIR}/traefik.yml"
  fi
  # Restore dynamic config
  rm -f "${TRAEFIK_CFG_DIR}/dynamic/acme-test.yml"
  # Restore acme.json
  if [ -f "${ACME_DATA_DIR}/acme.json.bak" ]; then
    mv "${ACME_DATA_DIR}/acme.json.bak" "${ACME_DATA_DIR}/acme.json"
  fi
  rm -f "${ACME_DATA_DIR}/pebble-ca.pem"
  
  echo "Restarting vps-setup Traefik service..."
  systemctl --user restart traefik.service
  systemctl --user is-active traefik.service && echo "vps-setup traefik.service restored"
}
trap cleanup EXIT

echo "--- 1. Backing up existing configurations ---"
cp "${QUADLET_DIR}/traefik.container" "${QUADLET_DIR}/traefik.container.bak"
cp "${TRAEFIK_CFG_DIR}/traefik.yml" "${TRAEFIK_CFG_DIR}/traefik.yml.bak"
cp "${ACME_DATA_DIR}/acme.json" "${ACME_DATA_DIR}/acme.json.bak"

echo "--- 2. Starting Pebble ACME Test Server & DNS on Host Network ---"
podman rm -f spike-challtestsrv spike-pebble-server pebble 2>/dev/null || true

# Prepare custom pebble config pointing challenge validation to socket-activated :80 and :443
cat << 'PEBBLE_JSON' > /tmp/pebble-config.json
{
  "pebble": {
    "listenAddress": "0.0.0.0:14000",
    "managementListenAddress": "0.0.0.0:15000",
    "certificate": "test/certs/localhost/cert.pem",
    "privateKey": "test/certs/localhost/key.pem",
    "httpPort": 80,
    "tlsPort": 443,
    "ocspResponderURL": "",
    "externalAccountBindingRequired": false,
    "domainBlocklist": ["blocked-domain.example"],
    "retryAfter": {
        "authz": 3,
        "order": 5
    },
    "keyAlgorithm": "ecdsa",
    "profiles": {
      "default": {
        "description": "The profile you know and love",
        "validityPeriod": 7776000
      }
    }
  }
}
PEBBLE_JSON

# challtestsrv provides mock DNS for pebble: all A queries resolve to 127.0.0.1 (disabling IPv6)
podman run -d --name spike-challtestsrv --network host ghcr.io/letsencrypt/pebble-challtestsrv:latest -dnsserver :8053 -management :8055 -defaultIPv4 127.0.0.1 -defaultIPv6 "" -http01 "" -tlsalpn01 ""
sleep 1
podman run -d --name spike-pebble-server --network host -e PEBBLE_VA_NOSLEEP=1 -v /tmp/pebble-config.json:/test/config/pebble-config.json:ro ghcr.io/letsencrypt/pebble:latest -config /test/config/pebble-config.json -dnsserver 127.0.0.1:8053
sleep 2

# Extract Pebble's minica root CA (which signed Pebble's port 14000 TLS cert) and store in ACME volume
podman cp spike-pebble-server:/test/certs/pebble.minica.pem "${ACME_DATA_DIR}/pebble-ca.pem"
chmod 644 "${ACME_DATA_DIR}/pebble-ca.pem"
ls -la "${ACME_DATA_DIR}/pebble-ca.pem"

echo "--- 3. Setting up Relay from network web to Host Pebble (Port 14000) ---"
mkdir -p "${RUN_DIR}"
rm -f "${RUN_DIR}/pebble.sock"
socat UNIX-LISTEN:"${RUN_DIR}/pebble.sock",fork,reuseaddr TCP:127.0.0.1:14000 &
SOCAT_PID=$!
sleep 1

# Name the container "pebble" on network web so Pebble's TLS certificate SAN (pebble, localhost) matches!
podman run -d --name pebble --network web -v "${RUN_DIR}:/run/dermaga:ro" docker.io/alpine/socat:latest TCP-LISTEN:14000,fork,reuseaddr UNIX-CONNECT:/run/dermaga/pebble.sock
sleep 1

# Verify connectivity from network web to Pebble
podman run --rm --network web docker.io/curlimages/curl:latest -k -s --connect-timeout 2 --max-time 3 https://pebble:14000/dir > /dev/null
echo "Relay verified: network web can reach Pebble ACME directory at https://pebble:14000/dir"

echo "--- 4. Starting Mock Backend Container ---"
podman rm -f spike-target-acme 2>/dev/null || true
podman run -d --name spike-target-acme --network web --entrypoint /target_app -v "${HOME}/spikes/target_app:/target_app:ro" docker.io/alpine/socat:latest
sleep 1

echo "--- 5. Configuring Traefik with Pebble ACME resolvers (HTTP-01 and TLS-ALPN-01) ---"
# Add LEGO_CA_CERTIFICATES to Quadlet
cat << 'CONTAINER_EOF' > "${QUADLET_DIR}/traefik.container"
# Managed by spike 3.4
[Unit]
Description=Traefik reverse proxy (socket-activated)
Requires=traefik-http.socket traefik-https.socket podman.socket
After=traefik-http.socket traefik-https.socket podman.socket

[Container]
Image=docker.io/library/traefik:v3.7
ContainerName=traefik
Network=web.network
Notify=true
AutoUpdate=registry
NoNewPrivileges=true
ReadOnly=true
DropCapability=ALL
PidsLimit=256
PodmanArgs=--memory=256m --env=LEGO_CA_CERTIFICATES=/acme/pebble-ca.pem
Environment=LEGO_CA_CERTIFICATES=/acme/pebble-ca.pem
Volume=%t/podman/podman.sock:/var/run/docker.sock
Volume=%h/.config/traefik/traefik.yml:/etc/traefik/traefik.yml:ro
Volume=%h/.config/traefik/dynamic:/etc/traefik/dynamic:ro
Volume=traefik-acme.volume:/acme

[Service]
Sockets=traefik-http.socket traefik-https.socket
Restart=always
TimeoutStartSec=300

[Install]
WantedBy=default.target
CONTAINER_EOF

# Configure Traefik static config with both resolvers
cat << 'TRAEFIK_YML' > "${TRAEFIK_CFG_DIR}/traefik.yml"
global:
  checkNewVersion: false
  sendAnonymousUsage: false

log:
  level: INFO
  format: json

entryPoints:
  web:
    address: ":80"
  websecure:
    address: ":443"
    http3: {}

providers:
  file:
    directory: /etc/traefik/dynamic
    watch: true

certificatesResolvers:
  pebble-http:
    acme:
      email: "spike@example.com"
      storage: /acme/acme.json
      caServer: "https://pebble:14000/dir"
      httpChallenge:
        entryPoint: web
  pebble-tls:
    acme:
      email: "spike@example.com"
      storage: /acme/acme.json
      caServer: "https://pebble:14000/dir"
      tlsChallenge: {}
TRAEFIK_YML

# Clear acme.json with empty json object and mode 0600
truncate -s 0 "${ACME_DATA_DIR}/acme.json"
chmod 600 "${ACME_DATA_DIR}/acme.json"

# Reload systemd user daemon and restart traefik
systemctl --user daemon-reload
systemctl --user restart traefik.service
sleep 3
systemctl --user is-active traefik.service && echo "Traefik restarted with Pebble ACME config"

echo "--- 6. Testing HTTP-01 Challenge with Socket-Activated Port 80 ---"
cat << 'DYNAMIC_HTTP' > "${TRAEFIK_CFG_DIR}/dynamic/acme-test.yml"
http:
  routers:
    http01-app:
      rule: "Host(`http01.example.com`)"
      entryPoints:
        - websecure
      tls:
        certResolver: pebble-http
      service: acme-backend
  services:
    acme-backend:
      loadBalancer:
        servers:
          - url: "http://spike-target-acme:8080"
DYNAMIC_HTTP

# Trigger certificate issuance by hitting Traefik on port 443 with Host header
echo "Triggering certificate request for http01.example.com..."
curl -k -s --connect-timeout 2 --max-time 3 https://127.0.0.1/ -H "Host: http01.example.com" > /dev/null || true

# Poll acme.json until http01 certificate appears
HTTP01_ISSUED=false
for i in {1..30}; do
  if grep -q "http01.example.com" "${ACME_DATA_DIR}/acme.json"; then
    echo "PASS: HTTP-01 certificate issued and stored in acme.json!"
    HTTP01_ISSUED=true
    break
  fi
  sleep 0.5
done

if [ "$HTTP01_ISSUED" != true ]; then
  echo "FAIL: HTTP-01 certificate was not issued within timeout"
  podman logs traefik
  podman logs spike-pebble-server
  exit 1
fi

echo "--- 7. Testing Certificate Status Reading (without Traefik API) ---"
python3 -c '
import json, sys

with open(sys.argv[1], "r") as f:
    data = json.load(f)

resolver_data = data.get("pebble-http", {})
certs = resolver_data.get("Certificates", [])
print(f"Found {len(certs)} certificate(s) in pebble-http storage:")
for c in certs:
    domain = c.get("domain", {}).get("main")
    print(f" - Domain: {domain}")
' "${ACME_DATA_DIR}/acme.json"

echo "--- 8. Testing TLS-ALPN-01 Challenge with Socket-Activated Port 443 ---"
cat << 'DYNAMIC_TLS' > "${TRAEFIK_CFG_DIR}/dynamic/acme-test.yml"
http:
  routers:
    http01-app:
      rule: "Host(`http01.example.com`)"
      entryPoints:
        - websecure
      tls:
        certResolver: pebble-http
      service: acme-backend
    tlsalpn-app:
      rule: "Host(`alpn.example.com`)"
      entryPoints:
        - websecure
      tls:
        certResolver: pebble-tls
      service: acme-backend
  services:
    acme-backend:
      loadBalancer:
        servers:
          - url: "http://spike-target-acme:8080"
DYNAMIC_TLS

echo "Triggering certificate request for alpn.example.com..."
curl -k -s --connect-timeout 2 --max-time 3 https://127.0.0.1/ -H "Host: alpn.example.com" > /dev/null || true

TLSALPN_ISSUED=false
for i in {1..30}; do
  if grep -q "alpn.example.com" "${ACME_DATA_DIR}/acme.json"; then
    echo "PASS: TLS-ALPN-01 certificate issued and stored in acme.json!"
    TLSALPN_ISSUED=true
    break
  fi
  sleep 0.5
done

if [ "$TLSALPN_ISSUED" != true ]; then
  echo "TLS-ALPN-01 certificate was NOT issued. Checking logs:"
  podman logs --tail 20 traefik
  podman logs --tail 20 spike-pebble
else
  echo "TLS-ALPN-01 Status: PASS"
fi

echo "--- 9. Testing UDP/443 Socket Activation for HTTP/3 ---"
# Test HTTP/3 request against http01.example.com now that valid certificate exists in Traefik
H3_OUTPUT=$(curl -k --http3-only -v --connect-timeout 2 --max-time 4 https://127.0.0.1/ -H "Host: http01.example.com" 2>&1) || true
echo "Raw HTTP/3 curl output:"
echo "${H3_OUTPUT}"

if echo "${H3_OUTPUT}" | grep -E -q "HTTP/3 200|candidate-healthy"; then
  echo "RESULT (HOST-6): UDP/443 socket activation for HTTP/3 WORKS!"
else
  echo "RESULT (HOST-6): UDP/443 socket activation for HTTP/3 is UNSUPPORTED / NOT FUNCTIONAL"
fi

echo "=== SPIKE 3.4 COMPLETE ==="
