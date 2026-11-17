# dermaga

**dermaga** (Indonesian for "pier") turns a fresh Debian 13 VPS into a hardened, zero-downtime host and lets you deploy web applications from GitHub through a private web panel without memorizing commands or editing configuration files.

## 1. Before You Start (`INST-1`)

You need:

- A new VPS with **Debian 13** (x86_64 or ARM, 2 GB RAM minimum).
- A domain where you can add DNS records.
- **Your laptop's SSH key, added when you create the VPS.** Most providers ask for it on the create
  page. That is how the installer knows it is you.

A VPS created with only a root password is fine too: the installer asks for your key.

### No SSH key yet?

The key has two files on your laptop: a private one that **never leaves the laptop** (keep a backup
of it, like a password) and a public one (`.pub`) that you give to servers.

**Linux and macOS** (Terminal):

1. Check whether you have one: `cat ~/.ssh/id_ed25519.pub`
2. If that says "No such file", create one: `ssh-keygen -t ed25519` and press Enter at each question.
3. Show it: `cat ~/.ssh/id_ed25519.pub`. Copy the whole line (it starts with `ssh-ed25519`) and paste
   it into your provider's SSH key field.

**Windows 10 and newer** (PowerShell or Windows Terminal):

1. Check: `Get-Content $env:USERPROFILE\.ssh\id_ed25519.pub`
2. If it is missing: `ssh-keygen -t ed25519`, Enter at each question.
3. Show it again with the command from step 1, and paste the line at your provider.

## 2. Install

Log in to the server the way your provider says (for example `ssh debian@<server-ip>` or
`ssh root@<server-ip>`), then run the command from the release you want to install:

```bash
curl -fsSL https://github.com/r3p-dev/dermaga/releases/download/<tag>/install.sh | sudo bash
```

It asks only what has no safe default (your key, the SSH port, the public IP when it cannot see
it), shows a plan, hardens the server and installs everything. Before it closes the old way in, it
asks you to log in from a **new** terminal on the new port, and waits until that works.

At the end it prints **one command to paste on your laptop**. From then on:

```bash
ssh dermaga
```

opens a shell on the server and the panel at **http://localhost:7070**. The panel listens on the
server's `127.0.0.1:7070` only and is never reachable from the internet (`SEC-2`).

## 3. Server Commands (`SRV-1`)

Besides the initial installer, the owner only ever needs three commands on the server:

- `dermaga panel`: Prints the laptop command again.
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
