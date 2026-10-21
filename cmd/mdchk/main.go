// mdchk is the command-line entry point.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"

	"github.com/frithuz/mdchk/internal/cli"
)

func main() {
	configureOutputSignals()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	status := cli.RunContext(ctx, os.Args[1:], os.Stdout, os.Stderr, buildVersion())
	stop()
	os.Exit(status)
}

func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
