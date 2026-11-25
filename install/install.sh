#!/usr/bin/env bash
# dermaga installer: turns a fresh Debian 13 server into a dermaga host.
#
#   curl -fsSL https://github.com/r3p-dev/dermaga/releases/download/<tag>/install.sh | sudo bash
#
# What it does and why is specified in docs/PRD/install-and-access.md (INST, HOST, LOCK, LAP).
# Re-running it is safe: every file is compared before it is written, and services are only
# reloaded when something they read has changed.

set -Eeuo pipefail
shopt -s nullglob
umask 022

# The release workflow replaces this placeholder with the release tag.
readonly DERMAGA_VERSION="__DERMAGA_VERSION__"
readonly RELEASE_URL="https://github.com/r3p-dev/dermaga/releases/download/$DERMAGA_VERSION"

# Tests only: ROOT prefixes every system path, TTY replaces the terminal.
readonly ROOT="${DERMAGA_TEST_ROOT:-}"
readonly TTY="${DERMAGA_TEST_TTY:-/dev/tty}"

readonly SERVER_USER=server
readonly DEFAULT_SSH_PORT=4222
readonly CONFIRM_TIMEOUT_SECONDS=600
readonly SSHD_DROPIN="$ROOT/etc/ssh/sshd_config.d/00-dermaga.conf"
readonly NFT_CONF="$ROOT/etc/nftables.conf"
readonly F2B_JAIL="$ROOT/etc/fail2ban/jail.d/90-dermaga.local"
readonly SUDOERS_FILE="$ROOT/etc/sudoers.d/90-dermaga-server"

ASSUME_YES=no
CONFIG_FILE=""
CHANGES=0
WROTE_CHANGED=0
LOCKOUT_PHASE=none # none → open (old + new access) → closed (new access only)

# Answers, from the terminal or from --config (INST-4).
SSH_PUBLIC_KEY=""
SSH_PORT=""
PUBLIC_ADDRESS=""
SUDO_PASSWORDLESS=""
LOGIN_KEY_FP=""
DERMAGA_UPDATED=0
declare -a OLD_SSH_PORTS=()

# --- output ------------------------------------------------------------------
if [[ -t 2 ]]; then
  C_BOLD=$'\e[1m' C_RED=$'\e[31m' C_GREEN=$'\e[32m' C_YELLOW=$'\e[33m' C_CYAN=$'\e[36m' C_RESET=$'\e[0m'
else
  C_BOLD="" C_RED="" C_GREEN="" C_YELLOW="" C_CYAN="" C_RESET=""
fi
step() { printf '\n%s==>%s %s%s%s\n' "$C_CYAN" "$C_RESET" "$C_BOLD" "$*" "$C_RESET" >&2; }
info() { printf '    %s\n' "$*" >&2; }
ok() { printf '  %s✓%s %s\n' "$C_GREEN" "$C_RESET" "$*" >&2; }
warn() { printf '  %s!%s %s\n' "$C_YELLOW" "$C_RESET" "$*" >&2; }
die() {
  printf '%serror:%s %s\n' "$C_RED" "$C_RESET" "$*" >&2
  exit 1
}

# LOCK-5: whatever stops the installer, the way the user logged in keeps working until the new
# login has been proven; say so, and that running it again resumes.
on_exit() {
  local code=$?
  if ((code != 0)) && [[ $LOCKOUT_PHASE != closed ]]; then
    printf '\n%sThe installer stopped.%s Your current way of logging in still works.\n' "$C_BOLD" "$C_RESET" >&2
    printf 'Run the same command again to continue; it is safe to re-run.\n' >&2
  fi
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 129' HUP
trap 'exit 143' TERM

# --- prompts (INST-3) ----------------------------------------------------------
# The script itself arrives on stdin through the pipe, so answers come from the terminal (fd 3).
open_terminal() {
  if [[ $ASSUME_YES == yes ]]; then return 0; fi
  if ! { exec 3<"$TTY"; } 2>/dev/null; then
    die "no terminal to ask questions on. Run the install command in an interactive SSH session."
  fi
}

# ask VAR "question" default: shows the default; Enter accepts it.
ask() {
  local -n _answer=$1
  local _question=$2 _default=${3-} _reply=""
  if [[ $ASSUME_YES == yes ]]; then
    _answer=$_default
    return 0
  fi
  printf '  %s%s: ' "$_question" "${_default:+ [$_default]}" >&2
  IFS= read -r -u 3 _reply || die "no answer from the terminal"
  _answer=${_reply:-$_default}
}

# confirm "question": yes by default.
confirm() {
  local answer=""
  ask answer "$1 [Y/n]" ""
  [[ -z $answer || ${answer,,} == y* ]]
}

# --- helpers -----------------------------------------------------------------
valid_port() { [[ ${1-} =~ ^[0-9]{1,5}$ ]] && ((10#$1 >= 1 && 10#$1 <= 65535)); }

# write_file PATH MODE [OWNER] < content: atomic replace; counts a change only when the
# content or mode differs.
write_file() {
  local path=$1 mode=$2 owner=${3:-} dir tmp
  dir=$(dirname "$path")
  mkdir -p "$dir"
  tmp=$(mktemp "$dir/.dermaga.XXXXXX")
  cat >"$tmp"
  chmod "$mode" "$tmp"
  if [[ -n $owner ]]; then chown "$owner:$owner" "$tmp"; fi
  if [[ -f $path ]] && cmp -s "$tmp" "$path" && [[ $(stat -c %a "$path") == "${mode#0}" ]]; then
    rm -f "$tmp"
    WROTE_CHANGED=0
    return 0
  fi
  mv -f "$tmp" "$path"
  WROTE_CHANGED=1
  CHANGES=$((CHANGES + 1))
  info "updated ${path#"$ROOT"}"
}

server_home() { printf '%s/home/%s' "$ROOT" "$SERVER_USER"; }

# as_server CMD...: run as user server with its systemd user bus, like a login would.
as_server() {
  local uid
  uid=$(id -u "$SERVER_USER")
  runuser -u "$SERVER_USER" -- env -i \
    HOME="/home/$SERVER_USER" USER="$SERVER_USER" LOGNAME="$SERVER_USER" SHELL=/bin/bash \
    PATH="/home/$SERVER_USER/.local/bin:/usr/local/bin:/usr/bin:/bin" LANG=C.UTF-8 \
    XDG_RUNTIME_DIR="/run/user/$uid" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$uid/bus" \
    "$@"
}

# --- preflight (INST-2, INST-8) ------------------------------------------------
# The file is the target system's (under $ROOT, a temp root in tests), so shellcheck can't follow it.
# shellcheck source=/dev/null
os_field() { (. "$ROOT/etc/os-release" && printf '%s' "${!1:-}"); }

arch() {
  case $(uname -m) in
    x86_64) echo amd64 ;;
    aarch64) echo arm64 ;;
    *) die "this processor ($(uname -m)) is not supported; dermaga runs on x86_64 and arm64" ;;
  esac
}

preflight() {
  step "Checking this server"
  [[ $EUID -eq 0 || -n $ROOT ]] || die "the installer needs root. Run: curl -fsSL $RELEASE_URL/install.sh | sudo bash"
  [[ $DERMAGA_VERSION != __* ]] || die "this install.sh was not taken from a release; use the command from a release page"
  local id version
  id=$(os_field ID)
  version=$(os_field VERSION_ID)
  [[ $id == debian && $version == 13 ]] ||
    die "dermaga needs Debian 13 (trixie); this server runs ${id:-an unknown system} ${version}. Reinstall it with Debian 13 at your provider."
  arch >/dev/null
  [[ -d $ROOT/run/systemd/system ]] || die "systemd is required"
  if systemctl is-active --quiet ssh.socket; then
    # shortcut: socket-activated sshd ignores Port; Debian 13 uses ssh.service. Support it when a provider needs it.
    die "this server starts SSH through ssh.socket, which the installer does not support yet"
  fi
  local busy
  busy=$(ss -Hltnup '( sport = :80 or sport = :443 )' | grep -v '"systemd"' || true)
  if [[ -n $busy ]]; then
    printf '%s\n' "$busy" >&2
    die "another program already uses port 80 or 443 (listed above), often a web server from the provider image. Remove it, then run the installer again."
  fi
  ok "Debian 13 on $(uname -m)"
}

# --- answers (INST-4) ----------------------------------------------------------
# KEY=value lines, never sourced.
load_config() {
  local line key val
  [[ -n $CONFIG_FILE ]] || return 0
  [[ -r $CONFIG_FILE ]] || die "cannot read $CONFIG_FILE"
  while IFS= read -r line || [[ -n $line ]]; do
    [[ $line =~ ^[[:space:]]*([A-Z_]+)=\"?([^\"]*)\"?[[:space:]]*$ ]] || continue
    key=${BASH_REMATCH[1]} val=${BASH_REMATCH[2]}
    case $key in
      SSH_PUBLIC_KEY | SSH_PORT | PUBLIC_ADDRESS | SUDO_PASSWORDLESS) printf -v "$key" '%s' "$val" ;;
      *) warn "$CONFIG_FILE: unknown key $key (ignored)" ;;
    esac
  done <"$CONFIG_FILE"
}

# --- the login key (INST-5) ------------------------------------------------------
readonly KEY_RE='(sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com|ssh-ed25519|ecdsa-sha2-nistp(256|384|521)|ssh-rsa)[[:space:]]+[A-Za-z0-9+/]+=*([[:space:]][^"]*)?$'

# key_info KEY: prints "fingerprint|type|comment" for a usable public key, nothing otherwise.
key_info() {
  local tmp out bits fp type comment
  tmp=$(mktemp)
  printf '%s\n' "$1" >"$tmp"
  out=$(ssh-keygen -lf "$tmp" 2>/dev/null) || {
    rm -f "$tmp"
    return 1
  }
  rm -f "$tmp"
  read -r bits fp comment <<<"$out"
  type=${comment##*(}
  type=${type%)}
  comment=${comment% (*}
  if [[ $type == RSA ]] && ((bits < 2048)); then return 1; fi
  printf '%s|%s|%s\n' "$fp" "$type" "$comment"
}

# authorized_keys_files: where the keys of the current login can be found.
authorized_keys_files() {
  printf '%s\n' "$ROOT/root/.ssh/authorized_keys"
  if [[ -n ${SUDO_USER:-} && $SUDO_USER != root ]]; then
    printf '%s/home/%s/.ssh/authorized_keys\n' "$ROOT" "$SUDO_USER"
  fi
}

# collect_keys: one "key<TAB>fingerprint|type|comment" line per distinct usable key. Options
# such as cloud-init's command="echo 'Please login as debian'" are dropped with the regex.
collect_keys() {
  local f line key info
  local -A seen=()
  while IFS= read -r f; do
    [[ -r $f ]] || continue
    while IFS= read -r line || [[ -n $line ]]; do
      [[ ${line%$'\r'} =~ $KEY_RE ]] || continue
      key=${BASH_REMATCH[0]}
      info=$(key_info "$key") || continue
      [[ -z ${seen[${info%%|*}]:-} ]] || continue
      seen[${info%%|*}]=1
      printf '%s\t%s\n' "$key" "$info"
    done <"$f"
  done < <(authorized_keys_files)
}

# ssh_connection: SSH_CONNECTION of this login. sudo drops it from the environment, so look in
# the environment of the processes this installer was started from.
ssh_connection() {
  local pid=$$ value
  if [[ -n ${SSH_CONNECTION:-} ]]; then
    printf '%s' "$SSH_CONNECTION"
    return 0
  fi
  while ((pid > 1)); do
    value=$(tr '\0' '\n' <"/proc/$pid/environ" 2>/dev/null | sed -n 's/^SSH_CONNECTION=//p' || true)
    if [[ -n $value ]]; then
      printf '%s' "$value"
      return 0
    fi
    pid=$(awk '{print $4}' "/proc/$pid/stat" 2>/dev/null || echo 1)
  done
  return 1
}

# current_login_fingerprint: the key this SSH login used, from sshd's own log line
# "Accepted publickey for USER from IP port PORT ssh2: TYPE SHA256:..." (INFO level).
current_login_fingerprint() {
  local conn ip port
  conn=$(ssh_connection) || return 1
  read -r ip port _ <<<"$conn"
  journalctl -u ssh.service --no-pager -o cat 2>/dev/null |
    grep -F " from $ip port $port ssh2: " | grep '^Accepted publickey for ' | tail -n 1 | awk '{print $NF}'
}

# use_key KEY: accepts KEY as the login key when it is a valid public key.
use_key() {
  local info
  [[ $1 =~ ^$KEY_RE ]] && info=$(key_info "$1") || return 1
  SSH_PUBLIC_KEY=$1
  LOGIN_KEY_FP=${info%%|*}
  info=${info#*|}
  ok "key ${info%%|*} $LOGIN_KEY_FP ${info#*|}"
}

# read_pasted_key: explains how to make a key on the laptop and reads one until it is valid.
read_pasted_key() {
  local reply=""
  info "Your laptop needs an SSH key. On the laptop (not here), in a terminal:"
  info "  1. Check for one:     cat ~/.ssh/id_ed25519.pub"
  info "  2. None? Create one:  ssh-keygen -t ed25519   (press Enter at each question)"
  info "  3. Show it again:     cat ~/.ssh/id_ed25519.pub"
  info "Copy the one line it prints (it starts with ssh-ed25519) and paste it below."
  info "The private file ~/.ssh/id_ed25519 never leaves the laptop. Keep a backup of it."
  while :; do
    ask reply "Public key" ""
    use_key "${reply%$'\r'}" && return 0
    [[ $ASSUME_YES == no ]] || die "no SSH key on this server and none in $CONFIG_FILE"
    warn "that is not a public key. Paste the whole line from the .pub file, or press Ctrl+C to stop."
  done
}

choose_key() {
  step "Your SSH key"
  if [[ -n $SSH_PUBLIC_KEY ]]; then
    use_key "$SSH_PUBLIC_KEY" || die "SSH_PUBLIC_KEY in $CONFIG_FILE is not a valid public key"
    return 0
  fi
  local -a keys=() infos=()
  local current="" line i default=1 choice
  while IFS=$'\t' read -r line i; do
    keys+=("$line")
    infos+=("$i")
  done < <(collect_keys)
  current=$(current_login_fingerprint || true)

  if ((${#keys[@]} == 0)); then
    info "No SSH key is set up on this server yet."
    read_pasted_key
    return 0
  fi
  for i in "${!keys[@]}"; do
    local fp=${infos[$i]%%|*} rest=${infos[$i]#*|}
    local mark=""
    if [[ $fp == "$current" ]]; then
      mark=" (you logged in with this key)"
      default=$((i + 1))
    fi
    info "$((i + 1)). ${rest%%|*} $fp ${rest#*|}$mark"
  done
  if ((${#keys[@]} == 1)); then
    if confirm "Use this key to log in as $SERVER_USER?"; then choice=1; else
      read_pasted_key
      return 0
    fi
  else
    while :; do
      ask choice "Which key logs in as $SERVER_USER? (number, or p to paste another)" "$default"
      if [[ $choice == p ]]; then
        read_pasted_key
        return 0
      fi
      [[ $choice =~ ^[0-9]+$ ]] && ((choice >= 1 && choice <= ${#keys[@]})) && break
      warn "type a number from 1 to ${#keys[@]}"
    done
  fi
  SSH_PUBLIC_KEY=${keys[$((choice - 1))]}
  LOGIN_KEY_FP=${infos[$((choice - 1))]%%|*}
}

# --- port and address (INST-6) ---------------------------------------------------
# effective_ssh_ports: the ports sshd listens on with its current configuration.
effective_ssh_ports() { sshd -T 2>/dev/null | awk '$1 == "port" {print $2}' | sort -un; }

dropin_port() { awk '$1 == "Port" {p = $2} END {print p}' "$SSHD_DROPIN" 2>/dev/null || true; }

choose_port() {
  step "SSH port"
  local default busy
  default=$(dropin_port)
  default=${default:-$DEFAULT_SSH_PORT}
  while :; do
    if [[ -z $SSH_PORT ]]; then ask SSH_PORT "SSH port" "$default"; fi
    if ! valid_port "$SSH_PORT" || ((SSH_PORT == 80 || SSH_PORT == 443)); then
      [[ $ASSUME_YES == no ]] || die "invalid SSH_PORT $SSH_PORT"
      warn "choose a number from 1 to 65535, other than 80 and 443"
      SSH_PORT=""
      continue
    fi
    busy=$(ss -Hltnp "( sport = :$SSH_PORT )" | grep -v '"sshd"' || true)
    if [[ -n $busy ]]; then
      [[ $ASSUME_YES == no ]] || die "port $SSH_PORT is used by another program: $busy"
      warn "port $SSH_PORT is used by another program: $busy"
      SSH_PORT=""
      continue
    fi
    break
  done
  mapfile -t OLD_SSH_PORTS < <(effective_ssh_ports)
  ((${#OLD_SSH_PORTS[@]})) || die "could not read sshd's current configuration (sshd -T failed)"
  ok "SSH on port $SSH_PORT"
}

# is_public_ipv4: false for private (RFC 1918), shared (100.64.0.0/10), loopback, link-local
# and reserved addresses.
is_public_ipv4() {
  local a b
  [[ ${1-} =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$ ]] || return 1
  a=$((10#${BASH_REMATCH[1]})) b=$((10#${BASH_REMATCH[2]}))
  ((a <= 255 && b <= 255 && 10#${BASH_REMATCH[3]} <= 255 && 10#${BASH_REMATCH[4]} <= 255)) || return 1
  ((a == 0 || a == 10 || a == 127 || a >= 224)) && return 1
  ((a == 100 && b >= 64 && b <= 127)) && return 1
  ((a == 169 && b == 254)) && return 1
  ((a == 172 && b >= 16 && b <= 31)) && return 1
  ((a == 192 && b == 168)) && return 1
  return 0
}

# detect_addresses: global IPv4 and IPv6 of the default-route interface, without asking any
# outside service (NFR-8). Prints "v4 v6" (either may be "-").
detect_addresses() {
  local dev v4 v6
  dev=$(ip -o route show default 2>/dev/null | awk '{for (i = 1; i < NF; i++) if ($i == "dev") {print $(i + 1); exit}}')
  [[ -n $dev ]] || {
    echo "- -"
    return 0
  }
  v4=$(ip -o -4 addr show dev "$dev" scope global 2>/dev/null | awk '{split($4, a, "/"); print a[1]; exit}')
  v6=$(ip -o -6 addr show dev "$dev" scope global 2>/dev/null | awk '{split($4, a, "/"); print a[1]; exit}')
  echo "${v4:--} ${v6:--}"
}

choose_address() {
  step "Server address"
  local v4 v6
  read -r v4 v6 < <(detect_addresses)
  if [[ -n $PUBLIC_ADDRESS ]]; then
    :
  elif is_public_ipv4 "$v4"; then
    PUBLIC_ADDRESS=$v4
  else
    if [[ $v4 == - ]]; then
      info "This server has no IPv4 address on its main network interface${v6:+ (IPv6: $v6)}."
    else
      info "This server's own address, $v4, is private: the internet reaches it through your"
      info "provider's network. Your provider's control panel shows the public IP address."
    fi
    local default=$v4
    [[ $default != - ]] || default=$v6
    [[ $default != - ]] || default=""
    while :; do
      ask PUBLIC_ADDRESS "Public IP address of this server" "$default"
      [[ $PUBLIC_ADDRESS =~ ^[0-9A-Za-z.:-]+$ ]] && break
      [[ $ASSUME_YES == no ]] || die "no public address detected; set PUBLIC_ADDRESS in $CONFIG_FILE"
      warn "type an address like 203.0.113.10"
    done
  fi
  ok "address $PUBLIC_ADDRESS (stored in the panel's settings; you can change it there)"
}

# --- sudo (HOST-7) -----------------------------------------------------------------
choose_sudo() {
  step "sudo for $SERVER_USER"
  if [[ -n $SUDO_PASSWORDLESS ]]; then return 0; fi
  info "$SERVER_USER can become root with sudo. A sudo password is recommended: if your SSH"
  info "key is ever stolen, the thief still cannot become root without it. SSH stays key-only."
  if confirm "Protect sudo with a password (recommended)?"; then SUDO_PASSWORDLESS=no; else SUDO_PASSWORDLESS=yes; fi
}

# --- plan (INST-7) -------------------------------------------------------------------
access_is_final() { [[ -f $SSHD_DROPIN ]] && [[ $(render_sshd_final "$SSH_PORT") == "$(cat "$SSHD_DROPIN")" ]]; }

show_plan() {
  step "Plan"
  info "user         $SERVER_USER: the only SSH login, owns every container"
  info "ssh          port $SSH_PORT, key ${LOGIN_KEY_FP:-?} only; passwords and root login off"
  info "             (the current login keeps working until the new one is proven)"
  info "firewall     nftables: only SSH, 80/tcp, 443/tcp and 443/udp open"
  info "protection   fail2ban, kernel hardening, automatic security updates, zram swap"
  info "software     Podman, Traefik, git, curl; dermaga $DERMAGA_VERSION (checksum verified)"
  info "panel        on 127.0.0.1:7070 only, reached through SSH from your laptop"
  if [[ $SUDO_PASSWORDLESS == yes ]]; then
    warn "sudo without a password for $SERVER_USER (not recommended)"
  else
    info "sudo         asks for $SERVER_USER's password"
  fi
  confirm "Continue?" || die "stopped; nothing was changed"
}

# --- packages (INST-9) -----------------------------------------------------------------
readonly PACKAGES=(
  ca-certificates curl git sudo iproute2 dbus-user-session uidmap
  openssh-server nftables fail2ban python3-systemd unattended-upgrades apt-listchanges needrestart
  systemd-zram-generator podman passt crun netavark aardvark-dns catatonit
)

install_packages() {
  step "Packages"
  local -a missing=() want=("${PACKAGES[@]}") p
  if ! dpkg -s chrony >/dev/null 2>&1; then want+=(systemd-timesyncd); fi
  for p in "${want[@]}"; do
    dpkg -s "$p" >/dev/null 2>&1 || missing+=("$p")
  done
  if ((${#missing[@]} == 0)); then
    ok "all installed"
    return 0
  fi
  export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a
  local -a apt=(apt-get -y -q -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold)
  "${apt[@]}" update
  "${apt[@]}" install --no-install-recommends "${missing[@]}"
  CHANGES=$((CHANGES + 1))
  ok "installed ${missing[*]}"
}

# --- firewall (HOST-3) -------------------------------------------------------------------
# unique_ports PORT...: each port once, in numeric order.
unique_ports() { printf '%s\n' "$@" | sort -un | paste -sd' '; }

# render_nftables PORT...: only dermaga's own table is replaced, so fail2ban's table survives.
render_nftables() {
  local ports
  ports=$(unique_ports "$@")
  ports=${ports// /, }
  cat <<EOF
#!/usr/sbin/nft -f
# managed by dermaga installer
# Only this table is (re)created, so fail2ban's own table survives reloads.
# Rootless Podman never touches the host firewall: nothing bypasses these rules.

table inet dermaga
delete table inet dermaga

table inet dermaga {
  chain input {
    type filter hook input priority filter; policy drop;

    ct state established,related accept
    ct state invalid drop
    iif "lo" accept

    tcp flags & (fin|syn|rst|psh|ack|urg) == 0 drop comment "drop null packets"
    tcp flags & (fin|syn|rst|psh|ack|urg) == (fin|syn|rst|psh|ack|urg) drop comment "drop xmas packets"
    tcp flags & (syn|rst) == (syn|rst) drop comment "drop syn-rst packets"

    icmp type { destination-unreachable, time-exceeded, parameter-problem } accept
    icmp type echo-request limit rate 5/second burst 10 packets accept
    icmpv6 type { destination-unreachable, packet-too-big, time-exceeded, parameter-problem, nd-router-advert, nd-neighbor-solicit, nd-neighbor-advert, mld-listener-query } accept
    icmpv6 type echo-request limit rate 5/second burst 10 packets accept
    ip6 saddr fe80::/10 udp dport 546 accept

    tcp dport { $ports } accept comment "ssh"
    tcp dport { 80, 443 } accept comment "http, https"
    udp dport 443 accept comment "http/3"
  }

  chain forward {
    type filter hook forward priority filter; policy drop;
  }

  chain output {
    type filter hook output priority filter; policy accept;
  }
}
EOF
}

# apply_firewall PORT...: validated before loading; never `restart`, whose stop action flushes
# every table and would drop the current session for a moment.
apply_firewall() {
  render_nftables "$@" | write_file "$NFT_CONF" 0755
  nft -c -f "$NFT_CONF" || die "the generated firewall rules failed validation"
  systemctl enable --quiet nftables.service
  if ! systemctl is-active --quiet nftables.service; then
    systemctl start nftables.service
  elif ((WROTE_CHANGED)); then
    systemctl reload nftables.service
  fi
  ok "firewall: open ssh $(unique_ports "$@"), 80/tcp, 443/tcp, 443/udp"
}

# --- base system (HOST-5) ------------------------------------------------------------------
setup_base_system() {
  step "System hardening"
  write_file "$ROOT/etc/needrestart/conf.d/90-dermaga.conf" 0644 <<'EOF'
# managed by dermaga installer: restart services after library upgrades without asking
$nrconf{restart} = 'a';
EOF
  write_file "$ROOT/etc/systemd/journald.conf.d/90-dermaga.conf" 0644 <<'EOF'
# managed by dermaga installer: container logs land here too
[Journal]
Storage=persistent
Compress=yes
SystemMaxUse=500M
SystemKeepFree=1G
MaxRetentionSec=1month
EOF
  if ((WROTE_CHANGED)); then
    mkdir -p "$ROOT/var/log/journal"
    systemctl restart systemd-journald
  fi
  write_file "$ROOT/etc/systemd/coredump.conf.d/90-dermaga.conf" 0644 <<'EOF'
# managed by dermaga installer: core dumps can leak secrets from memory
[Coredump]
Storage=none
ProcessSizeMax=0
EOF

  printf 'tcp_bbr\n' | write_file "$ROOT/etc/modules-load.d/90-dermaga-bbr.conf" 0644
  modprobe tcp_bbr 2>/dev/null || warn "tcp_bbr unavailable; keeping the default congestion control"
  write_file "$ROOT/etc/modprobe.d/90-dermaga-blacklist.conf" 0644 <<'EOF'
# managed by dermaga installer: unused protocols with a history of local exploits
install dccp /bin/true
install sctp /bin/true
install rds /bin/true
install tipc /bin/true
EOF
  # A leading "-" makes systemd-sysctl skip keys this kernel doesn't have.
  write_file "$ROOT/etc/sysctl.d/90-dermaga.conf" 0644 <<'EOF'
# managed by dermaga installer

# --- network hardening
net.ipv4.conf.all.rp_filter = 1
net.ipv4.conf.default.rp_filter = 1
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.default.accept_redirects = 0
net.ipv4.conf.all.secure_redirects = 0
net.ipv4.conf.default.secure_redirects = 0
net.ipv6.conf.all.accept_redirects = 0
net.ipv6.conf.default.accept_redirects = 0
net.ipv4.conf.all.send_redirects = 0
net.ipv4.conf.default.send_redirects = 0
net.ipv4.conf.all.accept_source_route = 0
net.ipv4.conf.default.accept_source_route = 0
net.ipv6.conf.all.accept_source_route = 0
net.ipv4.conf.all.log_martians = 1
net.ipv4.icmp_echo_ignore_broadcasts = 1
net.ipv4.icmp_ignore_bogus_error_responses = 1
net.ipv4.tcp_syncookies = 1
net.ipv4.tcp_rfc1337 = 1

# --- kernel hardening
kernel.randomize_va_space = 2
kernel.sysrq = 0
kernel.kptr_restrict = 2
kernel.dmesg_restrict = 1
kernel.unprivileged_bpf_disabled = 1
net.core.bpf_jit_harden = 2
-kernel.perf_event_paranoid = 3
-kernel.yama.ptrace_scope = 1
kernel.kexec_load_disabled = 1
-vm.unprivileged_userfaultfd = 0
-dev.tty.tiocsti = 0
fs.protected_fifos = 2
fs.protected_regular = 2
fs.protected_symlinks = 1
fs.protected_hardlinks = 1
fs.suid_dumpable = 0

# --- rootless containers
-kernel.unprivileged_userns_clone = 1
# lets the systemd user manager own ports 80/443 for Traefik (socket activation)
net.ipv4.ip_unprivileged_port_start = 80
fs.inotify.max_user_watches = 524288
fs.inotify.max_user_instances = 1024

# --- performance
net.core.default_qdisc = fq
-net.ipv4.tcp_congestion_control = bbr
net.core.somaxconn = 4096
net.ipv4.tcp_fastopen = 3
net.ipv4.tcp_mtu_probing = 1
net.ipv4.tcp_slow_start_after_idle = 0
vm.vfs_cache_pressure = 50
EOF
  if ((WROTE_CHANGED)); then systemctl restart systemd-sysctl.service; fi

  write_file "$ROOT/etc/systemd/zram-generator.conf" 0644 <<'EOF'
# managed by dermaga installer: compressed swap in RAM
[zram0]
zram-size = min(ram / 2, 8192)
compression-algorithm = zstd
swap-priority = 100
EOF
  if ((WROTE_CHANGED)); then
    systemctl daemon-reload
    systemctl restart systemd-zram-setup@zram0.service || warn "zram swap did not start"
  fi
  printf 'vm.swappiness = 100\nvm.page-cluster = 0\n' | write_file "$ROOT/etc/sysctl.d/91-dermaga-swap.conf" 0644
  if ((WROTE_CHANGED)); then systemctl restart systemd-sysctl.service; fi

  write_file "$ROOT/etc/apt/apt.conf.d/20auto-upgrades" 0644 <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
EOF
  write_file "$ROOT/etc/apt/apt.conf.d/52dermaga-unattended-upgrades" 0644 <<'EOF'
// managed by dermaga installer
Unattended-Upgrade::AutoFixInterruptedDpkg "true";
Unattended-Upgrade::MinimalSteps "true";
Unattended-Upgrade::Remove-Unused-Kernel-Packages "true";
Unattended-Upgrade::Remove-New-Unused-Dependencies "true";
Unattended-Upgrade::Remove-Unused-Dependencies "true";
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-WithUsers "true";
Unattended-Upgrade::Automatic-Reboot-Time "04:00";
Unattended-Upgrade::SyslogEnable "true";
EOF
  systemctl enable --quiet --now unattended-upgrades.service apt-daily.timer apt-daily-upgrade.timer

  timedatectl set-ntp true || warn "could not enable clock synchronisation"

  # Debian cloud images never pull in network-online.target, which stalls anything waiting for
  # it at boot for 90 s (DEC-06, M0 spike 3.5b).
  if [[ ! -e $ROOT/etc/systemd/system/multi-user.target.wants/network-online.target ]]; then
    systemctl add-wants multi-user.target network-online.target
    CHANGES=$((CHANGES + 1))
  fi
  ok "sysctl, zram, journald, automatic security updates, clock sync, network-online"
}

# --- the user (HOST-1, HOST-6, HOST-7) -------------------------------------------------------
ensure_subids() {
  local f start
  for f in "$ROOT/etc/subuid" "$ROOT/etc/subgid"; do
    touch "$f"
    if grep -q "^$SERVER_USER:" "$f"; then continue; fi
    start=$(awk -F: 'BEGIN { m = 100000 } { e = $2 + $3; if (e > m) m = e } END { print m }' "$f")
    printf '%s:%s:65536\n' "$SERVER_USER" "$start" >>"$f"
    CHANGES=$((CHANGES + 1))
  done
}

setup_sudo() {
  if [[ $SUDO_PASSWORDLESS == yes ]]; then
    printf '%s ALL=(ALL:ALL) NOPASSWD: ALL\n' "$SERVER_USER" | write_file "$SUDOERS_FILE" 0440
    visudo -cqf "$SUDOERS_FILE" || {
      rm -f "$SUDOERS_FILE"
      die "the generated sudoers file was invalid"
    }
    return 0
  fi
  if [[ -f $SUDOERS_FILE ]]; then
    rm -f "$SUDOERS_FILE"
    CHANGES=$((CHANGES + 1))
  fi
  if [[ $(passwd -S "$SERVER_USER" | awk '{print $2}') != P ]]; then
    [[ $ASSUME_YES == no ]] || die "a sudo password needs a terminal; set SUDO_PASSWORDLESS=yes in $CONFIG_FILE"
    info "Choose a password for $SERVER_USER. It is only for sudo; SSH stays key-only."
    until passwd "$SERVER_USER" <&3; do warn "the passwords did not match, try again"; done
    CHANGES=$((CHANGES + 1))
  fi
}

setup_user() {
  step "User $SERVER_USER"
  local home uid
  home=$(server_home)
  # cgroup v2 delegation, so rootless containers can enforce cpu, io, memory and pids limits
  write_file "$ROOT/etc/systemd/system/user@.service.d/90-dermaga-delegate.conf" 0644 <<'EOF'
# managed by dermaga installer
[Service]
Delegate=cpu cpuset io memory pids
EOF
  if ((WROTE_CHANGED)); then systemctl daemon-reload; fi

  if ! id "$SERVER_USER" >/dev/null 2>&1; then
    useradd --create-home --home-dir "/home/$SERVER_USER" --shell /bin/bash "$SERVER_USER"
    CHANGES=$((CHANGES + 1))
  fi
  chmod 0700 "$home"
  usermod -aG sudo,adm,systemd-journal "$SERVER_USER"
  ensure_subids

  mkdir -p "$home/.ssh"
  chmod 0700 "$home/.ssh"
  chown "$SERVER_USER:$SERVER_USER" "$home/.ssh"
  {
    if [[ -f $home/.ssh/authorized_keys ]]; then cat "$home/.ssh/authorized_keys"; fi
    printf '%s\n' "$SSH_PUBLIC_KEY"
  } | awk 'NF && !seen[$0]++' | write_file "$home/.ssh/authorized_keys" 0600 "$SERVER_USER"
  setup_sudo

  local d
  for d in .config .config/containers .config/containers/systemd .config/systemd .config/systemd/user \
    .config/dermaga .local .local/bin .local/share; do
    mkdir -p "$home/$d"
    chmod 0700 "$home/$d"
    chown "$SERVER_USER:$SERVER_USER" "$home/$d"
  done
  write_file "$home/.config/containers/containers.conf" 0644 "$SERVER_USER" <<'EOF'
# managed by dermaga installer
[containers]
# stdout/stderr to journald: rotated and size-capped
log_driver = "journald"

[engine]
events_logger = "journald"

[network]
default_rootless_network_cmd = "pasta"
EOF

  # linger: the user's services (Traefik, dermaga, apps) start at boot without a login
  if [[ ! -e $ROOT/var/lib/systemd/linger/$SERVER_USER ]]; then
    loginctl enable-linger "$SERVER_USER"
    CHANGES=$((CHANGES + 1))
  fi
  uid=$(id -u "$SERVER_USER")
  systemctl start "user@$uid.service"
  local i
  for ((i = 0; i < 30; i++)); do
    [[ -S $ROOT/run/user/$uid/bus ]] && break
    sleep 1
  done
  [[ -S $ROOT/run/user/$uid/bus ]] || die "the systemd user manager for $SERVER_USER did not start"
  ok "key, sudo, subuids, lingering, cgroup delegation"
}

# --- fail2ban (HOST-4) ------------------------------------------------------------------------
setup_fail2ban() {
  local ports
  ports=$(unique_ports "$@")
  ports=${ports// /,}
  write_file "$ROOT/etc/fail2ban/fail2ban.d/90-dermaga.local" 0644 <<'EOF'
# managed by dermaga installer: recidive reads fail2ban's own log, so it must be a file
[Definition]
logtarget = /var/log/fail2ban.log
EOF
  local changed=$WROTE_CHANGED
  write_file "$F2B_JAIL" 0644 <<EOF
# managed by dermaga installer
[DEFAULT]
backend = systemd
banaction = nftables-multiport
banaction_allports = nftables-allports
bantime = 1h
bantime.increment = true
bantime.maxtime = 1w
findtime = 10m
maxretry = 5
ignoreip = 127.0.0.1/8 ::1

[sshd]
enabled = true
port = $ports
mode = aggressive

# repeat offenders: banned on all ports for a week
[recidive]
enabled = true
backend = auto
logpath = /var/log/fail2ban.log
banaction = %(banaction_allports)s
bantime = 1w
findtime = 1d
maxretry = 3
EOF
  changed=$((changed | WROTE_CHANGED))
  touch "$ROOT/var/log/fail2ban.log"
  chmod 0640 "$ROOT/var/log/fail2ban.log"
  fail2ban-client -t >/dev/null || die "the fail2ban configuration test failed"
  systemctl enable --quiet fail2ban.service
  if ((changed)) || ! systemctl is-active --quiet fail2ban.service; then systemctl restart fail2ban.service; fi
  ok "fail2ban: sshd on port $ports, plus recidive"
}

# --- SSH and the lockout guard (HOST-2, LOCK-1 … LOCK-5) ------------------------------------------
supported_algos() {
  local kind=$1 wanted_list=$2 have a out=()
  have=$(ssh -Q "$kind" 2>/dev/null)
  for a in $wanted_list; do
    if grep -qxF "$a" <<<"$have"; then out+=("$a"); fi
  done
  local IFS=,
  printf '%s' "${out[*]}"
}

# render_sshd_open NEW OLD...: LOCK-1. The new port accepts only server with a key; every old
# port keeps working as before. "Match all" ends the block, so lines after the Include in
# sshd_config stay global.
render_sshd_open() {
  local new=$1 p
  shift
  local -A ports=()
  printf '# managed by dermaga installer: old access stays open until the new login works\n'
  for p in "$@" "$new"; do
    [[ -z ${ports[$p]:-} ]] || continue
    ports[$p]=1
    printf 'Port %s\n' "$p"
  done
  printf 'LogLevel VERBOSE\n'
  for p in "$@"; do
    if [[ $p != "$new" ]]; then
      printf 'Match LocalPort %s\n    AllowUsers %s\n    AuthenticationMethods publickey\n    PermitRootLogin no\nMatch all\n' "$new" "$SERVER_USER"
      break
    fi
  done
}

# render_sshd_final PORT: HOST-2, the same guarantees as vps-setup.sh. 00- sorts first and sshd
# keeps the first value it reads, so this wins over cloud-init's 50-cloud-init.conf.
render_sshd_final() {
  local kex ciphers macs hostkeys pubkeys
  kex=$(supported_algos kex "mlkem768x25519-sha256 sntrup761x25519-sha512 sntrup761x25519-sha512@openssh.com curve25519-sha256 curve25519-sha256@libssh.org")
  ciphers=$(supported_algos cipher "chacha20-poly1305@openssh.com aes256-gcm@openssh.com aes128-gcm@openssh.com")
  macs=$(supported_algos mac "hmac-sha2-512-etm@openssh.com hmac-sha2-256-etm@openssh.com umac-128-etm@openssh.com")
  hostkeys=$(supported_algos key-sig "ssh-ed25519 ssh-ed25519-cert-v01@openssh.com rsa-sha2-512 rsa-sha2-256 rsa-sha2-512-cert-v01@openssh.com rsa-sha2-256-cert-v01@openssh.com")
  pubkeys=$(supported_algos key-sig "ssh-ed25519 ssh-ed25519-cert-v01@openssh.com sk-ssh-ed25519@openssh.com sk-ssh-ed25519-cert-v01@openssh.com ecdsa-sha2-nistp256 ecdsa-sha2-nistp384 ecdsa-sha2-nistp521 sk-ecdsa-sha2-nistp256@openssh.com rsa-sha2-512 rsa-sha2-256")
  cat <<EOF
# managed by dermaga installer
Port $1
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitEmptyPasswords no
PubkeyAuthentication yes
AuthenticationMethods publickey
AllowUsers $SERVER_USER
MaxAuthTries 3
MaxSessions 4
MaxStartups 10:30:60
LoginGraceTime 20
ClientAliveInterval 300
ClientAliveCountMax 2
PermitUserEnvironment no
X11Forwarding no
AllowAgentForwarding no
# ssh -L to the panel works; reverse, socket and tunnel forwarding do not
AllowTcpForwarding local
AllowStreamLocalForwarding no
GatewayPorts no
PermitTunnel no
UseDNS no
DebianBanner no
LogLevel VERBOSE
HostKey /etc/ssh/ssh_host_ed25519_key
HostKey /etc/ssh/ssh_host_rsa_key
KexAlgorithms $kex
Ciphers $ciphers
MACs $macs
HostKeyAlgorithms $hostkeys
PubkeyAcceptedAlgorithms $pubkeys
EOF
}

# sshd_listening PORT...: true when sshd listens on every given port within 10 s (sshd re-execs
# on reload, so the ports are briefly missing).
sshd_listening() {
  local p i missing
  for ((i = 0; i < 10; i++)); do
    missing=0
    for p in "$@"; do
      ss -Hltnp "( sport = :$p )" | grep -q '"sshd' || missing=1
    done
    ((missing)) || return 0
    sleep 1
  done
  return 1
}

# apply_sshd CONTENT PORT...: the lockout guard around every sshd change. The config is checked
# with sshd -t before a reload; if sshd then does not listen on exactly the expected ports, the
# previous file is put back and sshd reloaded again.
apply_sshd() {
  local content=$1 backup="" had=0 expected
  shift
  if [[ -f $SSHD_DROPIN ]]; then
    backup=$(mktemp)
    cp -p "$SSHD_DROPIN" "$backup"
    had=1
  fi
  printf '%s\n' "$content" | write_file "$SSHD_DROPIN" 0644
  if ((WROTE_CHANGED == 0)); then
    rm -f "$backup"
    return 0
  fi
  restore() {
    if ((had)); then mv -f "$backup" "$SSHD_DROPIN"; else rm -f "$SSHD_DROPIN" "$backup"; fi
  }
  if ! sshd -t; then
    restore
    die "sshd rejected the new configuration. The previous one is back and sshd was not reloaded."
  fi
  ssh-keygen -A >/dev/null
  systemctl reload ssh.service
  expected=$(unique_ports "$@")
  if ! sshd_listening "$@" || [[ $(effective_ssh_ports | paste -sd' ') != "$expected" ]]; then
    restore
    systemctl reload ssh.service
    die "sshd did not listen on port(s) $expected after the change. The previous configuration is back."
  fi
  rm -f "$backup"
}

# login_seen SINCE PORT: a successful key login as server on PORT since SINCE (epoch seconds),
# from two lines sshd logs with LogLevel VERBOSE for the same connection:
#   Connection from C port CP on S port PORT rdomain ""
#   Accepted publickey for server from C port CP ssh2: ED25519 SHA256:...
login_seen() {
  journalctl -u ssh.service --since "@$1" --no-pager -o cat 2>/dev/null |
    awk -v port="$2" -v user="$SERVER_USER" '
      $1 == "Connection" && $2 == "from" && $9 == port { conn[$3 " " $5] = 1 }
      $1 == "Accepted" && $2 == "publickey" && $4 == user && (($6 " " $8) in conn) { found = 1 }
      END { exit !found }'
}

# open_new_access: LOCK-1. Returns when the new port works next to the old ones.
open_new_access() {
  step "SSH: opening port $SSH_PORT next to your current login"
  apply_sshd "$(render_sshd_open "$SSH_PORT" "${OLD_SSH_PORTS[@]}")" "${OLD_SSH_PORTS[@]}" "$SSH_PORT"
  LOCKOUT_PHASE=open
  ok "port $SSH_PORT accepts $SERVER_USER with your key; port(s) ${OLD_SSH_PORTS[*]} work as before"
}

# wait_for_new_login: LOCK-2, LOCK-3.
wait_for_new_login() {
  local since=$1 i rc reply
  step "Prove the new login before the old one is closed"
  printf '\n  %sOpen a NEW terminal on your laptop and run:%s\n\n      ssh -p %s %s@%s\n\n' \
    "$C_BOLD" "$C_RESET" "$SSH_PORT" "$SERVER_USER" "$PUBLIC_ADDRESS" >&2
  info "Keep this window open. If it cannot connect, your provider's firewall may need port"
  info "$SSH_PORT opened in its control panel."
  info "Waiting up to $((CONFIRM_TIMEOUT_SECONDS / 60)) minutes; this continues by itself once you are logged in."
  if [[ $ASSUME_YES == no ]]; then info "If it does not notice your login, type yes and press Enter."; fi
  # Each round takes 2 s: a read that times out, or a sleep.
  for ((i = 0; i < CONFIRM_TIMEOUT_SECONDS / 2; i++)); do
    if login_seen "$since" "$SSH_PORT"; then
      ok "seen: $SERVER_USER logged in with a key on port $SSH_PORT"
      return 0
    fi
    if [[ $ASSUME_YES == no ]]; then
      rc=0
      IFS= read -r -t 2 -u 3 reply || rc=$?
      if ((rc == 0)) && [[ ${reply,,} == yes ]]; then
        warn "continuing on your word that the new login works"
        return 0
      elif ((rc == 0)); then
        info "type yes once the login in the new terminal works"
      fi
      ((rc > 128)) && continue # timed out: the 2 s have passed
    fi
    sleep 2
  done
  die "no login as $SERVER_USER on port $SSH_PORT within $((CONFIRM_TIMEOUT_SECONDS / 60)) minutes. Nothing was closed."
}

# close_old_access: LOCK-4, only after the new login was proven.
close_old_access() {
  step "SSH: closing the old way in"
  apply_sshd "$(render_sshd_final "$SSH_PORT")" "$SSH_PORT"
  local effective
  effective=$(sshd -T -C "user=$SERVER_USER,host=localhost,addr=127.0.0.1,lport=$SSH_PORT" 2>/dev/null |
    grep -E '^(passwordauthentication|permitrootlogin) ' | sort | paste -sd' ')
  [[ $effective == "passwordauthentication no permitrootlogin no" ]] ||
    die "unexpected effective sshd settings: $effective"
  LOCKOUT_PHASE=closed
  apply_firewall "$SSH_PORT"
  setup_fail2ban "$SSH_PORT"
  ok "only $SERVER_USER, only with a key, only on port $SSH_PORT"
}

# --- dermaga (INST-9 … INST-11, SEC-22) --------------------------------------------------------
install_binary() {
  step "dermaga $DERMAGA_VERSION"
  local a tmp expected got target
  a=$(arch)
  target="$(server_home)/.local/bin/dermaga"
  tmp=$(mktemp -d)
  curl -fsSL --proto '=https' --tlsv1.2 --max-time 60 -o "$tmp/checksums.txt" "$RELEASE_URL/checksums.txt" ||
    die "could not download checksums.txt for $DERMAGA_VERSION; the binary cannot be verified"
  expected=$(awk -v f="dermaga-linux-$a" '$2 == f || $2 == "*" f {print $1}' "$tmp/checksums.txt")
  [[ $expected =~ ^[0-9a-f]{64}$ ]] || die "checksums.txt has no checksum for dermaga-linux-$a; refusing to install an unverified binary"
  if [[ -f $target ]] && [[ $(sha256sum "$target" | awk '{print $1}') == "$expected" ]]; then
    rm -rf "$tmp"
    ok "already installed (sha256 $expected)"
    return 0
  fi
  curl -fsSL --proto '=https' --tlsv1.2 --max-time 300 -o "$tmp/dermaga" "$RELEASE_URL/dermaga-linux-$a" ||
    die "could not download dermaga-linux-$a"
  got=$(sha256sum "$tmp/dermaga" | awk '{print $1}')
  if [[ $got != "$expected" ]]; then
    rm -rf "$tmp"
    die "checksum mismatch for dermaga-linux-$a (expected $expected, got $got); refusing to install it"
  fi
  chmod 0755 "$tmp/dermaga"
  chown "$SERVER_USER:$SERVER_USER" "$tmp/dermaga"
  mv -f "$tmp/dermaga" "$target.new"
  mv -f "$target.new" "$target"
  rm -rf "$tmp"
  DERMAGA_UPDATED=1
  CHANGES=$((CHANGES + 1))
  ok "installed $target (sha256 $got verified)"
}

start_dermaga() {
  step "Starting dermaga"
  local unit i
  as_server dermaga install-settings --ssh-address "$PUBLIC_ADDRESS" --ssh-port "$SSH_PORT" \
    --login-key-fingerprint "$LOGIN_KEY_FP"
  unit="$(server_home)/.config/systemd/user/dermaga.service"
  write_file "$unit" 0644 "$SERVER_USER" <<'EOF'
# managed by dermaga installer
[Unit]
Description=dermaga daemon and panel

[Service]
ExecStart=%h/.local/bin/dermaga daemon
Restart=on-failure
RestartSec=3s

[Install]
WantedBy=default.target
EOF
  if ((WROTE_CHANGED)); then
    as_server systemctl --user daemon-reload
    DERMAGA_UPDATED=1
  fi
  as_server systemctl --user enable --quiet dermaga.service
  if ((DERMAGA_UPDATED)); then
    as_server systemctl --user restart dermaga.service
  else
    as_server systemctl --user start dermaga.service
  fi
  info "waiting for Traefik (the first start downloads its image)"
  for ((i = 0; i < 150; i++)); do
    as_server systemctl --user is-active --quiet traefik.service && break
    sleep 2
  done
  for ((i = 0; i < 30; i++)); do
    [[ $(timedatectl show -P NTPSynchronized 2>/dev/null) == yes ]] && break
    sleep 1
  done
  step "Health checks"
  if as_server dermaga doctor >&2; then ok "all checks passed"; else warn "some checks failed; see the hints above"; fi
}

print_summary() {
  step "Done"
  as_server dermaga panel >&2
  info ""
  info "The panel has no admin password yet. On the server, set one with: dermaga admin set-password"
  if [[ -n ${SUDO_USER:-} && $SUDO_USER != "$SERVER_USER" && $SUDO_USER != root ]]; then
    info "Once ssh dermaga works, the provider's account is no longer needed: sudo deluser --remove-home $SUDO_USER"
  fi
  if [[ -f $ROOT/run/reboot-required ]]; then warn "an update needs a reboot: sudo reboot (everything comes back by itself)"; fi
  if ((CHANGES == 0)); then ok "nothing changed: this server was already set up"; fi
}

parse_args() {
  while (($#)); do
    case $1 in
      --config)
        CONFIG_FILE=${2:-}
        shift 2 || die "--config needs a file"
        ;;
      --yes) ASSUME_YES=yes && shift ;;
      *) die "unknown option: $1" ;;
    esac
  done
  if [[ $ASSUME_YES == yes && -z $CONFIG_FILE ]]; then die "--yes needs --config FILE"; fi
}

main() {
  parse_args "$@"
  preflight
  open_terminal
  load_config
  choose_key
  choose_port
  choose_address
  choose_sudo
  show_plan

  local final=no
  if access_is_final; then final=yes; fi
  install_packages
  if [[ $final == yes ]]; then apply_firewall "$SSH_PORT"; else apply_firewall "${OLD_SSH_PORTS[@]}" "$SSH_PORT"; fi
  setup_base_system
  setup_user
  if [[ $final == yes ]]; then setup_fail2ban "$SSH_PORT"; else setup_fail2ban "${OLD_SSH_PORTS[@]}" "$SSH_PORT"; fi
  install_binary
  start_dermaga

  if [[ $final == yes ]]; then
    LOCKOUT_PHASE=closed
    close_old_access
  else
    local since
    since=$(date +%s)
    open_new_access
    wait_for_new_login "$since"
    close_old_access
  fi
  print_summary
}

# Piped (curl | sudo bash) and run as a file both start main; sourcing (tests) only defines.
if [[ -z ${BASH_SOURCE[0]-} || ${BASH_SOURCE[0]} == "$0" ]]; then
  main "$@"
fi
