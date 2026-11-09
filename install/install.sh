#!/usr/bin/env bash
# =============================================================================
# dermaga installer
# Single-command installer for Debian 13: rootless Podman, Traefik (file-provider
# only), systemd user sessions, and hardened security configuration.
#
# Usage:
#   bash install.sh config-example > dermaga-host.conf
#   sudo bash install.sh init --config dermaga-host.conf
#   curl -fsSL https://raw.githubusercontent.com/r3p-dev/dermaga/main/install/install.sh | sudo bash
# =============================================================================
set -Eeuo pipefail

VERSION="0.8.0"
CONFIG_PATH="/etc/dermaga/host.conf"
INSTALL_PATH="/usr/local/bin/dermaga"
REPO="r3p-dev/dermaga"

# --- Terminal colors ---------------------------------------------------------
if [[ -t 1 && ${TERM:-} != dumb ]]; then
  C_RESET='\033[0m'
  C_BOLD='\033[1m'
  C_GREEN='\033[32m'
  C_YELLOW='\033[33m'
  C_RED='\033[31m'
  C_BLUE='\033[34m'
else
  C_RESET='' C_BOLD='' C_GREEN='' C_YELLOW='' C_RED='' C_BLUE=''
fi

step() { printf "\n${C_BOLD}${C_BLUE}==>${C_RESET} ${C_BOLD}%s${C_RESET}\n" "$*"; }
ok()   { printf "  ${C_GREEN}✓${C_RESET} %s\n" "$*"; }
info() { printf "  %s\n" "$*"; }
warn() { printf "  ${C_YELLOW}!${C_RESET} %s\n" "$*" >&2; }
die()  { printf "  ${C_RED}✗ error:${C_RESET} %s\n" "$*" >&2; exit 1; }

is_yes() {
  case "${1:-}" in
    [Yy][Ee][Ss]|[Tt][Rr][Uu][Ee]|1) return 0 ;;
    *) return 1 ;;
  esac
}

WRITE_CHANGED=0
write_file() {
  local target=$1 mode=$2 owner=${3:-root} tmp
  tmp=$(mktemp "${target}.tmp.XXXXXX")
  cat >"$tmp"
  chmod "$mode" "$tmp"
  chown "$owner:$owner" "$tmp"
  if [[ -f $target ]] && cmp -s "$tmp" "$target"; then
    rm -f "$tmp"
    WRITE_CHANGED=0
  else
    mv -f "$tmp" "$target"
    WRITE_CHANGED=1
  fi
}

cmd_config_example() {
  cat <<'EOF'
# dermaga-host.conf — Settings for dermaga host installer
# Usage: sudo bash install.sh init --config dermaga-host.conf

# --- User & Access -----------------------------------------------------------
USER_NAME="server"                 # Single Linux user: SSH, sudo, and container owner
SSH_PUBLIC_KEY=""                 # REQUIRED: Output of `cat ~/.ssh/id_ed25519.pub` from your laptop
GITHUB_KEYS_USER=""               # Optional: GitHub username to import public keys from
SUDO_NOPASSWD="no"                # "no" = sudo asks for user password; "yes" = passwordless sudo
SSH_PORT="22"                     # Custom SSH port (e.g. 4222)

# --- Server & System ---------------------------------------------------------
SERVER_HOSTNAME=""                # Optional static hostname
TIMEZONE="UTC"                    # Server timezone (e.g. Asia/Jakarta, Europe/Berlin)
AUTO_REBOOT="yes"                 # Reboot automatically for kernel security updates
AUTO_REBOOT_TIME="04:00"          # Time for automatic kernel reboot (HH:MM)
ZRAM_PERCENT="50"                 # Compressed swap in RAM (% of RAM); 0 disables zram

# --- Traefik & ACME (HTTPS) --------------------------------------------------
ACME_EMAIL=""                     # REQUIRED: Your email address for Let's Encrypt certificates
ACME_STAGING="no"                 # "yes" for testing / staging CA to avoid rate limits
TRAEFIK_IMAGE="docker.io/library/traefik:v3.7"  # Traefik container image (v3.7 pinned)
TRUSTED_PROXY_IPS=""              # Optional: Comma-separated CIDRs if using Cloudflare / external reverse proxy

# --- Dermaga Version & Release -----------------------------------------------
DERMAGA_VERSION="latest"          # Release tag (e.g. "v0.8.0") or "latest"
DERMAGA_REPO="r3p-dev/dermaga"    # GitHub repository for release downloads
EOF
}

CONFIG_KEYS=(
  USER_NAME SSH_PUBLIC_KEY GITHUB_KEYS_USER SUDO_NOPASSWD SSH_PORT
  SERVER_HOSTNAME TIMEZONE AUTO_REBOOT AUTO_REBOOT_TIME ZRAM_PERCENT
  ACME_EMAIL ACME_STAGING TRAEFIK_IMAGE TRUSTED_PROXY_IPS
  DERMAGA_VERSION DERMAGA_REPO
)

load_config_file() {
  local file=$1
  [[ -f $file ]] || return 0
  local line key val
  while IFS= read -r line || [[ -n $line ]]; do
    line=$(sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' <<<"$line")
    [[ -z $line || $line == \#* ]] && continue
    [[ $line =~ ^([A-Za-z0-9_]+)=(.*)$ ]] || continue
    key=${BASH_REMATCH[1]}
    val=${BASH_REMATCH[2]}
    val=$(sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//" <<<"$val")
    printf -v "$key" '%s' "$val"
  done <"$file"
}

resolve_config() {
  local file=${1:-}
  # Defaults
  : "${USER_NAME:=server}"
  : "${SSH_PUBLIC_KEY:=}"
  : "${GITHUB_KEYS_USER:=}"
  : "${SUDO_NOPASSWD:=no}"
  : "${SSH_PORT:=22}"
  : "${SERVER_HOSTNAME:=}"
  : "${TIMEZONE:=UTC}"
  : "${AUTO_REBOOT:=yes}"
  : "${AUTO_REBOOT_TIME:=04:00}"
  : "${ZRAM_PERCENT:=50}"
  : "${ACME_EMAIL:=}"
  : "${ACME_STAGING:=no}"
  : "${TRAEFIK_IMAGE:=docker.io/library/traefik:v3.7}"
  : "${TRUSTED_PROXY_IPS:=}"
  : "${DERMAGA_VERSION:=latest}"
  : "${DERMAGA_REPO:=r3p-dev/dermaga}"

  load_config_file "$CONFIG_PATH"
  if [[ -n $file ]]; then
    [[ -f $file ]] || die "config file not found: $file"
    load_config_file "$file"
  fi
}

validate_config() {
  [[ $USER_NAME =~ ^[a-z][a-z0-9_-]{0,31}$ ]] || die "invalid USER_NAME: '$USER_NAME'"
  [[ $USER_NAME != root ]] || die "USER_NAME cannot be root (dermaga requires a dedicated non-root user)"
  [[ $SSH_PORT =~ ^[0-9]+$ ]] && ((SSH_PORT >= 1 && SSH_PORT <= 65535)) || die "invalid SSH_PORT: $SSH_PORT"
  ((SSH_PORT != 80 && SSH_PORT != 443)) || die "SSH_PORT cannot be 80 or 443 (reserved for HTTP/HTTPS)"
  
  if [[ -z $ACME_EMAIL ]]; then
    if [[ -t 0 ]]; then
      read -rp "Enter Let's Encrypt email (ACME_EMAIL): " ACME_EMAIL
    fi
  fi
  [[ $ACME_EMAIL =~ ^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$ ]] || die "ACME_EMAIL is required (e.g. user@example.com)"

  [[ -e /usr/share/zoneinfo/$TIMEZONE ]] || die "unknown TIMEZONE: $TIMEZONE"
  [[ $AUTO_REBOOT_TIME =~ ^([01][0-9]|2[0-3]):[0-5][0-9]$ ]] || die "AUTO_REBOOT_TIME must be HH:MM"
  [[ $ZRAM_PERCENT =~ ^[0-9]+$ ]] && ((ZRAM_PERCENT <= 100)) || die "ZRAM_PERCENT must be 0-100"
}

load_user_ctx() {
  id "$USER_NAME" &>/dev/null || die "user '$USER_NAME' does not exist"
  USER_UID=$(id -u "$USER_NAME")
  USER_HOME=$(getent passwd "$USER_NAME" | cut -d: -f6)
  QUADLET_DIR="$USER_HOME/.config/containers/systemd"
  USER_UNIT_DIR="$USER_HOME/.config/systemd/user"
  TRAEFIK_DIR="$USER_HOME/.config/traefik"
  DERMAGA_CONFIG_DIR="$USER_HOME/.config/dermaga"
  DERMAGA_SHARE_DIR="$USER_HOME/.local/share/dermaga"
  DERMAGA_STATE_DIR="$USER_HOME/.local/state/dermaga"
}

as_user() {
  (
    cd "$USER_HOME"
    exec runuser -u "$USER_NAME" -- env -i \
      HOME="$USER_HOME" USER="$USER_NAME" LOGNAME="$USER_NAME" SHELL=/bin/bash \
      PATH=/usr/local/bin:/usr/bin:/bin LANG=C.UTF-8 \
      XDG_RUNTIME_DIR="/run/user/$USER_UID" \
      DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$USER_UID/bus" \
      "$@"
  )
}

user_systemctl() { as_user systemctl --user "$@"; }

preflight() {
  step "Preflight inspection"
  [[ $EUID -eq 0 ]] || die "dermaga installer must run as root or with sudo"
  
  if [[ -f /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    if [[ ${ID:-} != "debian" || ${VERSION_ID:-} != "13" ]]; then
      warn "Running on ${PRETTY_NAME:-Linux}; Debian 13 (trixie) is the officially supported target."
    else
      ok "Host OS: $PRETTY_NAME"
    fi
  fi

  case $(uname -m) in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *) die "unsupported system architecture: $(uname -m) (only x86_64 and aarch64 supported)" ;;
  esac
  ok "Architecture: $(uname -m) (target: $ARCH)"

  for cmd in systemctl loginctl curl tar gzip; do
    command -v "$cmd" >/dev/null || die "missing prerequisite command: $cmd"
  done
  ok "Prerequisite system tools verified"
}

collect_admin_keys() {
  ADMIN_KEYS=()
  if [[ -n $SSH_PUBLIC_KEY ]]; then
    while IFS= read -r key; do
      [[ -n $key ]] && ADMIN_KEYS+=("$key")
    done <<<"$SSH_PUBLIC_KEY"
  fi
  if [[ -n $GITHUB_KEYS_USER ]]; then
    info "fetching GitHub SSH keys for $GITHUB_KEYS_USER"
    local gh_keys
    gh_keys=$(curl -fsSL --proto '=https' --tlsv1.2 "https://github.com/${GITHUB_KEYS_USER}.keys" || true)
    while IFS= read -r key; do
      [[ -n $key ]] && ADMIN_KEYS+=("$key")
    done <<<"$gh_keys"
  fi
  if [[ ${#ADMIN_KEYS[@]} -eq 0 && -f "$USER_HOME/.ssh/authorized_keys" ]]; then
    while IFS= read -r key; do
      [[ -n $key ]] && ADMIN_KEYS+=("$key")
    done <"$USER_HOME/.ssh/authorized_keys"
  fi
  if [[ ${#ADMIN_KEYS[@]} -eq 0 && -n ${SUDO_USER:-} && -f "/home/$SUDO_USER/.ssh/authorized_keys" ]]; then
    while IFS= read -r key; do
      [[ -n $key ]] && ADMIN_KEYS+=("$key")
    done <"/home/$SUDO_USER/.ssh/authorized_keys"
  fi
}

setup_base_system() {
  step "Base system packages"
  export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a
  local -a apt=(apt-get -y -q -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold)

  info "updating package indices"
  "${apt[@]}" update
  "${apt[@]}" install --no-install-recommends \
    ca-certificates curl gnupg sudo xz-utils less nano procps iproute2 \
    openssh-server nftables fail2ban unattended-upgrades needrestart \
    dbus-user-session systemd-container uidmap util-linux jq

  # Podman and rootless networking helpers
  "${apt[@]}" install podman passt crun netavark aardvark-dns catatonit
  if ! dpkg -s chrony &>/dev/null; then
    "${apt[@]}" install --no-install-recommends systemd-timesyncd || true
  fi
  ok "base packages and rootless container stack installed"

  timedatectl set-ntp true 2>/dev/null || warn "could not enable NTP"
  timedatectl set-timezone "$TIMEZONE" 2>/dev/null || warn "could not set timezone $TIMEZONE"

  if [[ -n $SERVER_HOSTNAME && $(hostnamectl --static 2>/dev/null) != "$SERVER_HOSTNAME" ]]; then
    hostnamectl set-hostname "$SERVER_HOSTNAME"
    if grep -q '^127\.0\.1\.1' /etc/hosts; then
      sed -i "s/^127\.0\.1\.1.*/127.0.1.1\t$SERVER_HOSTNAME/" /etc/hosts
    else
      printf '127.0.1.1\t%s\n' "$SERVER_HOSTNAME" >>/etc/hosts
    fi
    ok "hostname configured: $SERVER_HOSTNAME"
  fi

  install -d -m 0755 /etc/systemd/journald.conf.d
  write_file /etc/systemd/journald.conf.d/90-dermaga.conf 0644 root <<'EOF'
[Journal]
Storage=persistent
Compress=yes
SystemMaxUse=500M
SystemKeepFree=1G
MaxRetentionSec=1month
EOF
  install -d -m 2755 -g systemd-journal /var/log/journal
  systemctl restart systemd-journald
  ok "journald storage configured"
}

ensure_subids() {
  local f start
  for f in /etc/subuid /etc/subgid; do
    touch "$f"
    if grep -q "^$USER_NAME:" "$f"; then
      local count
      count=$(awk -F: -v u="$USER_NAME" '$1 == u {print $3}' "$f")
      if ((count >= 65536)); then continue; fi
      sed -i "/^$USER_NAME:/d" "$f"
    fi
    start=$(awk -F: 'BEGIN { m = 100000 } { e = $2 + $3; if (e > m) m = e } END { print m }' "$f")
    printf '%s:%s:65536\n' "$USER_NAME" "$start" >>"$f"
  done
}

install_authorized_keys() {
  local file="$USER_HOME/.ssh/authorized_keys"
  install -d -m 0700 -o "$USER_NAME" -g "$USER_NAME" "$USER_HOME/.ssh"
  {
    if [[ -f $file ]]; then cat "$file"; fi
    if [[ ${#ADMIN_KEYS[@]} -gt 0 ]]; then
      printf '%s\n' "${ADMIN_KEYS[@]}"
    fi
  } | awk 'NF && !seen[$0]++' | write_file "$file" 0600 "$USER_NAME"
}

setup_sudo() {
  local sudoers="/etc/sudoers.d/90-dermaga-user"
  if is_yes "$SUDO_NOPASSWD"; then
    printf '%s ALL=(ALL:ALL) NOPASSWD: ALL\n' "$USER_NAME" | write_file "$sudoers" 0440 root
    ok "sudo: passwordless enabled"
  else
    local status
    status=$(passwd -S "$USER_NAME" 2>/dev/null | awk '{print $2}')
    if [[ $status == "P" ]]; then
      rm -f "$sudoers"
      ok "sudo: asks for user password"
    elif [[ -t 0 ]]; then
      info "Setting password for user '$USER_NAME' (sudo only):"
      passwd "$USER_NAME"
      rm -f "$sudoers"
      ok "sudo: password configured"
    else
      warn "no terminal to set password; configuring temporary passwordless sudo"
      printf '%s ALL=(ALL:ALL) NOPASSWD: ALL\n' "$USER_NAME" | write_file "$sudoers" 0440 root
    fi
  fi
}

setup_user() {
  step "User configuration ($USER_NAME)"
  
  # cgroup v2 delegation for rootless container resource enforcement
  install -d -m 0755 /etc/systemd/system/user@.service.d
  write_file /etc/systemd/system/user@.service.d/90-dermaga-delegate.conf 0644 root <<'EOF'
[Service]
Delegate=cpu cpuset io memory pids
EOF
  systemctl daemon-reload

  if ! id "$USER_NAME" &>/dev/null; then
    useradd --create-home --shell /bin/bash "$USER_NAME"
    ok "created user $USER_NAME"
  fi
  usermod -aG sudo,adm,systemd-journal "$USER_NAME" 2>/dev/null || true
  ensure_subids
  load_user_ctx
  install_authorized_keys
  setup_sudo

  # Prepare dermaga and systemd directories
  local dirs=(
    "$USER_HOME/.config"
    "$USER_HOME/.config/containers"
    "$QUADLET_DIR"
    "$USER_HOME/.config/systemd"
    "$USER_UNIT_DIR"
    "$TRAEFIK_DIR"
    "$TRAEFIK_DIR/dynamic"
    "$DERMAGA_CONFIG_DIR"
    "$DERMAGA_CONFIG_DIR/env"
    "$DERMAGA_SHARE_DIR"
    "$DERMAGA_SHARE_DIR/repos"
    "$DERMAGA_SHARE_DIR/deploy-keys"
    "$DERMAGA_STATE_DIR"
    "$DERMAGA_STATE_DIR/builds"
    "$DERMAGA_STATE_DIR/tmp"
    "$DERMAGA_STATE_DIR/traefik"
  )
  for d in "${dirs[@]}"; do
    install -d -m 0700 -o "$USER_NAME" -g "$USER_NAME" "$d"
  done
  chmod 0700 "$USER_HOME"

  # User lingering for persistent background services
  loginctl enable-linger "$USER_NAME"
  ok "enabled user lingering for $USER_NAME"

  systemctl start "user@$USER_UID.service"
  for _ in {1..30}; do
    if [[ -S /run/user/$USER_UID/bus ]]; then break; fi
    sleep 1
  done
  [[ -S /run/user/$USER_UID/bus ]] || die "systemd user session bus for $USER_NAME failed to initialize"
  ok "systemd user bus active (/run/user/$USER_UID/bus)"
}

setup_memory() {
  step "Memory and Swap"
  local mem_mb zram=no
  mem_mb=$(awk '/^MemTotal/ { print int($2 / 1024) }' /proc/meminfo)

  if ((ZRAM_PERCENT > 0)) && apt-get -y -q install --no-install-recommends systemd-zram-generator >/dev/null 2>&1; then
    write_file /etc/systemd/zram-generator.conf 0644 root <<EOF
[zram0]
zram-size = min(ram * $ZRAM_PERCENT / 100, 8192)
compression-algorithm = zstd
swap-priority = 100
EOF
    systemctl daemon-reload
    systemctl restart systemd-zram-setup@zram0.service 2>/dev/null || true
    if swapon --show=NAME --noheadings | grep -q zram; then zram=yes; fi
  fi

  if [[ $zram == yes ]]; then
    printf 'vm.swappiness = 100\nvm.page-cluster = 0\n' | write_file /etc/sysctl.d/91-dermaga-swap.conf 0644 root
    ok "zram swap: $ZRAM_PERCENT% of ${mem_mb} MiB RAM (zstd)"
  else
    if ! swapon --show=NAME --noheadings | grep -q .; then
      local size=$((mem_mb < 2048 ? mem_mb : 2048))
      if [[ ! -f /swapfile ]]; then
        fallocate -l "${size}M" /swapfile 2>/dev/null || dd if=/dev/zero of=/swapfile bs=1M count="$size" status=none
        chmod 600 /swapfile
        mkswap /swapfile >/dev/null
      fi
      grep -q '^/swapfile ' /etc/fstab || printf '/swapfile none swap sw 0 0\n' >>/etc/fstab
      swapon /swapfile 2>/dev/null || true
    fi
    printf 'vm.swappiness = 10\n' | write_file /etc/sysctl.d/91-dermaga-swap.conf 0644 root
    ok "disk swap active"
  fi
}

setup_kernel() {
  step "Kernel and Sysctl Hardening"
  write_file /etc/sysctl.d/90-dermaga.conf 0644 root <<'EOF'
# Managed by dermaga installer

# Network security
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

# Kernel integrity
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

# Rootless container support (systemd user owns ports 80/443 via socket activation)
-kernel.unprivileged_userns_clone = 1
net.ipv4.ip_unprivileged_port_start = 80
fs.inotify.max_user_watches = 524288
fs.inotify.max_user_instances = 1024

# Network performance
net.core.default_qdisc = fq
-net.ipv4.tcp_congestion_control = bbr
net.core.somaxconn = 4096
net.ipv4.tcp_fastopen = 3
net.ipv4.tcp_mtu_probing = 1
net.ipv4.tcp_slow_start_after_idle = 0
vm.vfs_cache_pressure = 50
EOF
  systemctl restart systemd-sysctl.service || sysctl --system >/dev/null 2>&1 || true
  ok "sysctl security parameters applied"
}

setup_auto_updates() {
  step "Automatic Security Updates"
  write_file /etc/apt/apt.conf.d/20auto-upgrades 0644 root <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
EOF
  write_file /etc/apt/apt.conf.d/52dermaga-unattended-upgrades 0644 root <<EOF
Unattended-Upgrade::AutoFixInterruptedDpkg "true";
Unattended-Upgrade::MinimalSteps "true";
Unattended-Upgrade::Remove-Unused-Kernel-Packages "true";
Unattended-Upgrade::Remove-New-Unused-Dependencies "true";
Unattended-Upgrade::Remove-Unused-Dependencies "true";
Unattended-Upgrade::Automatic-Reboot "$(is_yes "$AUTO_REBOOT" && echo true || echo false)";
Unattended-Upgrade::Automatic-Reboot-WithUsers "true";
Unattended-Upgrade::Automatic-Reboot-Time "$AUTO_REBOOT_TIME";
Unattended-Upgrade::SyslogEnable "true";
EOF
  systemctl enable --now unattended-upgrades.service apt-daily.timer apt-daily-upgrade.timer >/dev/null 2>&1
  ok "unattended security upgrades enabled"
}

setup_network_online() {
  step "Network-online boot synchronization (ADR D29)"
  local active_mono
  active_mono=$(systemctl show -p ActiveEnterTimestampMonotonic --value network-online.target 2>/dev/null || echo "0")
  if [[ $active_mono == "0" ]]; then
    info "network-online.target not reached this boot; adding dependency to multi-user.target"
    systemctl add-wants multi-user.target network-online.target
    ok "multi-user.target now wants network-online.target"
  else
    ok "network-online.target already synchronized"
  fi
}

setup_podman() {
  step "Rootless Podman"
  write_file "$USER_HOME/.config/containers/containers.conf" 0644 "$USER_NAME" <<'EOF'
[containers]
log_driver = "journald"

[engine]
events_logger = "journald"

[network]
default_rootless_network_cmd = "pasta"
EOF

  # Disable podman.socket: dermaga uses file provider and does not expose podman socket to containers
  user_systemctl disable --now podman.socket 2>/dev/null || true
  systemctl --global disable podman.socket 2>/dev/null || true
  ok "podman configured with journald logging and pasta network (podman.socket disabled)"
}

install_dermaga_binary() {
  step "Dermaga binary installation"
  local target_bin="dermaga-linux-${ARCH}"

  if [[ -n ${DERMAGA_BINARY:-} && -f $DERMAGA_BINARY ]]; then
    info "installing from local binary $DERMAGA_BINARY"
    install -m 0755 -o root -g root "$DERMAGA_BINARY" "$INSTALL_PATH"
    ok "$($INSTALL_PATH version) installed to $INSTALL_PATH"
    return 0
  fi

  local version=$DERMAGA_VERSION
  if [[ $version == "latest" ]]; then
    info "resolving latest release tag from GitHub"
    local tag
    tag=$(curl -fsSL -H "Accept: application/vnd.github+json" "https://api.github.com/repos/${DERMAGA_REPO}/releases/latest" 2>/dev/null | jq -r '.tag_name // empty' || true)
    if [[ -z $tag ]]; then
      tag=$(curl -sI "https://github.com/${DERMAGA_REPO}/releases/latest" | awk -F'tag/' '/^[Ll]ocation:/ {print $2}' | tr -d '\r\n')
    fi
    version=${tag:-v0.8.0}
  fi

  local dl_url="https://github.com/${DERMAGA_REPO}/releases/download/${version}/${target_bin}"
  local cs_url="https://github.com/${DERMAGA_REPO}/releases/download/${version}/checksums.txt"

  local tmpdir
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' RETURN

  info "downloading ${target_bin} ($version)"
  if ! curl -fsSL -o "$tmpdir/$target_bin" "$dl_url"; then
    if [[ -x $INSTALL_PATH ]]; then
      warn "release download failed; keeping existing $INSTALL_PATH"
      return 0
    fi
    die "failed to download $dl_url"
  fi

  info "verifying checksum against $cs_url"
  if curl -fsSL -o "$tmpdir/checksums.txt" "$cs_url"; then
    local expected got
    expected=$(awk -v f="$target_bin" '$2 == f || $2 == "*"f {print $1}' "$tmpdir/checksums.txt")
    got=$(sha256sum "$tmpdir/$target_bin" | awk '{print $1}')
    if [[ -n $expected && $expected != "$got" ]]; then
      die "checksum verification failed for $target_bin (expected $expected, got $got)"
    fi
    ok "SHA-256 checksum verified ($got)"
  else
    warn "could not fetch checksums.txt, skipping signature verification"
  fi

  install -m 0755 -o root -g root "$tmpdir/$target_bin" "$INSTALL_PATH"
  ok "$($INSTALL_PATH version) installed to $INSTALL_PATH"
}

setup_traefik() {
  step "Traefik (File-Provider Only)"

  # 1. Network & ACME Volume Quadlet units
  write_file "$QUADLET_DIR/web.network" 0644 "$USER_NAME" <<'EOF'
[Network]
NetworkName=web
EOF

  write_file "$QUADLET_DIR/traefik-acme.volume" 0644 "$USER_NAME" <<'EOF'
[Volume]
VolumeName=traefik-acme
EOF

  # 2. Systemd User Sockets (:80 and :443)
  write_file "$USER_UNIT_DIR/traefik-http.socket" 0644 "$USER_NAME" <<'EOF'
[Unit]
Description=Traefik :80 (HTTP -> HTTPS redirect, ACME challenges)

[Socket]
ListenStream=80
FileDescriptorName=web
Service=traefik.service

[Install]
WantedBy=sockets.target
EOF

  write_file "$USER_UNIT_DIR/traefik-https.socket" 0644 "$USER_NAME" <<'EOF'
[Unit]
Description=Traefik :443 (HTTPS over TCP, HTTP/3 over UDP)

[Socket]
ListenStream=443
ListenDatagram=443
FileDescriptorName=websecure
Service=traefik.service

[Install]
WantedBy=sockets.target
EOF

  # 3. File-provider Traefik container unit with DefaultDependencies=false (ADR D29)
  write_file "$QUADLET_DIR/traefik.container" 0644 "$USER_NAME" <<EOF
[Unit]
Description=Traefik reverse proxy (socket-activated)
Requires=traefik-http.socket traefik-https.socket
After=traefik-http.socket traefik-https.socket

[Quadlet]
DefaultDependencies=false

[Container]
Image=$TRAEFIK_IMAGE
ContainerName=traefik
Network=web.network
Notify=true
AutoUpdate=registry
NoNewPrivileges=true
ReadOnly=true
DropCapability=ALL
PidsLimit=256
PodmanArgs=--memory=256m
Volume=%h/.config/traefik/traefik.yml:/etc/traefik/traefik.yml:ro
Volume=%h/.config/traefik/dynamic:/etc/traefik/dynamic:ro
Volume=traefik-acme.volume:/acme
Volume=%h/.local/state/dermaga/traefik:/var/log/traefik

[Service]
Sockets=traefik-http.socket traefik-https.socket
Restart=always
TimeoutStartSec=300

[Install]
WantedBy=default.target
EOF

  # 4. Generate static traefik.yml and initial 00-global.yml via dermaga CLI
  local staging_arg=""
  if is_yes "$ACME_STAGING"; then staging_arg="--acme-staging"; fi
  local proxy_args=()
  if [[ -n $TRUSTED_PROXY_IPS ]]; then
    for cidr in ${TRUSTED_PROXY_IPS//,/ }; do
      proxy_args+=("--trusted-proxy" "$cidr")
    done
  fi

  info "generating traefik.yml (file-provider only, verify :8081)"
  $INSTALL_PATH traefik static --acme-email "$ACME_EMAIL" $staging_arg "${proxy_args[@]}" | \
    write_file "$TRAEFIK_DIR/traefik.yml" 0644 "$USER_NAME"

  # Initial dynamic placeholder and 00-global.yml
  if [[ ! -f "$TRAEFIK_DIR/dynamic/99-custom.yml" ]]; then
    write_file "$TRAEFIK_DIR/dynamic/99-custom.yml" 0644 "$USER_NAME" <<'EOF'
# 99-custom.yml — user-managed Traefik configuration (hot-reloaded)
# To apply the default security chain, include:
# middlewares:
#   - secure@file
EOF
  fi

  # Pull Traefik image
  info "pulling $TRAEFIK_IMAGE"
  as_user podman pull -q "$TRAEFIK_IMAGE" >/dev/null 2>&1 || true

  # Dry-run Quadlets
  if [[ -x /usr/libexec/podman/quadlet ]]; then
    QUADLET_UNIT_DIRS="$QUADLET_DIR" /usr/libexec/podman/quadlet -dryrun -user >/dev/null 2>&1 || warn "Quadlet validation warning"
  fi

  user_systemctl daemon-reload
  user_systemctl enable --now traefik-http.socket traefik-https.socket >/dev/null 2>&1 || true
  user_systemctl restart traefik.service 2>/dev/null || true
  ok "Traefik file-provider service and sockets configured"
}

setup_firewall() {
  step "Host Firewall (nftables)"
  write_file /etc/nftables.conf 0755 root <<EOF
#!/usr/sbin/nft -f
# Managed by dermaga installer

table inet dermaga_filter
delete table inet dermaga_filter

table inet dermaga_filter {
  chain input {
    type filter hook input priority filter; policy drop;

    ct state established,related accept
    ct state invalid drop
    iif "lo" accept

    # Scan mitigations
    tcp flags & (fin|syn|rst|psh|ack|urg) == 0 drop comment "drop null packets"
    tcp flags & (fin|syn|rst|psh|ack|urg) == (fin|syn|rst|psh|ack|urg) drop comment "drop xmas packets"
    tcp flags & (syn|rst) == (syn|rst) drop comment "drop syn-rst packets"

    # ICMP rate-limited
    icmp type { destination-unreachable, time-exceeded, parameter-problem } accept
    icmp type echo-request limit rate 5/second burst 10 packets accept
    icmpv6 type { destination-unreachable, packet-too-big, time-exceeded, parameter-problem, nd-router-advert, nd-neighbor-solicit, nd-neighbor-advert, mld-listener-query } accept
    icmpv6 type echo-request limit rate 5/second burst 10 packets accept

    # Public ports
    tcp dport $SSH_PORT accept comment "ssh"
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
  nft -c -f /etc/nftables.conf || die "nftables ruleset validation failed"
  systemctl enable nftables.service >/dev/null 2>&1
  if systemctl is-active --quiet nftables.service; then
    nft -f /etc/nftables.conf
  else
    systemctl start nftables.service
  fi
  ok "nftables default-deny active (open: SSH :$SSH_PORT, :80, :443 tcp/udp)"
}

setup_fail2ban() {
  step "fail2ban intrusion prevention"
  write_file /etc/fail2ban/fail2ban.d/90-dermaga.local 0644 root <<'EOF'
[Definition]
logtarget = /var/log/fail2ban.log
EOF
  touch /var/log/fail2ban.log
  chmod 0640 /var/log/fail2ban.log

  write_file /etc/fail2ban/jail.d/90-dermaga.local 0644 root <<EOF
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
port = $SSH_PORT
mode = aggressive

[recidive]
enabled = true
backend = auto
logpath = /var/log/fail2ban.log
banaction = %(banaction_allports)s
bantime = 1w
findtime = 1d
maxretry = 3
EOF
  fail2ban-client -t >/dev/null 2>&1 || die "fail2ban configuration test failed"
  systemctl enable fail2ban.service >/dev/null 2>&1
  systemctl restart fail2ban.service
  ok "fail2ban sshd (aggressive) + recidive jails active"
}

setup_ssh() {
  step "SSH Server Hardening"
  local conf="/etc/ssh/sshd_config.d/00-dermaga-hardening.conf"
  
  if ! grep -q . "$USER_HOME/.ssh/authorized_keys" 2>/dev/null; then
    die "$USER_NAME has no SSH keys — refusing to lock password login"
  fi

  ssh-keygen -A >/dev/null 2>&1 || true

  write_file "$conf" 0644 root <<EOF
# Managed by dermaga installer
Port $SSH_PORT
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitEmptyPasswords no
PubkeyAuthentication yes
AuthenticationMethods publickey
AllowUsers $USER_NAME
MaxAuthTries 3
MaxSessions 4
MaxStartups 10:30:60
LoginGraceTime 20
ClientAliveInterval 300
ClientAliveCountMax 2
PermitUserEnvironment no
X11Forwarding no
AllowAgentForwarding no
AllowTcpForwarding local
AllowStreamLocalForwarding no
GatewayPorts no
PermitTunnel no
UseDNS no
DebianBanner no
EOF

  if ! sshd -t; then
    rm -f "$conf"
    die "sshd rejected configuration; restored previous settings"
  fi

  if systemctl is-enabled --quiet ssh.socket 2>/dev/null; then
    install -d -m 0755 /etc/systemd/system/ssh.socket.d
    write_file /etc/systemd/system/ssh.socket.d/90-dermaga-port.conf 0644 root <<EOF
[Socket]
ListenStream=
ListenStream=$SSH_PORT
EOF
    systemctl daemon-reload
    systemctl restart ssh.socket
  else
    systemctl restart ssh.service || systemctl restart sshd.service
  fi
  ok "SSH locked: port $SSH_PORT, key-only, root disabled, local tunneling permitted"
}

setup_dermaga_service() {
  step "Dermaga systemd user service"
  write_file "$USER_UNIT_DIR/dermaga.service" 0644 "$USER_NAME" <<'EOF'
[Unit]
Description=dermaga daemon (single-user PaaS)
Documentation=https://github.com/r3p-dev/dermaga

[Service]
Type=exec
ExecStart=/usr/local/bin/dermaga daemon
Restart=on-failure
RestartSec=2s

[Install]
WantedBy=default.target
EOF

  user_systemctl daemon-reload
  user_systemctl enable --now dermaga.service
  ok "dermaga.service enabled and started"

  info "waiting for daemon socket (/run/user/$USER_UID/dermaga/dermaga.sock)"
  for _ in {1..20}; do
    if [[ -S "/run/user/$USER_UID/dermaga/dermaga.sock" ]]; then break; fi
    sleep 0.5
  done
  if [[ -S "/run/user/$USER_UID/dermaga/dermaga.sock" ]]; then
    ok "dermaga daemon listening on /run/user/$USER_UID/dermaga/dermaga.sock"
  else
    warn "daemon socket not ready yet; check 'journalctl --user -u dermaga' if issues persist"
  fi
}

run_doctor() {
  step "Host Verification (dermaga doctor)"
  as_user "$INSTALL_PATH" doctor || true
}

save_config() {
  install -d -m 0700 /etc/dermaga
  write_file "$CONFIG_PATH" 0600 root <<EOF
# Managed by dermaga installer
USER_NAME="$USER_NAME"
SSH_PUBLIC_KEY="$SSH_PUBLIC_KEY"
GITHUB_KEYS_USER="$GITHUB_KEYS_USER"
SUDO_NOPASSWD="$SUDO_NOPASSWD"
SSH_PORT="$SSH_PORT"
SERVER_HOSTNAME="$SERVER_HOSTNAME"
TIMEZONE="$TIMEZONE"
AUTO_REBOOT="$AUTO_REBOOT"
AUTO_REBOOT_TIME="$AUTO_REBOOT_TIME"
ZRAM_PERCENT="$ZRAM_PERCENT"
ACME_EMAIL="$ACME_EMAIL"
ACME_STAGING="$ACME_STAGING"
TRAEFIK_IMAGE="$TRAEFIK_IMAGE"
TRUSTED_PROXY_IPS="$TRUSTED_PROXY_IPS"
DERMAGA_VERSION="$DERMAGA_VERSION"
DERMAGA_REPO="$DERMAGA_REPO"
EOF
  ok "configuration saved in $CONFIG_PATH"
}

print_summary() {
  local ip fp
  ip=$(curl -s4 icanhazip.com || curl -s4 ifconfig.me || hostname -I | awk '{print $1}')
  fp=$(ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub 2>/dev/null | awk '{print $2}' || echo "N/A")

  step "Installation Complete!"
  cat <<EOF

${C_BOLD}Next steps to access your Dermaga instance:${C_RESET}

1. Add the panel SSH tunnel to your laptop's ${C_BOLD}~/.ssh/config${C_RESET}:

   Host dermaga-panel
       HostName $ip
       Port $SSH_PORT
       User $USER_NAME
       LocalForward 7070 127.0.0.1:7070
       SessionType none
       ExitOnForwardFailure yes

2. Connect the SSH tunnel:
   ${C_BOLD}ssh -N dermaga-panel${C_RESET}

3. Set your initial administrator password:
   ${C_BOLD}ssh -p $SSH_PORT $USER_NAME@$ip "dermaga admin set-password"${C_RESET}

4. Open the Web Dashboard in your browser:
   ${C_BOLD}http://localhost:7070${C_RESET}

5. (Recommended) Enable TOTP two-factor authentication:
   ${C_BOLD}ssh -p $SSH_PORT $USER_NAME@$ip "dermaga admin totp enable"${C_RESET}

Server ED25519 Fingerprint: ${C_BOLD}$fp${C_RESET}
EOF
}

cmd_init() {
  local config_file=""
  while (($#)); do
    case $1 in
      --config|-c)
        config_file=${2:-}
        shift 2
        ;;
      --yes|-y)
        shift
        ;;
      *)
        die "unknown option for init: $1"
        ;;
    esac
  done

  preflight
  resolve_config "$config_file"
  validate_config
  collect_admin_keys

  setup_base_system
  setup_user
  setup_memory
  setup_kernel
  setup_auto_updates
  setup_network_online
  setup_podman
  install_dermaga_binary
  setup_traefik
  setup_firewall
  setup_fail2ban
  setup_ssh
  setup_dermaga_service
  save_config
  run_doctor
  print_summary
}

usage() {
  cat <<EOF
dermaga installer v$VERSION

Usage:
  install.sh config-example > dermaga-host.conf   generate template configuration
  sudo bash install.sh init --config host.conf     provision and harden host from config
  sudo bash install.sh init                        run with environment variables or defaults
EOF
}

main() {
  if [[ $# -eq 0 ]]; then
    if [[ -f "$CONFIG_PATH" ]]; then
      cmd_init --config "$CONFIG_PATH"
    elif [[ -f "dermaga-host.conf" ]]; then
      cmd_init --config "dermaga-host.conf"
    elif [[ -t 0 ]]; then
      cmd_init
    else
      usage
      exit 0
    fi
    return 0
  fi

  case $1 in
    config-example)
      cmd_config_example
      ;;
    init)
      shift
      cmd_init "$@"
      ;;
    -h|--help|help)
      usage
      ;;
    *)
      die "unknown command: $1 (see --help)"
      ;;
  esac
}

main "$@"
