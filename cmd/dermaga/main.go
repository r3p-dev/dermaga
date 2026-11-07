// Command dermaga is the daemon and the CLI of a single-user PaaS for one Debian VPS.
package main

import (
	"os"

	"github.com/r3p-dev/dermaga/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:])) }
