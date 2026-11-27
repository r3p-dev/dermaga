# Development & Test Environment Setup

This document describes the environment setup for developing and testing **Dermaga**, including the development workstation, the test virtual machine (`dermaga-vm`), libvirt snapshots, the test GitHub App, webhook forwarding via smee/gosmee, and teardown procedures.

---

## 1. Development Workstation Setup

Dermaga's build and development workflow requires:

- **Operating System:** Linux (Debian, Fedora, Ubuntu, or Arch) with systemd and KVM/libvirt virtualization support.
- **Go Toolchain:** Go 1.27+ (standard library first, single static binary compiled with `CGO_ENABLED=0`; pinned in `go.mod` and CI).
- **JavaScript Toolchain:**
  - **Bun:** `bun` v1.4.4+ (used for building the SvelteKit 3 UI and producing `bun.lock`; pinned in CI).
  - **Node.js:** Node `v22+` / `v24` (LTS) for auxiliary tools and language servers.
- **System Utilities:** `make`, `git`, `curl`, `jq`, `libvirt-clients` (`virsh`), `virt-install`.
- **SSH Configuration:** `~/.ssh/config` on the workstation has two entries for the test VM. The key
  `~/.ssh/dermaga-vm` is the one cloud-init put into the image for user `debian`:
  ```sshconfig
  # snapshot `fresh`: only cloud-init's user debian, sshd on port 22
  Host dermaga-vm-init
      HostName 192.168.122.217
      User debian
      IdentityFile ~/.ssh/dermaga-vm
      IdentitiesOnly yes

  # after vps-setup.sh or the dermaga installer: user server on port 4222
  Host dermaga-vm
      HostName 192.168.122.217
      Port 4222
      User server
      IdentityFile ~/.ssh/dermaga-vm
      IdentitiesOnly yes
  ```
  The workstation's config also has an unmarked `Host dermaga` entry for the VM, so the dermaga
  laptop command adds its block as `Host dermaga-dermaga-vm` there (`LAP-1`).

---

## 2. Test VM (`dermaga-vm`) & Libvirt Snapshots

### 2.1 Virtual Machine Topology
- **VM Name:** `dermaga-vm`
- **Guest OS:** Debian GNU/Linux 13 (Trixie), minimal cloud image (`amd64`).
- **Primary User:** `server` (UID 1001, member of `sudo` group, passwordless sudo). Agents still never run `sudo`
  without the owner's explicit approval (`AGENTS.md` "Never").
- **Systemd Lingering:** Enabled for user `server` (`loginctl enable-linger server`).
- **Network Stack:** Rootless Podman 5.4.2 + Netavark bridge network `web` (`10.89.0.0/24`) + Pasta rootless backend.
- **SSH Port:** OpenSSH server listens on port `4222` (configured per `vps-setup.sh`).

### 2.2 Baseline Snapshots
Two baseline snapshots exist in libvirt for `dermaga-vm`:
1. `fresh`: Minimal clean Debian 13 cloud image immediately post-cloud-init.
2. `vps-setup`: Hardened Debian 13 baseline running `vps-setup.sh` (sshd on port 4222, `server` lingering, Traefik v3 Quadlet units on network `web`, socket-activated on ports 80 and 443 TCP/UDP).

### 2.3 The Cloud-Init Placeholder ISO (Critical)
- **Path on Host:** `/var/lib/libvirt/boot/virtinst-w157g_3q-cloudinit.iso`
- **Why it exists:** When `dermaga-vm` was initially provisioned with `virt-install`, a temporary cloud-init ISO was created in `/var/lib/libvirt/boot/` and attached as CD-ROM device `sda`. When the initial snapshots (`fresh`, `vps-setup`) were taken, libvirt captured the domain hardware topology including this ISO file path. Host package management or `/tmp` cleaners subsequently deleted the temporary ISO.
- **Why it must not be deleted:** Libvirt enforces that all disk source paths recorded in a snapshot's XML must exist on the host filesystem when restoring that snapshot (even empty read-only CD-ROM devices). If missing, `virsh snapshot-revert` aborts with:
  ```text
  error: Cannot access storage file '/var/lib/libvirt/boot/virtinst-w157g_3q-cloudinit.iso': No such file or directory
  ```
  A persistent 0-byte placeholder file exists on the host with correct SELinux context:
  ```bash
  sudo touch /var/lib/libvirt/boot/virtinst-w157g_3q-cloudinit.iso
  sudo chmod 644 /var/lib/libvirt/boot/virtinst-w157g_3q-cloudinit.iso
  sudo restorecon /var/lib/libvirt/boot/virtinst-w157g_3q-cloudinit.iso
  ```
  **This file must never be removed.**
- **Prevention for Future VM Installations:**
  When provisioning future test VMs, always eject transient installer/cloud-init media before taking snapshots:
  ```bash
  virsh --connect qemu:///system change-media <vm-name> sda --eject --config
  ```
  Note that a live eject (`--live`) is ephemeral and is undone when reverting to an older snapshot.

### 2.4 Running `m3_check.sh access` Without Locking Out Your Workstation

`test/vm/m3_check.sh access` intentionally runs denied SSH login attempts (`server` with password auth disabled, `root`, and `debian`) to prove `sshd` rejects them. Those failed attempts trigger `fail2ban` (`sshd` jail) on `dermaga-vm` a few seconds later.

To prevent banning your workstation IP:
1. `m3_check.sh access` opens an SSH `ControlMaster` connection (`ControlPersist=120`) before the denied-login checks and records your workstation IP (`SSH_CLIENT`).
2. After the denied-login checks, it polls `fail2ban-client status sshd` until the ban settles, logs the status as proof that `fail2ban` works, and runs `fail2ban-client unban <workstation_ip>` over the existing `ControlMaster` socket.
3. Pass `DERMAGA_SUDO_PASS_FILE=<path-to-sudo-password-file>` when `server` uses a sudo password (as set by `drive_install.py`).
4. After `m3_check.sh access` finishes, wait at least 30 s from the last denied attempt and verify a fresh `ssh dermaga` connection.

---

## 3. Safe Reboot Procedure (ACPI Poweroff vs Guest Reboot)

### 3.1 The `virsh reboot` Pitfall
On `dermaga-vm`, `qemu-guest-agent` is **not installed**.
When `virsh reboot dermaga-vm` is executed without a guest agent, libvirt falls back to ACPI signal injection. Because standard x86 ACPI lacks a dedicated hardware reboot button, QEMU asserts the ACPI power button (`system_powerdown`).
On the Debian guest, `systemd-logind` intercepts this as `Power key pressed short.` and activates `poweroff.target`. When the guest shuts down via poweroff, QEMU executes its `<on_poweroff>destroy</on_poweroff>` policy, leaving the VM permanently in the `shut off` state rather than restarting.

### 3.2 Recommended Reboot Method
To reboot `dermaga-vm` cleanly and allow QEMU to execute `<on_reboot>restart</on_reboot>`:
1. **Via SSH (when `server` has passwordless sudo, e.g. `vps-setup` snapshot):**
   ```bash
   ssh dermaga-vm "sudo -n reboot"
   ```
   *Mechanism:* Systemd activates `reboot.target`. The guest Linux kernel calls `kernel_restart()`, signalling a true reboot event to QEMU. QEMU detects the reboot and restarts the VM automatically in ~4 seconds.
   *(Rule: automated agents must ask the project owner first and receive explicit permission before executing any command requiring sudo, including guest reboots).*
2. **From the Host (after the M3 installer sets a sudo password for `server`, without `qemu-guest-agent`):**
   ```bash
   virsh --connect qemu:///system reboot dermaga-vm
   # Without qemu-guest-agent, ACPI powerdown leaves the VM shut off; start it again:
   virsh --connect qemu:///system start dermaga-vm
   ```
3. **Alternative (Guest Agent):**
   If `qemu-guest-agent` is installed on the guest, libvirt can reboot cleanly via:
   ```bash
   virsh --connect qemu:///system reboot dermaga-vm --mode agent
   ```

---

## 4. Test GitHub App & Webhook Forwarding

### 4.1 Manifest Flow
Dermaga connects to GitHub exclusively via a GitHub App created through the **GitHub App Manifest Flow**:
1. During first-run setup on `http://localhost:7070/setup`, the panel generates a manifest JSON payload.
2. The user submits the manifest to GitHub via HTML form POST to `https://github.com/settings/apps/new`.
3. GitHub redirects back to `http://localhost:7070/setup/github/callback?code=...` with a temporary conversion code.
4. The panel exchanges the code via `POST /app-manifests/{code}/conversions` to receive the App ID, PEM private key, client credentials, and sets the webhook secret via `PATCH /app/hook/config`.

### 4.2 Webhook Forwarding Behind NAT (`gosmee`)
For local development or testing behind NAT where GitHub cannot deliver webhooks directly to the VM:
1. The owner creates a channel on [smee.io](https://smee.io) and hands its URL over in the
   environment variable `DERMAGA_SMEE_URL` or the gitignored file `tmp/smee-url`. The URL is never
   printed in logs, evidence or commits.
2. `gosmee` v0.32.1 (pinned in `docs/DEPENDENCIES.md` §5) runs on the workstation from
   `tmp/tools/bin/gosmee`.
3. Deliveries must go the public way: Traefik on the VM's 443, the hook-domain router, `hook-relay`,
   then `hooks.sock`. The panel on `localhost:7070` does **not** serve `/hooks/github-app`; the
   command that used to stand here (`gosmee client … http://localhost:7070/hooks/github-app`) could
   never deliver. The hook domain does not resolve on the workstation and the certificate comes
   from Pebble, so the forwarder must pin the name to the VM's IP and trust the Pebble root, as
   `curl --resolve <hook-domain>:443:192.168.122.217 --cacert <pebble root>` does. gosmee has no
   such options, so its `--exec` hook runs a forwarder that does exactly that (verified in M7b on
   `v0.7.0-rc.4`: real deliveries accepted, the same body with one byte changed refused with 401):
   ```fish
   tmp/tools/bin/gosmee client --nocolor --noReplay --exec $PWD/test/vm/gosmee-forward.sh (cat tmp/smee-url) http://127.0.0.1:9
   ```
   `test/vm/gosmee-forward.sh` posts `$GOSMEE_PAYLOAD_FILE` with `curl --data-binary` (byte for
   byte) and the GitHub headers from `$GOSMEE_HEADERS_FILE` (`Content-Type`, `User-Agent`,
   `X-GitHub-Event`, `X-GitHub-Delivery`, `X-GitHub-Hook-ID`, `X-Hub-Signature-256`) to
   `https://<hook-domain>/hooks/github-app`, and logs only the event, delivery ID, body size and
   hash, and the HTTP status (to `tmp/evidence/M7/logs/` by default; the Pebble root is expected at
   `tmp/evidence/M7/pebble-minica.pem`; the script header lists the overrides). `--noReplay` keeps
   gosmee itself from posting anywhere; `http://127.0.0.1:9` is only a required placeholder. For the run the owner sets
   the GitHub App's webhook URL to the smee channel (secret unchanged) and afterwards back to
   `https://<hook-domain>/hooks/github-app`.
4. **Security Warning:** Smee channels are public and unauthenticated. **Never** point production webhooks or sensitive repositories at a public smee channel. Use smee strictly with throwaway test repositories.

---

## 5. Teardown Procedures

To reset the test environment cleanly after spikes or milestone verification:

1. **Stop Forwarding Services:**
   Terminate any running `gosmee` client processes on the host:
   ```bash
   killall gosmee 2>/dev/null || true
   ```
2. **Revert VM to Clean Baseline:**
   Revert `dermaga-vm` to the verified `vps-setup` snapshot *(Rule: automated agents must ask the project owner first before reverting or modifying snapshots)*:
   ```bash
   virsh --connect qemu:///system snapshot-revert dermaga-vm vps-setup
   virsh --connect qemu:///system start dermaga-vm
   ```
   *Post-revert verification:*
   ```bash
   ssh dermaga-vm uptime
   ```
3. **GitHub App Cleanup (Optional):**
   If a test GitHub App is no longer needed, delete it from GitHub under **Account Settings → Developer settings → GitHub Apps → [App Name] → Advanced → Delete GitHub App**.

---

## 6. Milestone M1 Setup & Operations

### 6.1 Building Binaries
Build the host binary and release binaries from the workspace root:
```bash
# Build static binary for current host architecture
make build

# Build static release binaries for both amd64 and arm64
make build-all
```
Binaries are placed in `bin/` (`bin/dermaga`, `bin/dermaga-linux-amd64`, `bin/dermaga-linux-arm64`).

### 6.2 Installing the Systemd User Service on `dermaga-vm`
1. Ensure required user directories exist:
   ```bash
   ssh dermaga-vm "mkdir -p ~/.local/bin ~/.config/systemd/user ~/.local/share/dermaga"
   ```
2. Copy the binary to `~/.local/bin/dermaga`:
   ```bash
   scp bin/dermaga-linux-amd64 dermaga-vm:~/.local/bin/dermaga
   ssh dermaga-vm "chmod +x ~/.local/bin/dermaga"
   ```
3. Create the systemd user service definition (`~/.config/systemd/user/dermaga.service`):
   ```ini
   [Unit]
   Description=Dermaga Host Agent
   After=network.target

   [Service]
   Type=simple
   ExecStart=%h/.local/bin/dermaga daemon
   Restart=always
   RestartSec=3s
   Environment=XDG_DATA_HOME=%h/.local/share
   Environment=XDG_CONFIG_HOME=%h/.config

   [Install]
   WantedBy=default.target
   ```
4. Enable and start the service:
   ```bash
   ssh dermaga-vm "systemctl --user daemon-reload && systemctl --user enable --now dermaga.service"
   ```

### 6.3 Atomic Binary Replacement (Avoiding "Text File Busy")
Linux kernels return `ETXTBSY` ("Text file busy") if `scp` attempts to write directly into an actively running binary executable. To update the running daemon safely without downtime:
```bash
# 1. Copy new binary to a temporary filename
scp bin/dermaga-linux-amd64 dermaga-vm:~/.local/bin/dermaga.new

# 2. Make executable, atomically replace via rename, and restart service
ssh dermaga-vm "chmod +x ~/.local/bin/dermaga.new && mv ~/.local/bin/dermaga.new ~/.local/bin/dermaga && systemctl --user restart dermaga.service"
```
The atomic `rename(2)` syscall updates the directory entry without mutating the open inode that the running daemon holds, and `systemctl --user restart` cleanly re-execs the new binary.

No installer re-run is needed for a new release: on start, the reconciler rewrites any dermaga-owned Traefik file that differs from what the new binary renders and restarts Traefik once (for example 0.7.0-rc.2 adds the internal `verify` entrypoint to `traefik.yml`). The listening sockets stay with systemd, so requests wait in the backlog during that restart instead of failing. The next start finds the files current and does not restart Traefik.

Traefik's log shows one warning per start, `Unable to use socket activation for entrypoint` with `"entryPointName":"verify"`. This is expected: `verify` (`:8099`, DEC-33) is internal and has no systemd socket, so Traefik binds it inside its container itself. Ports 80 and 443 stay socket-activated.

### 6.4 Operator Access & SSH Tunneling
The web panel listens exclusively on `127.0.0.1:7070` on the server (`SEC-2`). Access it securely from the workstation:
```bash
# Open an SSH port-forwarding tunnel
ssh -N -L 7070:127.0.0.1:7070 dermaga-vm

# In your browser, navigate to:
# http://127.0.0.1:7070
```

### 6.5 Recovery Commands
If operator credentials or 2FA state need to be reset via the server shell:
```bash
# Set or reset admin password (revokes all active sessions)
# On a TTY, prompts interactively with hidden input and confirmation:
ssh -t dermaga-vm "~/.local/bin/dermaga admin set-password"

# Or non-interactively via piped stdin:
echo "secret-pass" | ssh dermaga-vm "~/.local/bin/dermaga admin set-password"

# Disable TOTP two-factor authentication
ssh dermaga-vm "~/.local/bin/dermaga admin totp disable"

# Verify daemon status and view connection instructions
ssh dermaga-vm "~/.local/bin/dermaga panel"
```

### 6.6 M1 Teardown
To completely tear down the M1 installation and remove all runtime state on `dermaga-vm`:
```bash
ssh dermaga-vm "systemctl --user stop dermaga.service && systemctl --user disable dermaga.service && rm -f ~/.config/systemd/user/dermaga.service && systemctl --user daemon-reload && rm -rf ~/.local/share/dermaga ~/.local/bin/dermaga"
```

### 6.7 Favicon and Brand Asset Generation (Development)
The web panel assets are generated directly from the canonical SVG logo (`ui/static/logo.svg`):
```bash
# Generate multi-size transparent PNG favicons and scalable SVG favicon
magick -background none ui/static/logo.svg -resize 32x32 -gravity center -extent 32x32 ui/static/favicon.png
magick -background none ui/static/logo.svg -resize 16x16 -gravity center -extent 16x16 ui/static/favicon-16x16.png
cp ui/static/logo.svg ui/static/favicon.svg
```



---

## 7. Milestone M3: Installing on the `fresh` Snapshot

From M3 on, the VM is set up by the installer itself, from the `fresh` snapshot.

1. Revert to `fresh` (ask the owner first), then make reboots restart the VM:
   ```bash
   virsh --connect qemu:///system snapshot-revert dermaga-vm fresh
   virt-xml --connect qemu:///system dermaga-vm --edit --events on_reboot=restart
   virsh --connect qemu:///system start dermaga-vm
   ```
   The snapshots were taken while running, so the revert restores a **running** domain whose live
   definition still has `on_reboot=destroy`; `virt-xml` changes only the persistent one
   (`virsh dumpxml --inactive`). A cold start applies it: `virsh shutdown dermaga-vm`, wait for
   `shut off`, `virsh start dermaga-vm`, then `virsh dumpxml dermaga-vm | grep on_reboot` shows
   `restart`. (Found 2026-11-26, `tmp/evidence/M7/logs/v062-vm-revert.log`.)
2. A release with `install.sh`, both binaries and `checksums.txt` must exist: the owner pushes a tag
   (`v0.3.0-rc.2` for M3b; `v0.3.0-rc` has no release) and `.github/workflows/release.yml`
   publishes it. CI runs the same `make release` on every push, so the tag only re-runs known steps.
   To rehearse the release job locally as root: `test/ci/simulate_release.sh .github/workflows/release.yml HEAD <tag>`.
3. Run the installer interactively as `debian` (it asks on the terminal):
   ```bash
   ssh -t dermaga-vm-init 'curl -fsSL https://github.com/r3p-dev/dermaga/releases/download/v0.3.0/install.sh | sudo bash'
   ```
   Enter accepts every default. When it waits, log in from a second terminal:
   `ssh -p 4222 -i ~/.ssh/dermaga-vm server@192.168.122.217`. Afterwards `ssh dermaga-vm` works and
   `dermaga-vm-init` (port 22, user `debian`) is refused.
4. Checks from the workstation: `test/vm/port_scan.sh` (during the install), `test/vm/m3_check.sh
   access|state|running|control 192.168.122.217 4222 ~/.ssh/dermaga-vm`.
5. **Local ACME (Pebble) for TLS tests:** run `./test/vm/setup_pebble.sh dermaga-vm` to start Pebble on network `web`, export `pebble-minica.pem` and `pebble-api-ca.pem`, point Traefik's `le` resolver to `https://pebble:14000/dir`, and restart `traefik.service` It also removes dermaga's marker from `traefik.yml`, so a daemon restart cannot re-render the file back to Let's Encrypt; until the VM is reverted, the reconciler reports `traefik.yml` as foreign and no longer starts Traefik itself (systemd keeps it running).

**Recovery if SSH is lost:** the old login stays open until the new one is proven (`LOCK-1`–`LOCK-5`).
If the VM still cannot be reached, use the libvirt console (`virsh --connect qemu:///system console
dermaga-vm` or virt-manager); `debian` has no password in the cloud image, so the practical recovery
is reverting to `fresh` (owner's approval).

**Teardown:** revert to `fresh` or `vps-setup` (owner's approval). On the workstation, remove the block
the laptop command added (between `# dermaga laptop access:` and `# end of dermaga laptop access`,
plus the `Host *` line after it) or restore `~/.ssh/config.dermaga-backup`.
