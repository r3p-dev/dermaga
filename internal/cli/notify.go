package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/r3p-dev/dermaga/internal/api"
	"github.com/r3p-dev/dermaga/internal/name"
)

func cmdNotify(args []string) error {
	if len(args) == 0 {
		args = []string{"test"}
	}

	switch args[0] {
	case "test":
		return notifyTest(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "usage: %s notify test [--provider P] [--target T] [--token K]\n", name.Name)
		return errQuiet
	}
}

func notifyTest(args []string) error {
	fs := flag.NewFlagSet("notify test", flag.ContinueOnError)
	provider := fs.String("provider", "", "provider override (ntfy, telegram, discord)")
	target := fs.String("target", "", "target override (topic, chat_id, webhook URL)")
	token := fs.String("token", "", "token override")
	if err := parse(fs, args); err != nil {
		return err
	}

	req := api.TestNotificationRequest{
		Provider: *provider,
		Target:   *target,
		Token:    *token,
	}

	var res api.TestNotificationResponse
	if err := call("POST", "/api/notifications/test", req, &res); err != nil {
		return err
	}

	if !res.Success {
		return fmt.Errorf("notification test failed: %s", res.Detail)
	}

	fmt.Println("✓ " + res.Detail)
	return nil
}

func cmdConfig(args []string) error {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: %s config <subcommand>\n\navailable subcommands:\n  notifications  configure alert notification channels\n", name.Name)
		return errQuiet
	}

	switch args[0] {
	case "notifications", "notify":
		return configNotifications(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown config target: %s\nusage: %s config notifications\n", args[0], name.Name)
		return errQuiet
	}
}

func configNotifications(args []string) error {
	if len(args) == 0 {
		args = []string{"show"}
	}

	switch args[0] {
	case "show", "get":
		var view api.NotificationSettingsView
		if err := call("GET", "/api/settings/notifications", nil, &view); err != nil {
			return err
		}
		fmt.Printf("Notification Channel Settings:\n")
		fmt.Printf("  Provider:   %s\n", yesNo(view.Provider != "", view.Provider, "(disabled)"))
		fmt.Printf("  Target:     %s\n", yesNo(view.Target != "", view.Target, "(not set)"))
		fmt.Printf("  Configured: %s\n", yesNo(view.Configured, "yes", "no"))
		fmt.Printf("  Events:     %s\n", view.Events)
		return nil

	case "set":
		fs := flag.NewFlagSet("config notifications set", flag.ContinueOnError)
		provider := fs.String("provider", "", "provider: ntfy, telegram, discord, or '' to disable")
		target := fs.String("target", "", "target: topic URL/name, telegram chat_id, or discord webhook URL")
		token := fs.String("token", "", "token: ntfy auth token or telegram bot token (leave empty to keep current)")
		events := fs.String("events", "", "comma-separated event triggers: deploy.failed,backup.failed,disk.warning")
		if err := parse(fs, args[1:]); err != nil {
			return err
		}

		req := api.UpdateNotificationSettingsRequest{
			Provider: strings.TrimSpace(*provider),
			Target:   strings.TrimSpace(*target),
			Token:    strings.TrimSpace(*token),
			Events:   strings.TrimSpace(*events),
		}

		var res map[string]string
		if err := call("POST", "/api/settings/notifications", req, &res); err != nil {
			return err
		}
		fmt.Println("✓ notification settings updated")
		return nil

	default:
		fmt.Fprintf(os.Stderr, "usage: %s config notifications [show | set --provider P --target T [--token K] [--events E]]\n", name.Name)
		return errQuiet
	}
}
