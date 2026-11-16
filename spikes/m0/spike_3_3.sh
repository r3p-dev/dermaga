#!/usr/bin/env bash
set -euo pipefail

echo "=== SPIKE 3.3: Traefik File Provider Reload, JSON in .yml, Error Handling & Verify Router ==="
echo "Date: $(date -Is)"
echo "Host: $(hostname)"
echo "User: $(whoami)"

DYNAMIC_DIR="/tmp/spike-33-dynamic"
rm -rf "${DYNAMIC_DIR}"
mkdir -p "${DYNAMIC_DIR}"

cleanup() {
  echo "--- Cleanup & Restoring Environment ---"
  podman rm -f spike-traefik-33 spike-target-v1 spike-target-v2 2>/dev/null || true
  rm -rf "${DYNAMIC_DIR}"
  echo "Restarting vps-setup Traefik units..."
  systemctl --user start traefik-http.socket traefik-https.socket traefik.service
  systemctl --user is-active traefik.service && echo "vps-setup traefik.service restored"
}
trap cleanup EXIT

echo "--- 1. Stopping vps-setup Traefik to avoid port/resource collision ---"
systemctl --user stop traefik.service traefik-http.socket traefik-https.socket

echo "--- 2. Starting target backend containers (v1 and v2) ---"
podman rm -f spike-target-v1 spike-target-v2 2>/dev/null || true
podman run -d --name spike-target-v1 --network web --entrypoint /target_app -v "${HOME}/spikes/target_app:/target_app:ro" docker.io/alpine/socat:latest
podman run -d --name spike-target-v2 --network web --entrypoint /target_app -v "${HOME}/spikes/target_app:/target_app:ro" docker.io/alpine/socat:latest
sleep 1

echo "--- 3. Starting Traefik with file provider watching dynamic directory ---"
podman rm -f spike-traefik-33 2>/dev/null || true
podman run -d --name spike-traefik-33 \
  --network web \
  -v "${DYNAMIC_DIR}:/dynamic:ro" \
  docker.io/library/traefik:v3.7 \
  --entrypoints.web.address=:80 \
  --entrypoints.internal.address=:8081 \
  --providers.file.directory=/dynamic \
  --providers.file.watch=true \
  --log.level=WARN

sleep 2

echo "--- 4. Testing JSON in .yml and atomic rename reload latency ---"
cat << 'JSON_EOF' > "${DYNAMIC_DIR}/app.yml.tmp"
{
  "http": {
    "routers": {
      "app-router": {
        "rule": "PathPrefix(`/app`)",
        "entryPoints": ["web"],
        "service": "app-service"
      }
    },
    "services": {
      "app-service": {
        "loadBalancer": {
          "servers": [
            { "url": "http://spike-target-v1:8080" }
          ]
        }
      }
    }
  }
}
JSON_EOF

START_NS=$(date +%s%N)
mv "${DYNAMIC_DIR}/app.yml.tmp" "${DYNAMIC_DIR}/app.yml"

# Poll until Traefik responds 200 OK
RELOADED=false
for i in {1..50}; do
  STATUS=$(podman run --rm --network web docker.io/curlimages/curl:latest -s -o /dev/null -w "%{http_code}" --connect-timeout 1 --max-time 2 http://spike-traefik-33/app || echo "000")
  if [ "$STATUS" == "200" ]; then
    END_NS=$(date +%s%N)
    DURATION_MS=$(( (END_NS - START_NS) / 1000000 ))
    echo "PASS: JSON in .yml accepted. Reload latency: ${DURATION_MS} ms"
    RELOADED=true
    break
  fi
  sleep 0.05
done

if [ "$RELOADED" != true ]; then
  echo "FAIL: Traefik did not reload route from JSON .yml file"
  podman logs spike-traefik-33
  exit 1
fi

echo "--- 5. Testing 'one invalid file freezes updates' behavior ---"
# Add an invalid YAML/JSON file into the watched directory
cat << 'BAD_EOF' > "${DYNAMIC_DIR}/corrupted.yml"
http:
  routers:
    broken-router:
      rule: "Path(unclosed-quote"
      invalid_syntax_key:::
BAD_EOF

sleep 1
echo "Traefik log after invalid file added:"
podman logs --tail 5 spike-traefik-33

# Now attempt to add a new valid route (app-v2)
cat << 'V2_EOF' > "${DYNAMIC_DIR}/app-v2.yml.tmp"
{
  "http": {
    "routers": {
      "v2-router": {
        "rule": "PathPrefix(`/v2`)",
        "entryPoints": ["web"],
        "service": "v2-service"
      }
    },
    "services": {
      "v2-service": {
        "loadBalancer": {
          "servers": [
            { "url": "http://spike-target-v2:8080" }
          ]
        }
      }
    }
  }
}
V2_EOF
mv "${DYNAMIC_DIR}/app-v2.yml.tmp" "${DYNAMIC_DIR}/app-v2.yml"
sleep 2

# Check if Traefik loads the new route
STATUS_V2=$(podman run --rm --network web docker.io/curlimages/curl:latest -s -o /dev/null -w "%{http_code}" --connect-timeout 1 --max-time 2 http://spike-traefik-33/v2 || echo "000")
echo "HTTP status for new route while invalid file exists: ${STATUS_V2}"
if [ "$STATUS_V2" == "200" ]; then
  echo "BEHAVIOR: Traefik loaded valid file despite invalid sibling file"
else
  echo "BEHAVIOR CONFIRMED: Invalid file froze/blocked reloading updates! (HTTP ${STATUS_V2})"
fi

# Remove the broken file
rm -f "${DYNAMIC_DIR}/corrupted.yml"
sleep 2

# Verify that after removing the broken file, updates unfreeze
STATUS_V2_AFTER=$(podman run --rm --network web docker.io/curlimages/curl:latest -s -o /dev/null -w "%{http_code}" --connect-timeout 1 --max-time 2 http://spike-traefik-33/v2 || echo "000")
echo "HTTP status for new route after removing invalid file: ${STATUS_V2_AFTER}"
if [ "$STATUS_V2_AFTER" == "200" ]; then
  echo "PASS: Removing invalid file immediately restored dynamic configuration updates"
else
  echo "FAIL: Updates still blocked after removing invalid file"
fi

echo "--- 6. Testing Token Router on Internal Entrypoint for Switch Verification ---"
# Add a verify router bound exclusively to the 'internal' entrypoint (:8081)
cat << 'VERIFY_EOF' > "${DYNAMIC_DIR}/verify.yml.tmp"
{
  "http": {
    "routers": {
      "verify-token-router": {
        "rule": "PathPrefix(`/verify`) && Header(`X-Switch-Token`, `tok-release-candidate-99`)",
        "entryPoints": ["internal"],
        "service": "verify-service"
      }
    },
    "services": {
      "verify-service": {
        "loadBalancer": {
          "servers": [
            { "url": "http://spike-target-v2:8080" }
          ]
        }
      }
    }
  }
}
VERIFY_EOF
mv "${DYNAMIC_DIR}/verify.yml.tmp" "${DYNAMIC_DIR}/verify.yml"

echo "Testing request to internal entry point WITH correct switch token:"
VERIFY_RESP=""
for i in {1..30}; do
  VERIFY_RESP=$(podman run --rm --network web docker.io/curlimages/curl:latest -s -H "X-Switch-Token: tok-release-candidate-99" --connect-timeout 1 --max-time 2 http://spike-traefik-33:8081/verify || echo "")
  if [[ "${VERIFY_RESP}" == *"candidate-healthy"* ]]; then
    break
  fi
  sleep 0.1
done
echo "Response: ${VERIFY_RESP}"
if [[ "${VERIFY_RESP}" == *"candidate-healthy"* ]]; then
  echo "PASS: Verify router authenticated candidate release via internal entry point"
else
  echo "FAIL: Verify router failed: ${VERIFY_RESP}"
  podman logs spike-traefik-33
  exit 1
fi

echo "Testing request to internal entry point WITHOUT correct switch token (Expect 404):"
STATUS_NO_TOK=$(podman run --rm --network web docker.io/curlimages/curl:latest -s -o /dev/null -w "%{http_code}" -H "X-Switch-Token: wrong-token" --connect-timeout 2 --max-time 3 http://spike-traefik-33:8081/verify)
echo "Status with bad token: ${STATUS_NO_TOK}"
if [ "$STATUS_NO_TOK" == "404" ]; then
  echo "PASS: Verify router refused request without matching token"
else
  echo "FAIL: Expected 404 with bad token, got ${STATUS_NO_TOK}"
  exit 1
fi

echo "Atomically removing verify router..."
rm -f "${DYNAMIC_DIR}/verify.yml"

REMOVED=false
for i in {1..30}; do
  STATUS_REMOVED=$(podman run --rm --network web docker.io/curlimages/curl:latest -s -o /dev/null -w "%{http_code}" -H "X-Switch-Token: tok-release-candidate-99" --connect-timeout 1 --max-time 2 http://spike-traefik-33:8081/verify || echo "000")
  if [ "$STATUS_REMOVED" == "404" ]; then
    echo "PASS: Verify router cleanly removed after switch (HTTP 404)"
    REMOVED=true
    break
  fi
  sleep 0.1
done

if [ "$REMOVED" != true ]; then
  echo "FAIL: Verify router still responding after removal (HTTP ${STATUS_REMOVED})"
  exit 1
fi

echo "=== SPIKE 3.3 SUCCESS ==="
