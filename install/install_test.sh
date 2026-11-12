#!/usr/bin/env bash
# Checks the release checksum gate of install.sh (gap G9) with file:// URLs:
# no network, no root. Run: bash install/install_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
bin=dermaga-linux-amd64
fail=0

# Runs verify_release_checksum in a fresh shell; prints its output, returns its exit code.
verify() { # <release dir>
  local dl=$work/dl
  rm -rf "$dl" && mkdir "$dl" && cp "$work/$bin" "$dl/$bin"
  DERMAGA_INSTALL_SOURCE_ONLY=1 bash -c '
    source "$1"; verify_release_checksum "$2" "$3" "file://$4/checksums.txt"
  ' _ "$here/install.sh" "$dl" "$bin" "$1" 2>&1
}

expect() { # <name> <want: 0 or nonzero> <release dir> <text the output must contain>
  local out code
  out=$(verify "$3"); code=$?
  if { [[ $2 == 0 && $code -eq 0 ]] || [[ $2 != 0 && $code -ne 0 ]]; } && [[ $out == *"$4"* ]]; then
    printf 'PASS  %-28s exit=%d  %s\n' "$1" "$code" "$(printf '%s' "$out" | tail -n1)"
  else
    printf 'FAIL  %-28s exit=%d (want %s, output containing "%s")\n%s\n' "$1" "$code" "$2" "$4" "$out"
    fail=1
  fi
}

printf 'fake release binary\n' >"$work/$bin"
good=$(sha256sum "$work/$bin" | awk '{print $1}')
wrong=$(printf 'x' | sha256sum | awk '{print $1}')
mkdir "$work/ok" "$work/missing" "$work/noline" "$work/wrong"
printf '%s  %s\n%s  dermaga-linux-arm64\n' "$good" "$bin" "$wrong" >"$work/ok/checksums.txt"
printf '%s  dermaga-linux-arm64\n' "$good" >"$work/noline/checksums.txt"
printf '%s  %s\n' "$wrong" "$bin" >"$work/wrong/checksums.txt"

expect "good checksum"            0       "$work/ok"      "SHA-256 checksum verified ($good)"
expect "checksums.txt missing"    nonzero "$work/missing" "cannot fetch file://$work/missing/checksums.txt"
expect "no line for the binary"   nonzero "$work/noline"  "has no SHA-256 line for $bin"
expect "wrong hash"               nonzero "$work/wrong"   "checksum verification failed for $bin"
exit $fail
