package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Only startup synchronization is overlaid. The production signal registration,
// context, command, reporter and process exit are unchanged. The marker proves
// NotifyContext returned before the parent sends the actual OS event.
func TestCompiledPlatformInterrupt(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	seam := "\tstatus := cli.RunContext"
	if strings.Count(string(source), seam) != 1 {
		t.Fatal("main readiness seam changed")
	}
	hook := `if err := os.WriteFile(os.Getenv("MDCHK_TEST_READY"), []byte("ready"), 0600); err != nil { panic(err) }
 var readyByte [1]byte
 if _, err := os.Stdin.Read(readyByte[:]); err != nil { panic(err) }
 `
	modified := strings.Replace(string(source), seam, hook+seam, 1)
	dir := t.TempDir()
	replacement := filepath.Join(dir, "main.go")
	//nolint:gosec // Fixed source overlay in a test-owned temporary directory.
	if err := os.WriteFile(replacement, []byte(modified), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := filepath.Abs("main.go")
	if err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(dir, "overlay.json")
	mapping, err := json.Marshal(map[string]any{"Replace": map[string]string{original: replacement}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlay, mapping, 0600); err != nil {
		t.Fatal(err)
	}
	binary := buildConfigurationExecutable(t, overlay)
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
	putConfigurationFile(t, root, "doc.md", strings.Repeat("bounded real work\n", 30000)+"last")
	check := unchangedFixFixture(t, root)
	defer check()
	ready := filepath.Join(dir, "ready")
	//nolint:gosec // Execute only the just-built synchronization overlay.
	cmd := exec.Command(binary, "lint", "--format=json", "doc.md")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "MDCHK_TEST_READY="+ready)
	configureInterruptProcess(cmd)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	finished := false
	defer func() {
		if err := input.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
		if !finished {
			_ = cmd.Process.Kill()
			select {
			case cleanupErr := <-done:
				if cleanupErr != nil {
					var exit *exec.ExitError
					if !errors.As(cleanupErr, &exit) {
						t.Error(cleanupErr)
					}
				}
			case <-time.After(5 * time.Second):
				t.Error("owned process cleanup timed out")
			}
		}
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	waiting := true
	for waiting {
		select {
		case err := <-done:
			finished = true
			t.Fatalf("process exited before handler readiness: %v %s", err, &errOut)
		case <-deadline.C:
			t.Fatal("signal-handler readiness timed out")
		case <-poll.C:
			//nolint:gosec // Read only the test-owned startup marker.
			if data, err := os.ReadFile(ready); err == nil && string(data) == "ready" {
				waiting = false
			}
		}
	}
	// Platform helper sends a kernel/console event, never a Go context cancel.
	sendPlatformInterrupt(t, cmd.Process)
	if _, err := input.Write([]byte("go")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		finished = true
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 15 {
			t.Fatalf("caught interrupt: %v stdout=%s stderr=%s", err, &out, &errOut)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("caught interrupt failed to finish")
	}
	r := validateConfigurationJSON(t, out.Bytes())
	cancelled := false
	for _, i := range r.Interruptions {
		cancelled = cancelled || i.Reason == "cancelled"
	}
	if r.Complete || r.Status != "incomplete" || !cancelled || errOut.Len() != 0 {
		t.Fatalf("interrupt result: %+v stderr%s", r, &errOut)
	}
	t.Log("actual OS interrupt caught by normal NotifyContext; JSON incomplete/cancelled; exit15; bytes unchanged")
}

func TestCompiledPlatformRuntimeBounds(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
	putConfigurationFile(t, root, "doc.md", strings.Repeat("real source\n", 1000))
	check := unchangedFixFixture(t, root)
	defer check()
	got := runConfigurationProcess(t, binary, root, "lint", "--format=json", "--limit=operation-time=1ns", "--limit=queue-time=1ns", "doc.md")
	r := validateConfigurationJSON(t, []byte(got.Stdout))
	if got.Status != 15 || r.Complete || len(r.Interruptions) == 0 {
		t.Fatal(got)
	}
	t.Log("ordinary executable runtime deadline reported incomplete/15")
}
