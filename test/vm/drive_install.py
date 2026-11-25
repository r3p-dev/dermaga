#!/usr/bin/env python3
"""Runs the published installer in a real terminal over `ssh -tt` and types the answers a user
would type, so M3 scenarios run exactly as documented (curl ... | sudo bash).

Everything the terminal shows goes to --log. What the driver types is logged as a note, except
passwords, which are read from --password-file and never written anywhere.

  test/vm/drive_install.py --ssh dermaga-vm-init --url URL --log LOG --password-file FILE \
      [--key-answer ''|n] [--paste-file FILE] [--at-wait login|interrupt|hangup] [--login-args '...']

--at-wait: what happens when the installer asks for a login from a new terminal:
  login      run the printed `ssh -p PORT server@ADDR` (plus --login-args) as a second process
  interrupt  run it (expected to fail), then press Ctrl+C in the installer's terminal
  hangup     close the installer's terminal (SIGHUP to its ssh client), like closing the window
"""
import argparse, os, pty, re, select, signal, socket, subprocess, sys, time
from datetime import datetime

ANSI = re.compile(rb"\x1b\[[0-9;]*m")
PROMPTS = [
    ("key-choice", rb"Which key logs in as server\? \(number, or p to paste another\) \[\d+\]: $"),
    ("key-confirm", rb"Use this key to log in as server\? \[Y/n\]: $"),
    ("paste", rb"Public key: $"),
    ("port", rb"SSH port \[\d+\]: $"),
    ("address", rb"Public IP address of this server \[[^\]]*\]: $"),
    ("sudo-choice", rb"Protect sudo with a password \(recommended\)\? \[Y/n\]: $"),
    ("password", rb"(New password|Retype new password|\[sudo\] password for server): $"),
    ("plan", rb"Continue\? \[Y/n\]: $"),
]
WAIT = re.compile(rb"Open a NEW terminal on your laptop and run:\s+(ssh -p (\d+) server@(\S+))")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--ssh", required=True)
    ap.add_argument("--url", required=True)
    ap.add_argument("--log", required=True)
    ap.add_argument("--password-file", required=True)
    ap.add_argument("--key-answer", default="")
    ap.add_argument("--paste-file")
    ap.add_argument("--at-wait", choices=["login", "interrupt", "hangup"], default="login")
    ap.add_argument("--login-args", default="")
    ap.add_argument("--timeout", type=int, default=1800)
    # Shell commands run in the same session first (scenario b removes the provider key there,
    # because no later login as that user would work).
    ap.add_argument("--prefix", default="")
    a = ap.parse_args()

    password = open(a.password_file).read().strip()
    pastes = [l.rstrip("\n") for l in open(a.paste_file)] if a.paste_file else []
    log = open(a.log, "ab")

    def note(text):
        log.write(f"\n[driver {datetime.now().isoformat(timespec='seconds')}] {text}\n".encode())
        log.flush()

    head = subprocess.run(["git", "rev-parse", "--short", "HEAD"], capture_output=True, text=True).stdout.strip()
    note(f"date: {datetime.now().astimezone().isoformat(timespec='seconds')} hostname: {socket.gethostname()} commit: {head}")
    command = a.prefix + f"curl -fsSL {a.url} | sudo bash"
    note(f"$ ssh -tt {a.ssh} '{command}'")

    pid, fd = pty.fork()
    if pid == 0:
        os.execvp("ssh", ["ssh", "-tt", "-o", "ServerAliveInterval=15", a.ssh, command])

    def send(data, shown):
        os.write(fd, data)
        note(f"typed: {shown}")

    buf, seen_wait, deadline = b"", False, time.time() + a.timeout
    while time.time() < deadline:
        r, _, _ = select.select([fd], [], [], 1.0)
        if not r:
            continue
        try:
            chunk = os.read(fd, 4096)
        except OSError:
            break
        if not chunk:
            break
        log.write(chunk)
        log.flush()
        buf = (buf + ANSI.sub(b"", chunk))[-4096:]
        for name, pattern in PROMPTS:
            if re.search(pattern, buf):
                buf = b""
                if name == "password":
                    send(password.encode() + b"\r", "password (hidden)")
                elif name == "paste":
                    line = pastes.pop(0) if pastes else ""
                    send(line.encode() + b"\r", line or "Enter")
                elif name == "key-confirm" or name == "key-choice":
                    send(a.key_answer.encode() + b"\r", a.key_answer or "Enter")
                else:
                    send(b"\r", "Enter")
                break
        m = WAIT.search(buf)
        if m and not seen_wait:
            seen_wait = True
            printed = m.group(1).decode()
            note(f"the installer printed the login test: {printed}")
            if a.at_wait == "hangup":
                time.sleep(3)
                note("closing the installer's terminal: SIGHUP -> SIGTERM -> SIGKILL to its ssh client")
                os.close(fd)
                for sig in (signal.SIGHUP, signal.SIGTERM, signal.SIGKILL):
                    try:
                        os.kill(pid, sig)
                    except ProcessLookupError:
                        break
                    for _ in range(10):
                        wpid, status = os.waitpid(pid, os.WNOHANG)
                        if wpid == pid:
                            code = os.waitstatus_to_exitcode(status)
                            note(f"installer terminal closed: exit={code}")
                            sys.exit(code)
                        time.sleep(0.1)
                break
            words = printed.split()  # ssh -p PORT server@ADDR; extra options go before the destination
            login = words[:1] + a.login_args.split() + [
                "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new"] + words[1:] + ["echo new-login-ok"]
            note("new terminal: $ " + " ".join(login))
            res = subprocess.run(login, capture_output=True, text=True, timeout=60)
            note(f"new terminal output: {(res.stdout + res.stderr).strip()} (exit={res.returncode})")
            if a.at_wait == "interrupt":
                time.sleep(5)
                send(b"\x03", "Ctrl+C")
    if time.time() >= deadline:
        note(f"no end after {a.timeout} s: closing the installer's terminal")
        os.kill(pid, signal.SIGTERM)
    code = 0
    for _ in range(50):
        wpid, status = os.waitpid(pid, os.WNOHANG)
        if wpid == pid:
            code = os.waitstatus_to_exitcode(status)
            break
        time.sleep(0.1)
    else:
        os.kill(pid, signal.SIGKILL)
        _, status = os.waitpid(pid, 0)
        code = os.waitstatus_to_exitcode(status)
    note(f"installer terminal closed: exit={code}")
    sys.exit(code)


if __name__ == "__main__":
    main()
