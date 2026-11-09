package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/r3p-dev/dermaga/internal/api"
	"github.com/r3p-dev/dermaga/internal/config"
	"github.com/r3p-dev/dermaga/internal/doctor"
	"github.com/r3p-dev/dermaga/internal/name"
)

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parse(fs, args); err != nil {
		return err
	}
	checks := doctorChecks()
	if *asJSON {
		printJSON(checks)
	} else {
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, c := range checks {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", strings.ToUpper(c.Status), c.Name, c.Detail)
		}
		tw.Flush()
	}
	for _, c := range checks {
		if c.Status == "fail" {
			return errQuiet
		}
	}
	return nil
}

func doctorChecks() []doctor.Check {
	cs := doctor.RunHostChecks()
	daemon := checkDaemon()
	if len(cs) >= 3 {
		all := make([]doctor.Check, 0, len(cs)+len(daemon))
		all = append(all, cs[:3]...)
		all = append(all, daemon...)
		all = append(all, cs[3:]...)
		return all
	}
	return append(cs, daemon...)
}

func checkDaemon() []doctor.Check {
	p, err := config.DefaultPaths()
	if err != nil {
		return []doctor.Check{{Name: "daemon", Status: "fail", Detail: err.Error()}}
	}
	fi, err := os.Stat(p.Socket())
	if err != nil {
		return []doctor.Check{{Name: "daemon", Status: "fail", Detail: fmt.Sprintf("no socket at %s: systemctl --user status %s", p.Socket(), name.Name)}}
	}
	if fi.Mode().Perm() != 0o600 {
		return []doctor.Check{{Name: "daemon", Status: "fail", Detail: fmt.Sprintf("%s has mode %v, want 0600", p.Socket(), fi.Mode().Perm())}}
	}
	var st api.Status
	if err := call("GET", "/api/status", nil, &st); err != nil {
		return []doctor.Check{{Name: "daemon", Status: "fail", Detail: err.Error()}}
	}
	cs := []doctor.Check{{Name: "daemon", Status: "ok", Detail: fmt.Sprintf("%s, pid %d, RSS %.1f MB, socket %s (0600)", st.Version, st.PID, float64(st.RSSKB)/1024, p.Socket())}}
	if st.AdminPasswordSet {
		cs = append(cs, doctor.Check{Name: "admin password", Status: "ok", Detail: "set"})
	} else {
		cs = append(cs, doctor.Check{Name: "admin password", Status: "warn", Detail: "not set: dermaga admin set-password"})
	}
	if st.TOTPEnabled {
		cs = append(cs, doctor.Check{Name: "totp", Status: "ok", Detail: "enabled"})
	} else {
		cs = append(cs, doctor.Check{Name: "totp", Status: "warn", Detail: "off: dermaga admin totp enable (strongly recommended)"})
	}
	return cs
}
