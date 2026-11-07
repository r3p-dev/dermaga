package cli

import (
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/name"
)

func cmdPanel(args []string) error {
	host, _ := os.Hostname()
	fs := flag.NewFlagSet("panel", flag.ContinueOnError)
	hostFlag := fs.String("host", host, "public name or IP your laptop uses to reach this server")
	if err := parse(fs, args); err != nil {
		return err
	}
	p, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	cfg, err := config.Load(p.ConfigFile())
	if err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	files, _ := filepath.Glob("/etc/ssh/sshd_config.d/*.conf")
	fmt.Print(panelSnippet(*hostFlag, sshPort(append(files, "/etc/ssh/sshd_config")), u.Username, cfg.Panel.Port))
	return nil
}

func panelSnippet(host, sshPort, user string, port int) string {
	return fmt.Sprintf(`The panel listens on 127.0.0.1:%[4]d only. Reach it through an SSH tunnel.

Add this to ~/.ssh/config on your laptop (fix HostName if it isn't how you reach this server):

Host %[5]s
    HostName %[1]s
    Port %[2]s
    User %[3]s
    LocalForward %[4]d 127.0.0.1:%[4]d
    SessionType none
    ExitOnForwardFailure yes

Then run "ssh -f %[5]s" and open http://localhost:%[4]d
(use exactly this URL: the panel rejects any other Host, and the local port must stay %[4]d).

Phone: in an SSH app with port forwarding (e.g. Termius), add a local forward from port %[4]d
to 127.0.0.1:%[4]d on this host, connect, then open http://localhost:%[4]d in the phone's browser.
`, host, sshPort, user, port, name.Name)
}

// sshPort returns the first Port directive in files, in the order sshd reads
// them (Debian includes sshd_config.d/*.conf before its own settings).
func sshPort(files []string) string {
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			if fl := strings.Fields(l); len(fl) == 2 && strings.EqualFold(fl[0], "Port") {
				return fl[1]
			}
		}
	}
	return "22"
}
