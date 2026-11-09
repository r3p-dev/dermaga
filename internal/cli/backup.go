package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/r3p-dev/dermaga/internal/api"
	"github.com/r3p-dev/dermaga/internal/backup"
	"github.com/r3p-dev/dermaga/internal/name"
)

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func cmdBackup(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintf(os.Stdout, "usage: %s backup [list | snapshots [--tag T] | run <target> | system | check [--read-data] | settings [--reveal-password] | failure-notify <unit>]\n", name.Name)
		return flag.ErrHelp
	}
	switch args[0] {
	case "list", "snapshots":
		return backupList(args[1:])
	case "run":
		return backupRun(args[1:])
	case "system":
		return backupSystem(args[1:])
	case "check":
		return backupCheck(args[1:])
	case "settings":
		return backupSettings(args[1:])
	case "failure-notify":
		return backupFailureNotify(args[1:])
	}
	fmt.Fprintf(os.Stderr, "usage: %s backup [list | snapshots [--tag T] | run <target> | system | check [--read-data] | settings [--reveal-password] | failure-notify <unit>]\n", name.Name)
	return errQuiet
}

func backupList(args []string) error {
	fs := flag.NewFlagSet("backup list", flag.ContinueOnError)
	tag := fs.String("tag", "", "filter snapshots by tag (e.g. db:shopdb, system)")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parse(fs, args); err != nil {
		return err
	}

	path := "/api/backup/snapshots"
	if *tag != "" {
		path += "?tag=" + *tag
	}

	var list []backup.Snapshot
	if err := call("GET", path, nil, &list); err != nil {
		return err
	}
	if *asJSON {
		return printJSON(list)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tDATE\tTAGS\tSIZE\tPATHS")
	for _, s := range list {
		id := s.ShortID
		if id == "" {
			id = s.ID
			if len(id) > 8 {
				id = id[:8]
			}
		}
		date := s.Time.Local().Format("2006-01-02 15:04:05")
		tags := strings.Join(s.Tags, ",")
		if tags == "" {
			tags = "-"
		}
		paths := strings.Join(s.Paths, ",")
		size := humanBytes(s.SizeBytes)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", id, date, tags, size, paths)
	}
	return tw.Flush()
}

func backupRun(args []string) error {
	target, _, err := sub("backup run", args)
	if err != nil {
		return err
	}
	req := api.RunBackupRequest{Target: target}
	var snap backup.Snapshot
	if err := callTimeout("POST", "/api/backup/run", 5*time.Minute, req, &snap); err != nil {
		return err
	}
	fmt.Printf("Backup completed for %s (snapshot %s, %s)\n", target, snap.ShortID, humanBytes(snap.SizeBytes))
	return nil
}

func backupSystem(args []string) error {
	var snap backup.Snapshot
	if err := callTimeout("POST", "/api/backup/system", 5*time.Minute, nil, &snap); err != nil {
		return err
	}
	fmt.Printf("System backup completed successfully (snapshot %s, %s)\n", snap.ShortID, humanBytes(snap.SizeBytes))
	return nil
}

func backupCheck(args []string) error {
	fs := flag.NewFlagSet("backup check", flag.ContinueOnError)
	readData := fs.Bool("read-data", false, "read a subset of repository data to verify integrity")
	subset := fs.String("subset", "10%", "percentage or size of data subset to read")
	if err := parse(fs, args); err != nil {
		return err
	}

	req := api.CheckBackupRequest{}
	if *readData {
		req.ReadDataSubset = *subset
	}

	var res map[string]string
	fmt.Println("Running restic repository integrity check...")
	if err := callTimeout("POST", "/api/backup/check", 5*time.Minute, req, &res); err != nil {
		return err
	}
	fmt.Println("Repository check passed: no errors found.")
	return nil
}

func backupSettings(args []string) error {
	fs := flag.NewFlagSet("backup settings", flag.ContinueOnError)
	reveal := fs.Bool("reveal-password", false, "reveal the restic repository password in cleartext")
	if err := parse(fs, args); err != nil {
		return err
	}

	var st backup.Settings
	if err := call("GET", "/api/backup/settings", nil, &st); err != nil {
		return err
	}

	fmt.Printf("Repository: %s\n", st.Repository)
	if st.S3Bucket != "" {
		fmt.Printf("S3 Endpoint: %s\n", st.S3Endpoint)
		fmt.Printf("S3 Bucket:   %s\n", st.S3Bucket)
		fmt.Printf("S3 Region:   %s\n", st.S3Region)
		fmt.Printf("S3 Access:   %s\n", st.S3AccessKey)
	}
	if *reveal && st.Password != "" {
		fmt.Printf("Restic Pass: %s\n", st.Password)
	} else if st.Password != "" {
		fmt.Println("Restic Pass: •••••••• (pass --reveal-password to show)")
	}
	return nil
}

func backupFailureNotify(args []string) error {
	unit := "unknown"
	if len(args) > 0 {
		unit = args[0]
	}
	req := api.FailureNotifyRequest{Unit: unit}
	var res map[string]string
	return call("POST", "/api/backup/failure-notify", req, &res)
}

func cmdRestoreSystem(args []string) error {
	fs := flag.NewFlagSet("restore-system", flag.ContinueOnError)
	snapshot := fs.String("snapshot", "", "snapshot ID to restore (defaults to latest)")
	yes := fs.Bool("yes", false, "skip interactive confirmation")
	fs.BoolVar(yes, "y", false, "skip interactive confirmation")
	if err := parse(fs, args); err != nil {
		return err
	}

	if !*yes {
		fmt.Fprintf(os.Stderr, "WARNING: This will restore and overwrite the Dermaga SQLite database,\nenvironment files, deploy keys, and Traefik configuration.\nType 'restore' to continue: ")
		var confirm string
		if _, err := fmt.Fscanln(os.Stdin, &confirm); err != nil || confirm != "restore" {
			fmt.Fprintln(os.Stderr, "restore cancelled")
			return errQuiet
		}
	}

	req := api.RestoreSystemRequest{
		SnapshotID: *snapshot,
		Confirm:    true,
	}
	var resp map[string]string
	if err := call("POST", "/api/backup/restore-system", req, &resp); err != nil {
		return err
	}
	fmt.Println("System restored successfully. Please restart dermaga daemon to apply the restored state.")
	return nil
}

func cmdKeys(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintf(os.Stdout, "usage: %s keys [export]\n", name.Name)
		return flag.ErrHelp
	}
	if args[0] != "export" {
		fmt.Fprintf(os.Stderr, "usage: %s keys export\n", name.Name)
		return errors.New("unknown keys subcommand: " + args[0])
	}
	var resp map[string]string
	if err := call("GET", "/api/keys/export", nil, &resp); err != nil {
		return err
	}
	fmt.Print(resp["instructions"])
	return nil
}
