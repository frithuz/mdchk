// specadapter is a test executable for the retained upstream spec_tests.py.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, input io.Reader, output, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("specadapter", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	scope := flags.String("scope", "core", "closed conformance scope (tagged test build only)")
	machine := flags.Bool("json", false, "emit test outcome JSON instead of upstream HTML")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintln(diagnostics, "specadapter accepts Markdown on stdin, not positional arguments")
		return 1
	}
	content, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
	if err != nil {
		_, _ = fmt.Fprintln(diagnostics, "read input:", err)
		return 1
	}
	if len(content) > 1<<20 {
		_, _ = fmt.Fprintln(diagnostics, "adapter input exceeds 1048576 bytes")
		return 1
	}
	result, actual, err := execute(content, *scope)
	if err != nil {
		_, _ = fmt.Fprintln(diagnostics, err)
		return 1
	}
	if *machine {
		// Protocol success does not mean a conformance pass: Status is required.
		if err := writeJSON(output, actual); err != nil {
			_, _ = fmt.Fprintln(diagnostics, err)
			return 1
		}
		return 0
	}
	if result.Status == "rendered" {
		n, err := io.WriteString(output, result.HTML)
		if err == nil && n != len(result.HTML) {
			err = io.ErrShortWrite
		}
		if err != nil {
			_, _ = fmt.Fprintln(diagnostics, err)
			return 1
		}
		return 0
	}
	if err := writeJSON(diagnostics, result); err != nil {
		return 1
	}
	switch result.Status {
	case "pending":
		return 2
	case "interrupted":
		return 3
	default:
		return 1
	}
}

// Check short writes explicitly; test adapters never report protocol success
// after a writer silently accepted only a prefix of the JSON object.
func writeJSON(output io.Writer, value any) error {
	content, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(content) >= 16<<20 {
		return fmt.Errorf("adapter JSON output exceeds limit")
	}
	content = append(content, '\n')
	n, err := output.Write(content)
	if err == nil && n != len(content) {
		return io.ErrShortWrite
	}
	return err
}
