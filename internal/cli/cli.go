// Package cli is dermaga's command-line entry: a small subcommand router on
// the standard flag package (BRIEF §3.11). Commands other than daemon,
// hook-relay and probe talk to the daemon over its Unix socket.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/r3p-dev/dermaga/internal/api"
	"github.com/r3p-dev/dermaga/internal/audit"
	"github.com/r3p-dev/dermaga/internal/daemon"
	"github.com/r3p-dev/dermaga/internal/name"
	"github.com/r3p-dev/dermaga/internal/probe"
	"github.com/r3p-dev/dermaga/internal/relay"
)

// errQuiet: the command already printed why it failed; exit 1 without more output.
var errQuiet = errors.New("")

type command struct {
	run   func(args []string) error
	usage string
}

func commands() map[string]command {
	return map[string]command{
		"daemon":         {func([]string) error { return daemon.Run() }, "run the daemon (systemd user service)"},
		"status":         {cmdStatus, "show the daemon's status [--json]"},
		"audit":          {cmdAudit, "show the audit log [--limit N] [--json]"},
		"admin":          {cmdAdmin, "set-password | totp enable | totp disable"},
		"panel":          {cmdPanel, "print the SSH tunnel snippet and the panel URL [--host H]"},
		"doctor":         {cmdDoctor, "check the host and the daemon [--json]"},
		"apps":           {cmdApps, "list | show | create | edit | remove | webhook"},
		"db":             {cmdDB, "list | show | create | link | unlink | shell | proxy | backup | restore"},
		"backup":         {cmdBackup, "list | snapshots | run <target> | system | check | settings"},
		"restore-system": {cmdRestoreSystem, "restore-system [--snapshot ID] [-y|--yes]"},
		"keys":           {cmdKeys, "export: export the master secret encryption key"},
		"deploy":         {cmdDeploy, "deploy <app> [--ref SHA] [--no-follow]"},
		"rollback":       {cmdRollback, "rollback <app> [--to RELEASE] [--no-follow]"},
		"logs":           {cmdLogs, "logs <app> [-f] [-n N] [--build latest|RELEASE]"},
		"env":            {cmdEnv, "env <app> [list [--reveal] | set K=V… | unset K… | edit]"},
		"domains":        {cmdDomains, "domains <app> [add HOST [--redirect] | remove HOST | primary HOST]"},
		"middleware":     {cmdMiddleware, "middleware <app> [show | basic-auth add|remove USER]"},
		"github":         {cmdGitHub, "known-hosts: pin GitHub's SSH host keys"},
		"settings":       {cmdSettings, "show or change settings [--hook-host H]"},
		"traefik":        {cmdTraefik, "static: print traefik.yml [--acme-email E] [--acme-staging]"},
		"self-update":    {cmdSelfUpdate, "check for or apply updates [--check] [--version V] [--force]"},
		"version":        {func([]string) error { fmt.Println(name.Name, name.Version()); return nil }, "print the version"},
		"hook-relay":     {cmdHookRelay, "TCP → hooks.sock relay (runs inside the dg-hooks container)"},
		"probe":          {cmdProbe, "HTTP health probe (runs inside the rootless netns)"},
		"db-bridge":      {cmdDBBridge, "bridge unix socket to container TCP (runs inside rootless netns)"},
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, "usage: %s <command> [flags]\n\ncommands:\n", name.Name)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, n := range []string{"status", "apps", "db", "backup", "restore-system", "keys", "deploy", "rollback", "logs", "env", "domains", "middleware", "settings", "github",
		"audit", "admin", "panel", "doctor", "traefik", "self-update", "daemon", "version", "hook-relay", "probe"} {
		fmt.Fprintf(tw, "  %s\t%s\n", n, commands()[n].usage)
	}
	tw.Flush()
}

func Main(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		usage(os.Stdout)
		return 0
	}
	c, ok := commands()[args[0]]
	if !ok {
		fmt.Fprintf(os.Stderr, "%s: unknown command %q\n\n", name.Name, args[0])
		usage(os.Stderr)
		return 2
	}
	err := c.run(args[1:])
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errQuiet):
		return 1
	default:
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", name.Name, args[0], err)
		return 1
	}
}

// parse parses flags; the flag package has already printed any error.
func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errQuiet
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return errQuiet
	}
	return nil
}

func printJSON(v any) error {
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	return e.Encode(v)
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parse(fs, args); err != nil {
		return err
	}
	var st api.Status
	if err := call("GET", "/api/status", nil, &st); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(st)
	}
	fmt.Printf("%s %s (%s), pid %d, up %s, RSS %.1f MB\nadmin password: %s, TOTP: %s\n",
		st.Name, st.Version, st.GoVersion, st.PID, time.Since(st.StartedAt).Round(time.Second), float64(st.RSSKB)/1024,
		yesNo(st.AdminPasswordSet, "set", "NOT SET (dermaga admin set-password)"), yesNo(st.TOTPEnabled, "on", "off"))
	return nil
}

func yesNo(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

func cmdAudit(args []string) error {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	limit := fs.Int("limit", 50, "number of entries (1..1000)")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parse(fs, args); err != nil {
		return err
	}
	var entries []audit.Entry
	if err := call("GET", fmt.Sprintf("/api/audit?limit=%d", *limit), nil, &entries); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(entries)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tACTOR\tACTION\tTARGET\tDETAILS")
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.Time.Format(time.DateTime), e.Actor, e.Action, e.Target, e.Details)
	}
	return tw.Flush()
}

func cmdHookRelay(args []string) error {
	fs := flag.NewFlagSet("hook-relay", flag.ContinueOnError)
	listen := fs.String("listen", ":8080", "TCP address to accept on")
	target := fs.String("target", "/run/"+name.Name+"/hooks.sock", "Unix socket to forward to")
	if err := parse(fs, args); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	return relay.Serve(ln, *target)
}

func cmdProbe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	var o probe.Options
	fs.StringVar(&o.URL, "url", "", "health URL, e.g. http://dg-app-1234abcd:8080/health")
	fs.StringVar(&o.DNS, "dns", "", "DNS server for container names (the network gateway), e.g. 10.89.0.1:53")
	fs.IntVar(&o.Successes, "successes", 3, "consecutive successes required")
	fs.DurationVar(&o.Interval, "interval", time.Second, "time between attempts")
	fs.StringVar(&o.Host, "host", "", "Host header to send (verification through Traefik)")
	fs.StringVar(&o.Header, "header", "", `one extra request header, "Name: value"`)
	fs.BoolVar(&o.AnyStatus, "any-status", false, "any answer below 500 counts, except Traefik's no-router 404")
	fs.DurationVar(&o.Timeout, "timeout", 120*time.Second, "overall deadline")
	if err := parse(fs, args); err != nil {
		return err
	}
	return probe.Run(context.Background(), o, os.Stdout)
}
