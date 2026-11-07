package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
	"syscall"
	"unsafe"

	"github.com/r3p-dev/dermaga/internal/auth"
)

const adminUsage = "usage: dermaga admin set-password | totp enable | totp disable"

func cmdAdmin(args []string) error {
	switch strings.Join(args, " ") {
	case "set-password":
		return setPassword()
	case "totp enable":
		return totpEnable()
	case "totp disable":
		if err := call("DELETE", "/api/admin/totp", nil, nil); err != nil {
			return err
		}
		fmt.Println("TOTP disabled.")
		return nil
	default:
		return errors.New(adminUsage)
	}
}

func setPassword() error {
	pw, err := readSecret("New admin password: ")
	if err != nil {
		return err
	}
	if isTerminal() {
		again, err := readSecret("Repeat: ")
		if err != nil {
			return err
		}
		if again != pw {
			return errors.New("passwords do not match")
		}
	}
	if err := call("PUT", "/api/admin/password", map[string]string{"password": pw}, nil); err != nil {
		return err
	}
	fmt.Println("Admin password set. Existing panel sessions were logged out.")
	return nil
}

func totpEnable() error {
	secret := auth.NewTOTPSecret()
	account := "admin"
	if u, err := user.Current(); err == nil {
		account = u.Username
	}
	if h, err := os.Hostname(); err == nil {
		account += "@" + h
	}
	fmt.Printf("Add this to your authenticator app (paste the URI, or enter the secret by hand):\n\n  %s\n\n  secret: %s\n\n",
		auth.TOTPURI(secret, account), secret)
	code, err := readLine("Enter the 6-digit code it shows: ")
	if err != nil {
		return err
	}
	if err := call("PUT", "/api/admin/totp", map[string]string{"secret": secret, "code": code}, nil); err != nil {
		return err
	}
	fmt.Println("TOTP enabled. The panel now asks for a code at login.")
	return nil
}

var stdin = bufio.NewReader(os.Stdin)

func readLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	s, err := stdin.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimRight(s, "\r\n"), nil
}

func termios() (*syscall.Termios, bool) {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return &t, e == 0
}

func isTerminal() bool { _, ok := termios(); return ok }

// readSecret reads a line with echo off when stdin is a terminal; otherwise
// (scripted use) it reads one line from stdin.
func readSecret(prompt string) (string, error) {
	old, ok := termios()
	if !ok {
		return readLine(prompt)
	}
	noEcho := *old
	noEcho.Lflag &^= syscall.ECHO
	setTermios(&noEcho)
	defer func() { setTermios(old); fmt.Fprintln(os.Stderr) }()
	return readLine(prompt)
}

func setTermios(t *syscall.Termios) {
	syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(t)))
}
