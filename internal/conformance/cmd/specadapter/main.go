// specadapter is a test executable for the retained upstream spec_tests.py.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/frithuz/mdchk/internal/conformance"
	"github.com/frithuz/mdchk/internal/parser"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, input io.Reader, output, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("specadapter", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
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
	content, err := io.ReadAll(input)
	if err != nil {
		_, _ = fmt.Fprintln(diagnostics, "read input:", err)
		return 1
	}
	result := conformance.Evaluate(parser.Parse(context.Background(), parser.NewSource(content)))
	if *machine {
		// Protocol success does not mean a conformance pass: Status is required.
		if err := json.NewEncoder(output).Encode(result); err != nil {
			_, _ = fmt.Fprintln(diagnostics, err)
			return 1
		}
		return 0
	}
	if result.Status == "rendered" {
		if _, err := io.WriteString(output, result.HTML); err != nil {
			_, _ = fmt.Fprintln(diagnostics, err)
			return 1
		}
		return 0
	}
	if err := json.NewEncoder(diagnostics).Encode(result); err != nil {
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
