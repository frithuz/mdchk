//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func configureInterruptProcess(cmd *exec.Cmd) {
	// CTRL_BREAK_EVENT may target this isolated child group, never the test runner.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
func sendPlatformInterrupt(t *testing.T, p *os.Process) {
	t.Helper()
	kernel := syscall.NewLazyDLL("kernel32.dll")
	// A real generation failure records the observed runner console limitation.
	generate := kernel.NewProc("GenerateConsoleCtrlEvent")
	ok, _, nativeErr := generate.Call(1, uintptr(p.Pid)) // CTRL_BREAK_EVENT
	if ok == 0 {
		t.Skipf("actual isolated Windows CTRL_BREAK_EVENT unavailable: %v", nativeErr)
	}
}
