//go:build unix

package main

import (
	"os/signal"
	"syscall"
)

// Let stdout/stderr writes return EPIPE so the adapter can preserve its semantic
// exit precedence. Go otherwise terminates on SIGPIPE for descriptors 1 and 2.
// This process-wide disposition belongs only to the executable, not cli.Run.
func configureOutputSignals() {
	signal.Ignore(syscall.SIGPIPE)
}
