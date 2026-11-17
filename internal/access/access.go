// Package access builds the one-line laptop command (LAP-1) that adds a marked "Host dermaga"
// block to the laptop's ~/.ssh/config, so that `ssh dermaga` opens a shell and the panel tunnel.
package access

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// Settings keys the installer stores (INST-6) and the panel commands read.
const (
	SettingAddress        = "ssh_address"
	SettingPort           = "ssh_port"
	SettingKeyFingerprint = "ssh_login_key_fingerprint"
)

// PanelURL is where the panel is reached on the laptop through the tunnel.
const PanelURL = "http://localhost:7070"

var (
	hostnameRegex    = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	shortHostRegex   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	fingerprintRegex = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)
)

// Target is the server the laptop connects to.
type Target struct {
	Address        string // public IP or DNS name of the server
	Port           int    // SSH port
	ServerHostname string // short hostname, used for the dermaga-<hostname> alias
	KeyFingerprint string // SHA256 fingerprint of the login key; empty disables identity matching
}

// Validate rejects anything that is not a plain address, port, hostname or fingerprint. Only
// these validated values are placed into the shell command, so it never needs escaping.
func (t Target) Validate() error {
	if net.ParseIP(t.Address) == nil && !hostnameRegex.MatchString(t.Address) {
		return fmt.Errorf("invalid server address %q", t.Address)
	}
	if t.Port < 1 || t.Port > 65535 {
		return fmt.Errorf("invalid SSH port %d", t.Port)
	}
	if !shortHostRegex.MatchString(t.ServerHostname) {
		return fmt.Errorf("invalid server hostname %q", t.ServerHostname)
	}
	if t.KeyFingerprint != "" && !fingerprintRegex.MatchString(t.KeyFingerprint) {
		return fmt.Errorf("invalid key fingerprint %q", t.KeyFingerprint)
	}
	return nil
}

// TestLoginCommand is the command the installer asks the user to run from a new terminal (LOCK-2).
func (t Target) TestLoginCommand() string {
	return fmt.Sprintf("ssh -p %d server@%s", t.Port, t.Address)
}

// LaptopCommand returns the LAP-1 command. It is one line of `sh -c '...'` so it pastes the same
// way into bash, zsh and fish: the script contains no single quote, no "\\" and no "\'", the only
// sequences fish interprets inside single quotes. The script:
//   - creates ~/.ssh (0700) and ~/.ssh/config (0600) if needed;
//   - does nothing but print the alias when this server's marker is already there;
//   - uses the alias dermaga-<hostname> when a "Host dermaga" exists that it did not write;
//   - backs up an existing config once, to config.dermaga-backup;
//   - writes IdentityFile + IdentitiesOnly only for a ~/.ssh/*.pub whose fingerprint matches
//     the server's login key, and says so when none matches;
//   - puts the block first, followed by "Host *", because ssh uses the first value it finds and
//     options at the top of a config apply to every host, exactly as under "Host *".
func LaptopCommand(t Target) (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	port := strconv.Itoa(t.Port)
	marker := "# dermaga laptop access: server@" + t.Address + " port " + port + " as"
	lines := []string{
		"set -e",
		"fp=" + t.KeyFingerprint,
		"a=dermaga",
		`d="$HOME/.ssh"`,
		`c="$d/config"`,
		`m="` + marker + `"`,
		`mkdir -p "$d"`,
		`chmod 700 "$d"`,
		`[ -e "$c" ] || : > "$c"`,
		`chmod 600 "$c"`,
		`if grep -qF "$m " "$c"; then a=$(grep -F "$m " "$c" | head -n 1 | sed "s/^.* as //"); echo "Already saved. From now on: ssh $a, then open ` + PanelURL + `"; exit 0; fi`,
		`if grep -Eiq "^[[:space:]]*host([[:space:]]+[^[:space:]]+)*[[:space:]]+dermaga([[:space:]]|$)" "$c"; then a=dermaga-` + t.ServerHostname + `; fi`,
		`k=`,
		`if [ -n "$fp" ]; then for f in "$d"/*.pub; do [ -f "$f" ] || continue; if [ "$(ssh-keygen -lf "$f" 2>/dev/null | cut -d " " -f 2)" = "$fp" ]; then k="${f%.pub}"; break; fi; done; fi`,
		`b=`,
		`if [ -s "$c" ] && [ ! -e "$c.dermaga-backup" ]; then cp -p "$c" "$c.dermaga-backup"; b=1; fi`,
		`t="$c.dermaga-tmp"`,
		`{ printf "%s\n" "$m $a" "Host $a" "    HostName ` + t.Address + `" "    Port ` + port + `" "    User server" "    LocalForward 7070 127.0.0.1:7070" "    ServerAliveInterval 30" "    ServerAliveCountMax 3"; ` +
			`if [ -n "$k" ]; then printf "    IdentityFile \042%s\042\n    IdentitiesOnly yes\n" "$k"; fi; ` +
			`printf "%s\n" "# end of dermaga laptop access" ""; ` +
			`if [ -s "$c" ]; then printf "%s\n" "Host *" ""; cat "$c"; fi; } > "$t"`,
		// cat instead of mv keeps a symlinked ~/.ssh/config pointing where it did
		`cat "$t" > "$c"`,
		`rm -f "$t"`,
		`if [ -n "$b" ]; then echo "Your previous ssh config is saved as $c.dermaga-backup"; fi`,
		`if [ -n "$fp" ] && [ -z "$k" ]; then echo "No key in $d matches $fp, so no IdentityFile was set; ssh will try your default keys."; fi`,
		`echo "Saved. From now on: ssh $a, then open ` + PanelURL + `"`,
	}
	script := strings.Join(lines, "; ")
	if strings.Contains(script, "'") || strings.Contains(script, `\\`) {
		return "", fmt.Errorf("laptop command would not paste safely into every shell")
	}
	return "sh -c '" + script + "'", nil
}
