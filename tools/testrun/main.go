// testrun runs go test quietly: one summary line when every test passes, only
// the failing output when one fails, and the full -v stream when DEBUG is set.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, output, diagnostics io.Writer) int {
	if debug(os.Getenv("DEBUG")) {
		//nolint:gosec // The caller's own go test arguments, passed through unchanged.
		command := exec.Command("go", append([]string{"test", "-v"}, args...)...)
		command.Stdin, command.Stdout, command.Stderr = os.Stdin, output, diagnostics
		return status(command.Run(), diagnostics)
	}
	//nolint:gosec // The caller's own go test arguments, passed through unchanged.
	command := exec.Command("go", append([]string{"test", "-json"}, args...)...)
	command.Stdin, command.Stderr = os.Stdin, diagnostics
	events, err := command.StdoutPipe()
	if err != nil {
		_, _ = fmt.Fprintf(diagnostics, "testrun: %v\n", err)
		return 1
	}
	if err := command.Start(); err != nil {
		_, _ = fmt.Fprintf(diagnostics, "testrun: %v\n", err)
		return 1
	}
	result, readErr := filter(events, output)
	// Drain what an early filter error left, so go test can finish.
	_, _ = io.Copy(io.Discard, events)
	code := status(command.Wait(), diagnostics)
	if readErr != nil {
		_, _ = fmt.Fprintf(diagnostics, "testrun: reading go test output: %v\n", readErr)
		if code == 0 {
			code = 1
		}
	}
	_, _ = fmt.Fprintln(output, result.summary(code))
	return code
}

// debug reports whether a DEBUG value turns on verbose output: set,
// non-empty and not "0".
func debug(value string) bool { return value != "" && value != "0" }

// status maps the go test result to the exit code testrun returns.
func status(err error, diagnostics io.Writer) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		return exit.ExitCode()
	}
	_, _ = fmt.Fprintf(diagnostics, "testrun: go test: %v\n", err)
	return 1
}
