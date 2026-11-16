#!/usr/bin/env bash
set -euo pipefail

echo "=== SPIKE 3.7: Binary Size & Idle RSS (modernc.org/sqlite + Embedded SvelteKit Assets) ==="
echo "Date: $(date -Is)"
echo "Host: $(hostname)"
echo "User: $(whoami)"

BIN_PATH="${HOME}/spikes/app_size_bin"
DB_PATH="${HOME}/spikes/spike_test.db"

cleanup() {
  echo "--- Cleanup ---"
  if [ -n "${APP_PID:-}" ]; then
    kill -TERM "${APP_PID}" 2>/dev/null || true
    wait "${APP_PID}" 2>/dev/null || true
  fi
  rm -f "${DB_PATH}" "${BIN_PATH}"
  echo "Spike 3.7 cleaned up."
}
trap cleanup EXIT

echo ""
echo "--- 1. Binary Size Analysis ---"
ls -lh "${BIN_PATH}"
BIN_BYTES=$(stat -c%s "${BIN_PATH}")
echo "Binary size in bytes: ${BIN_BYTES} ($(( BIN_BYTES / 1024 / 1024 )) MB)"

echo ""
echo "--- 2. Starting Binary on dermaga-vm ---"
cd "${HOME}/spikes"
"${BIN_PATH}" &
APP_PID=$!
sleep 2

if ! kill -0 "${APP_PID}" 2>/dev/null; then
  echo "FAIL: Application failed to start!"
  exit 1
fi

echo "App running with PID ${APP_PID}"

echo ""
echo "--- 3. Measuring Initial Idle RSS ---"
ps -o pid,rss,vsz,comm -p "${APP_PID}"
INIT_RSS=$(awk '/VmRSS/{print $2}' "/proc/${APP_PID}/status")
echo "Initial VmRSS: ${INIT_RSS} kB ($(( INIT_RSS / 1024 )) MB)"

echo ""
echo "--- 4. Exercising SQLite and Embedded Static Asset Endpoints ---"
echo "Fetching static / (index.html):"
curl -s --max-time 5 http://127.0.0.1:8088/ | head -n 3
echo ""
echo "Fetching SQLite /api/apps:"
curl -s --max-time 5 http://127.0.0.1:8088/api/apps

echo "Sending 100 requests to exercise runtime..."
for i in {1..100}; do
  curl -s --max-time 2 http://127.0.0.1:8088/api/apps >/dev/null
  curl -s --max-time 2 http://127.0.0.1:8088/bundle.js >/dev/null
done
echo "Requests completed."

echo ""
echo "--- 5. Measuring RSS Under/After Load ---"
sleep 2
ps -o pid,rss,vsz,comm -p "${APP_PID}"
POST_RSS=$(awk '/VmRSS/{print $2}' "/proc/${APP_PID}/status")
PEAK_RSS=$(awk '/VmHWM/{print $2}' "/proc/${APP_PID}/status")
echo "Post-load VmRSS: ${POST_RSS} kB ($(( POST_RSS / 1024 )) MB)"
echo "Peak VmHWM: ${PEAK_RSS} kB ($(( PEAK_RSS / 1024 )) MB)"

echo ""
echo "--- 6. Verifying NFR-2 Target (< 30 MB) ---"
if [ "${POST_RSS}" -lt 30720 ]; then
  echo "PASS: Post-load RSS (${POST_RSS} kB / $(( POST_RSS / 1024 )) MB) is well under 30 MB target (NFR-2)"
else
  echo "FAIL: Post-load RSS (${POST_RSS} kB) exceeds 30 MB target!"
  exit 1
fi

echo "=== SPIKE 3.7 SUCCESS ==="
