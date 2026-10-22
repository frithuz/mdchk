//go:build unix

package main

import (
	"os"
	"os/exec"
	"testing"
)

func configureInterruptProcess(_ *exec.Cmd) {}
func sendPlatformInterrupt(t *testing.T, p *os.Process) {
	t.Helper()
	if err := p.Signal(os.Interrupt); err != nil {
		t.Fatalf("actual SIGINT delivery: %v", err)
	}
}
