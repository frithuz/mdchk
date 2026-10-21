// mdchk is the command-line entry point.
package main

import (
	"os"
	"runtime/debug"

	"github.com/frithuz/mdchk/internal/cli"
)

func main() { os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, buildVersion())) }

func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
