// Package cli implements mdchk command dispatch without process exits or project I/O.
package cli

import (
	"fmt"
	"io"
	"strings"
)

const (
	exitUsage      = 11
	exitIO         = 13
	exitIncomplete = 15
)

const generalHelp = `Usage: mdchk [global options] <command> [options] [paths...]

Commands:
  lint       Parse lint arguments (execution is not available in this build)

Global options:
  --help     Show general or selected command help
  --version  Print version
`
const lintHelp = `Usage: mdchk lint [options] [paths...]

Options may appear before or after paths. Use -- for dash-prefixed paths.
  --config PATH       Select project configuration
  --format FORMAT     text (default), json, vscode, or github
  --limit NAME=VALUE  Set a finite resource limit; repeat for multiple limits
  --help              Show this help
  --version           Print version

Bootstrap only: no files are checked and no configuration is loaded.
Lint execution returns 15; result reporters are not yet implemented.
`

// Run executes with supplied arguments (excluding argv[0]) and streams. Version
// is supplied by the executable so tests and alternate launchers need no globals.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	var global []string
	for len(args) > 0 && (args[0] == "--help" || args[0] == "--version") {
		global = append(global, args[0])
		args = args[1:]
	}
	if len(args) == 0 {
		for _, arg := range global {
			if arg == "--help" {
				return output(stdout, generalHelp, 0)
			}
		}
		if len(global) > 0 {
			return output(stdout, "mdchk "+version+"\n", 0)
		}
		return output(stdout, generalHelp, 0)
	}
	switch args[0] {
	case "lint":
		opts, err := parseLint(append(global, args[1:]...))
		if err != nil {
			return output(stderr, "mdchk: "+err.Error()+"\n", exitUsage)
		}
		if opts.help {
			return output(stdout, lintHelp, 0)
		}
		if opts.version {
			return output(stdout, "mdchk "+version+"\n", 0)
		}
		return output(stderr, "mdchk: lint execution is not available in this build; no files were checked\n", exitIncomplete)
	default:
		return output(stderr, fmt.Sprintf("mdchk: unknown command or global option %q; use --help\n", args[0]), exitUsage)
	}
}

func output(w io.Writer, message string, status int) int {
	if _, err := io.WriteString(w, message); err != nil {
		return exitIO
	}
	return status
}

type lintOptions struct {
	paths         []string
	config        string
	format        string
	limits        map[string]int64
	help, version bool
}

func parseLint(args []string) (lintOptions, error) {
	opts := lintOptions{format: "text", limits: make(map[string]int64)}
	literal := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if literal {
			opts.paths = append(opts.paths, arg)
			continue
		}
		if arg == "--" {
			literal = true
			continue
		}
		if arg == "--help" {
			opts.help = true
			continue
		}
		if arg == "--version" {
			opts.version = true
			continue
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			opts.paths = append(opts.paths, arg)
			continue
		}
		name, value, inline := strings.Cut(arg, "=")
		switch name {
		case "--config", "--format", "--limit":
		default:
			return opts, fmt.Errorf("unsupported lint option %q", arg)
		}
		if !inline {
			next := i + 1
			if next >= len(args) {
				return opts, fmt.Errorf("%s requires a value", name)
			}
			//nolint:gosec // next is nonnegative and checked against len(args) immediately above.
			value = args[next]
			if strings.HasPrefix(value, "--") {
				return opts, fmt.Errorf("%s requires a value", name)
			}
			i = next
		}
		if value == "" {
			return opts, fmt.Errorf("%s requires a nonempty value", name)
		}
		switch name {
		case "--config":
			opts.config = value
		case "--format":
			switch value {
			case "text", "json", "vscode", "github":
				opts.format = value
			default:
				return opts, fmt.Errorf("unsupported format %q", value)
			}
		case "--limit":
			key, n, err := parseLimit(value)
			if err != nil {
				return opts, err
			}
			if old, ok := opts.limits[key]; ok && old != n {
				return opts, fmt.Errorf("conflicting values for limit %q", key)
			}
			opts.limits[key] = n
		}
	}
	if err := validateLimits(opts.limits); err != nil {
		return opts, err
	}
	return opts, nil
}
