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
		fmt.Fprintf(os.Stdout, "usage: %s backup [list | snapshots [--tag T] | run <target> | system | check [--read-data] | settings [--json] | settings set … | password <init|reveal|confirm> | failure-notify <unit>]\n", name.Name)
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
	case "password":
		return backupPassword(args[1:])
	case "failure-notify":
		return backupFailureNotify(args[1:])
	}
	fmt.Fprintf(os.Stderr, "usage: %s backup [list | snapshots [--tag T] | run <target> | system | check [--read-data] | settings [--json] | settings set … | password <init|reveal|confirm> | failure-notify <unit>]\n", name.Name)
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

// backupRunRequest parses `backup run [--scheduled] <target>`. Only the flag
// marks a run as scheduled; dermaga's timer units pass it. The environment
// (INVOCATION_ID) says nothing: every child of any systemd service inherits it.
func backupRunRequest(args []string) (api.RunBackupRequest, error) {
	fs := flag.NewFlagSet("backup run", flag.ContinueOnError)
	scheduled := fs.Bool("scheduled", false, "for dermaga's timer units: a failure is reported by the unit's OnFailure=, not by this run")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return api.RunBackupRequest{}, err
		}
		return api.RunBackupRequest{}, errQuiet
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s backup run <db:NAME | system | volume:APP:VOL>\n", name.Name)
		return api.RunBackupRequest{}, errQuiet
	}
	return api.RunBackupRequest{Target: fs.Arg(0), Scheduled: *scheduled}, nil
}

func backupRun(args []string) error {
	req, err := backupRunRequest(args)
	if err != nil {
		return err
	}
	target := req.Target
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

	var res api.CheckBackupResponse
	fmt.Println("Running restic repository integrity check...")
	if err := callTimeout("POST", "/api/backup/check", 5*time.Minute, req, &res); err != nil {
		return err
	}
	fmt.Print(res.Output) // restic's own report, not a summary of ours
	return nil
}

func backupSettings(args []string) error {
	if len(args) > 0 && args[0] == "set" {
		return backupSettingsSet(args[1:])
	}
	fs := flag.NewFlagSet("backup settings", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parse(fs, args); err != nil {
		return err
	}
	return showBackupSettings(*asJSON)
}

func showBackupSettings(asJSON bool) error {
	var st backup.Settings
	if err := call("GET", "/api/backup/settings", nil, &st); err != nil {
		return err
	}
	if asJSON {
		return printJSON(st)
	}

	fmt.Printf("Repository: %s\n", st.Repository)
	if st.S3Bucket != "" {
		fmt.Printf("S3 Endpoint: %s\n", st.S3Endpoint)
		fmt.Printf("S3 Bucket:   %s\n", st.S3Bucket)
		fmt.Printf("S3 Region:   %s\n", st.S3Region)
		fmt.Printf("S3 Access:   %s\n", st.S3AccessKey)
		fmt.Printf("S3 Secret:   %s\n", yesNo(st.S3SecretKey != "", "set", "(not set)"))
	}
	switch st.PasswordState {
	case backup.PasswordConfirmed:
		fmt.Println("Restic Pass: set, offline copy confirmed (dermaga backup password reveal)")
	case backup.PasswordUnconfirmed:
		fmt.Println("Restic Pass: set, NOT confirmed as stored offline (dermaga backup password reveal)")
	default:
		fmt.Println("Restic Pass: not set, backups fail (dermaga backup password init)")
	}
	return nil
}

const backupPasswordUsage = "backup password <init | reveal | confirm>"

// backupPassword manages the restic password: generated once and shown once
// (init), shown again on an explicit, audited request (reveal), and marked as
// stored offline (confirm).
func backupPassword(args []string) error {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s %s\n", name.Name, backupPasswordUsage)
		return errQuiet
	}
	var res api.BackupPasswordResponse
	switch args[0] {
	case "init":
		if err := call("POST", "/api/backup/password", nil, &res); err != nil {
			return err
		}
	case "reveal":
		if v, err := readLine("This prints the restic repository password. Type 'reveal' to continue: "); err != nil || v != "reveal" {
			return errors.New("not revealed")
		}
		if err := call("POST", "/api/backup/password/reveal", nil, &res); err != nil {
			return err
		}
	case "confirm":
		if err := call("POST", "/api/backup/password/confirm", nil, nil); err != nil {
			return err
		}
		fmt.Println("Recorded: the restic password is stored offline.")
		return nil
	default:
		fmt.Fprintf(os.Stderr, "usage: %s %s\n", name.Name, backupPasswordUsage)
		return errQuiet
	}
	fmt.Printf("Restic password: %s\n\n%s\n\n", res.Password, res.RecoveryStatement)
	if res.PasswordState == backup.PasswordConfirmed {
		return nil
	}
	if v, _ := readLine("Type 'stored' once you have saved it offline: "); v != "stored" {
		fmt.Printf("Not confirmed. %s doctor keeps warning until you run: %s backup password confirm\n", name.Name, name.Name)
		return nil
	}
	if err := call("POST", "/api/backup/password/confirm", nil, nil); err != nil {
		return err
	}
	fmt.Println("Recorded: the restic password is stored offline.")
	return nil
}

const backupSettingsSetUsage = "backup settings set [--repo R] [--s3-endpoint E] [--s3-bucket B] [--s3-region R] [--s3-access-key K] [--s3-secret-key] [--json]"

func backupSettingsSet(args []string) error {
	fs := flag.NewFlagSet("backup settings set", flag.ContinueOnError)
	var in backup.Settings
	fs.StringVar(&in.Repository, "repo", "", "restic repository: a local path or s3:https://host/bucket (derived from the S3 flags when omitted)")
	fs.StringVar(&in.S3Endpoint, "s3-endpoint", "", "S3 endpoint host, e.g. s3.eu-central-003.backblazeb2.com")
	fs.StringVar(&in.S3Bucket, "s3-bucket", "", "S3 bucket name")
	fs.StringVar(&in.S3Region, "s3-region", "", "S3 region")
	fs.StringVar(&in.S3AccessKey, "s3-access-key", "", "S3 access key ID (not secret)")
	secret := fs.Bool("s3-secret-key", false, "set the S3 secret access key: prompted without echo, or one line from stdin; it is never a flag value")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := parse(fs, args); err != nil {
		return err
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	delete(given, "json")
	if len(given) == 0 || fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "usage: %s %s\n", name.Name, backupSettingsSetUsage)
		return errQuiet
	}

	var cur backup.Settings
	if err := call("GET", "/api/backup/settings", nil, &cur); err != nil {
		return err
	}
	req := mergeBackupSettings(cur, in, given)
	if *secret {
		v, err := readSecret("S3 secret access key: ")
		if err != nil {
			return err
		}
		if v == "" {
			return errors.New("empty S3 secret access key")
		}
		req.S3SecretKey = v
	}
	if err := call("PUT", "/api/backup/settings", req, nil); err != nil {
		return err
	}
	if !*asJSON {
		fmt.Println("✓ backup settings updated")
	}
	return showBackupSettings(*asJSON)
}

// mergeBackupSettings applies the given flags to the current settings. The
// S3 secret is never echoed back: an empty secret key keeps the stored one
// (the GET masks the key, and resending the mask would store it).
// Changing the S3 endpoint or bucket without --repo lets the server derive
// the repository from them again.
func mergeBackupSettings(cur, in backup.Settings, given map[string]bool) backup.Settings {
	req := cur
	req.S3SecretKey = ""
	if given["repo"] {
		req.Repository = in.Repository
	} else if given["s3-endpoint"] || given["s3-bucket"] {
		req.Repository = ""
	}
	if given["s3-endpoint"] {
		req.S3Endpoint = in.S3Endpoint
	}
	if given["s3-bucket"] {
		req.S3Bucket = in.S3Bucket
	}
	if given["s3-region"] {
		req.S3Region = in.S3Region
	}
	if given["s3-access-key"] {
		req.S3AccessKey = in.S3AccessKey
	}
	return req
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
