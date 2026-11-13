package cli

import (
	"flag"
	"fmt"
	"os"
	"os/user"

	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/host"
	"github.com/r3p-dev/dermaga/internal/name"
)

func cmdPanel(args []string) error {
	hostname, _ := os.Hostname()
	fs := flag.NewFlagSet("panel", flag.ContinueOnError)
	hostFlag := fs.String("host", hostname, "public name or IP your laptop uses to reach this server")
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
	fmt.Print(panelSnippet(*hostFlag, host.SSHPort(), u.Username, cfg.Panel.Port))
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
