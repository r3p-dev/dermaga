# Development & Test Environment Setup

This document describes the environment setup for developing and testing **Dermaga**, including the development workstation, the test virtual machine (`dermaga-vm`), libvirt snapshots, the test GitHub App, webhook forwarding via smee/gosmee, and teardown procedures.

---

## 1. Development Workstation Setup

Dermaga's build and development workflow requires:

- **Operating System:** Linux (Debian, Fedora, Ubuntu, or Arch) with systemd and KVM/libvirt virtualization support.
- **Go Toolchain:** Go 1.24+ (standard library first, single static binary compiled with `CGO_ENABLED=0`).
- **JavaScript Toolchain:**
  - **Bun:** `bun` v1.4+ (used for building the SvelteKit 3 UI and producing `bun.lock`).
  - **Node.js:** Node `v22+` / `v24` (LTS) for auxiliary tools and language servers.
- **System Utilities:** `make`, `git`, `curl`, `jq`, `libvirt-clients` (`virsh`), `virt-install`.
- **SSH Configuration:** Configure `~/.ssh/config` for convenient access to the test VM:
  ```sshconfig
  Host dermaga-vm
      HostName 192.168.122.217
      User server
      Port 4222
      IdentityFile ~/.ssh/id_ed25519
      StrictHostKeyChecking accept-new
  ```

---

## 2. Test VM (`dermaga-vm`) & Libvirt Snapshots

### 2.1 Virtual Machine Topology
- **VM Name:** `dermaga-vm`
- **Guest OS:** Debian GNU/Linux 13 (Trixie), minimal cloud image (`amd64`).
- **Primary User:** `server` (UID 1001, member of `sudo` group).
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

---

## 3. Safe Reboot Procedure (ACPI Poweroff vs Guest Reboot)

### 3.1 The `virsh reboot` Pitfall
On `dermaga-vm`, `qemu-guest-agent` is **not installed**.
When `virsh reboot dermaga-vm` is executed without a guest agent, libvirt falls back to ACPI signal injection. Because standard x86 ACPI lacks a dedicated hardware reboot button, QEMU asserts the ACPI power button (`system_powerdown`).
On the Debian guest, `systemd-logind` intercepts this as `Power key pressed short.` and activates `poweroff.target`. When the guest shuts down via poweroff, QEMU executes its `<on_poweroff>destroy</on_poweroff>` policy, leaving the VM permanently in the `shut off` state rather than restarting.

### 3.2 Recommended Reboot Method
To reboot `dermaga-vm` cleanly and allow QEMU to execute `<on_reboot>restart</on_reboot>`:
1. **Via SSH (Recommended):**
   ```bash
   ssh dermaga-vm "sudo -n reboot"
   ```
   *Mechanism:* Systemd activates `reboot.target`. The guest Linux kernel calls `kernel_restart()`, signalling a true reboot event to QEMU. QEMU detects the reboot and restarts the VM automatically in ~4 seconds.
   *(Rule: automated agents must ask the project owner first and receive explicit permission before executing any command requiring sudo, including guest reboots).*
2. **Alternative (Guest Agent):**
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
1. Create a channel on [smee.io](https://smee.io) (e.g. `https://smee.io/dermaga-test-channel`).
2. Run `gosmee` on the development workstation to forward deliveries to the panel:
   ```bash
   gosmee client https://smee.io/dermaga-test-channel http://localhost:7070/hooks/github-app
   ```
3. **Security Warning:** Smee channels are public and unauthenticated. **Never** point production webhooks or sensitive repositories at a public smee channel. Use smee strictly with throwaway test repositories.

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
ssh dermaga-vm "~/.local/bin/dermaga admin set-password --password <secret>"

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

