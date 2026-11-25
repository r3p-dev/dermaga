#!/usr/bin/env bash
# M3 checks run from the laptop against dermaga-vm after the installer, as an outsider would see
# the server. No sudo: every fact comes from SSH logins, port probes and files user server can read.
#
#   test/vm/m3_check.sh access  HOST PORT KEY   LOCK-4, HOST-2, HOST-3, SEC-2 from outside
#   test/vm/m3_check.sh state   HOST PORT KEY   fingerprint of managed files and service PIDs (no-op check)
#   test/vm/m3_check.sh running HOST PORT KEY   daemon, Traefik and panel up (reboot check)
#   test/vm/m3_check.sh control HOST PORT KEY   negative control: the closed-port assertion must fail on PORT
set -Eeuo pipefail

cmd=${1:?command}
host=${2:?host}
port=${3:?ssh port}
key=${4:?private key}

echo "date: $(date -Is)"
echo "hostname: $(hostname)"
echo "commit: $(git rev-parse --short HEAD)"

ssh_opts=(-p "$port" -o BatchMode=yes -o ConnectTimeout=5 -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new)
as_server() { ssh "${ssh_opts[@]}" -i "$key" "server@$host" "$@"; }

failures=0
check() {
  local name=$1
  shift
  if "$@"; then echo "PASS  $name"; else
    echo "FAIL  $name"
    failures=$((failures + 1))
  fi
}

port_closed() { ! timeout 5 bash -c "</dev/tcp/$host/$1" 2>/dev/null; }

# denied USER [ssh options...]: the login is refused, and sshd offers only publickey.
denied() {
  local user=$1 out
  shift
  out=$(ssh -p "$port" -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=accept-new "$@" "$user@$host" true 2>&1) && return 1
  echo "      $out"
  [[ $out == *"Permission denied (publickey)"* ]]
}

case $cmd in
  access)
    check "server logs in with the key on port $port" as_server true
    check "panel answers on 127.0.0.1:7070 on the server" bash -c "[[ \$(ssh ${ssh_opts[*]} -i $key server@$host curl -s -o /dev/null -w '%{http_code}' --max-time 5 http://127.0.0.1:7070/) == 200 ]]"
    check "firewall rules open only ssh, 80, 443/tcp, 443/udp" bash -c "ssh ${ssh_opts[*]} -i $key server@$host cat /etc/nftables.conf | grep -E 'dport' | tee /dev/stderr | grep -qx '    tcp dport { $port } accept comment \"ssh\"'"
    check "old port 22 closed" port_closed 22
    check "panel port 7070 closed from outside (SEC-2)" port_closed 7070
    # Denied-login checks run last because 5 failed auth attempts trigger fail2ban (HOST-4).
    check "password login not offered to server" denied server -o PubkeyAuthentication=no -o PreferredAuthentications=password,keyboard-interactive
    check "root refused, even with the key" denied root -o IdentitiesOnly=yes -i "$key"
    check "provider user debian refused, even with the key" denied debian -o IdentitiesOnly=yes -i "$key"
    ;;
  state)
    # The script runs on the VM, so $u and $(…) must expand there, not here.
    # shellcheck disable=SC2016
    as_server 'sha256sum /etc/ssh/sshd_config.d/00-dermaga.conf /etc/nftables.conf /etc/fail2ban/jail.d/90-dermaga.local \
      /etc/sysctl.d/90-dermaga.conf ~/.ssh/authorized_keys ~/.local/bin/dermaga ~/.config/systemd/user/dermaga.service \
      ~/.config/containers/systemd/traefik.container ~/.config/systemd/user/traefik-http.socket \
      ~/.config/systemd/user/traefik-https.socket ~/.local/share/dermaga/traefik/traefik.yml
      for u in ssh nftables fail2ban; do echo "$u $(systemctl show -P MainPID $u) $(systemctl show -P ActiveEnterTimestamp $u)"; done
      for u in dermaga traefik; do echo "user/$u $(systemctl --user show -P MainPID $u) $(systemctl --user show -P ActiveEnterTimestamp $u)"; done'
    ;;
  running)
    check "dermaga, Traefik and its sockets active" as_server systemctl --user is-active dermaga.service traefik.service traefik-http.socket traefik-https.socket
    check "panel answers on 127.0.0.1:7070" bash -c "[[ \$(ssh ${ssh_opts[*]} -i $key server@$host curl -s -o /dev/null -w '%{http_code}' --max-time 5 http://127.0.0.1:7070/) == 200 ]]"
    check "Traefik answers on :80 from the laptop" bash -c "curl -s -o /dev/null --max-time 5 http://$host/"
    # The remote shell expands ~ to server's home on the VM.
    # shellcheck disable=SC2088
    check "dermaga doctor passes" as_server '~/.local/bin/dermaga doctor'
    ;;
  control)
    # A check that has never failed proves nothing: the closed-port assertion must fail on the
    # SSH port, which is open.
    if port_closed "$port"; then
      echo "FAIL  negative control: port_closed reported the open port $port as closed"
      exit 1
    fi
    echo "PASS  negative control: port_closed fails on the open port $port"
    exit 0
    ;;
  *)
    echo "unknown command $cmd" >&2
    exit 2
    ;;
esac

if ((failures)); then
  echo "$failures check(s) failed"
  exit 1
fi
echo "all checks passed"
