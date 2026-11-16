# dermaga

**dermaga** (Indonesian for "pier") turns a fresh Debian 13 VPS into a hardened, zero-downtime host and lets you deploy web applications from GitHub through a private web panel without memorizing commands or editing configuration files.

## 1. Before You Start (`INST-1`)

To use dermaga, you need:
- A server or VPS running a fresh installation of **Debian 13 (Trixie)** (x86_64 or arm64, 2 GB RAM minimum).
- A domain name where you can configure DNS records.
- **Add your laptop's SSH public key** when creating the VPS. Most VPS providers prompt for an SSH public key during instance creation.

If you don't have an SSH key pair yet:
- **Linux & macOS:** Run `ssh-keygen -t ed25519 -C "laptop"`, press Enter for defaults, and view your public key with `cat ~/.ssh/id_ed25519.pub`.
- **Windows (PowerShell / Windows Terminal):** Run `ssh-keygen -t ed25519`, then `Get-Content $env:USERPROFILE\.ssh\id_ed25519.pub`.

Your private key (`id_ed25519`) must never leave your laptop.

## 2. Accessing the Panel

The panel is never exposed directly to the public internet (`SEC-2`). It binds strictly to `127.0.0.1:7070` on the server and is accessed over an authenticated SSH tunnel:

```bash
# Connect and forward port 7070:
ssh -N -L 7070:127.0.0.1:7070 server@<your-server-ip> -p 4222
```

Then open `http://localhost:7070` in your web browser.

## 3. Server Commands (`SRV-1`)

Besides the initial installer, the owner only ever needs three commands on the server:

- `dermaga panel`: Prints the laptop SSH configuration snippet and a first-run setup link.
- `dermaga admin set-password`: Interactively resets or sets the panel administrator password.
- `dermaga admin totp disable`: Emergency recovery command to disable TOTP two-factor authentication.

There is no other user-facing CLI. All normal administration happens in the web panel (`UI-1`).

## 4. Development

### Prerequisites
- Go 1.24+
- Bun 1.4+
- Node.js 22+

### Building and Testing
```bash
# Check formatting, linting, tests, and vulnerabilities:
make check

# Build static binary:
make build
```

## License

Apache-2.0. See [LICENSE](LICENSE).
