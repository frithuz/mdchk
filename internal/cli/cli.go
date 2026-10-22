// Package cli implements mdchk command dispatch without process exits.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/frithuz/mdchk/internal/result"
)

const (
	exitUsage      = 11
	exitConfig     = 12
	exitIO         = 13
	exitIncomplete = 15
)

const generalHelp = `Usage: mdchk [global options] <command> [options] [paths...]

Commands:
  fix        Apply validated corrections; use --dry-run to preview
  lint       Check Markdown with available registered rules
  config     Validate project configuration
  inspect    Reveal processing details for one Markdown file

Global options:
  --help     Show general or selected command help
  --version  Print version

Lint, fix and config validate support text, json, vscode and github.
Inspect supports text and json.
`
const lintHelp = `Usage: mdchk lint [options] [paths...]

Options may appear before or after paths. Use -- for dash-prefixed paths.
  --config PATH       Select project configuration
  --format FORMAT     text (default), json, vscode or github
  --limit NAME=VALUE  Set a finite resource limit; repeat for multiple limits
  --help              Show this help
  --version           Print version

Configuration is loaded and validated before any target access.
Unavailable configuration returns 12; target I/O or invalid UTF-8 returns 13.
No selected files returns 11. Complete clean checks return 0, violations 10,
and unavailable checks or interrupted analysis 15.
Text and JSON reports go to stdout; execution logs go to stderr.
VS Code: located diagnostics on stdout, unlocated operational records on stderr.
GitHub: error annotations on stdout, safe recovery evidence on stderr.
Run GitHub output from the checkout root; invocation CWD declares path authority.
`

// Run executes with supplied arguments (excluding argv[0]) and streams. Version
// is supplied by the executable so tests and alternate launchers need no globals.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	return RunContext(context.Background(), args, stdout, stderr, version)
}

// RunContext propagates caller cancellation through acquisition, parsing and
// registered checks. Help, version and argument validation still precede IO.
func RunContext(ctx context.Context, args []string, stdout, stderr io.Writer, version string) int {
	return runContext(ctx, args, stdout, stderr, version, prepareLint, func(ctx context.Context, inv *lintInvocation, _ io.Writer, stderr io.Writer) int {
		return acquireLintContext(ctx, inv, stderr)
	})
}

func run(args []string, stdout, stderr io.Writer, version string, prepare func(lintOptions) (*lintInvocation, error), execute func(*lintInvocation, io.Writer, io.Writer) int) int {
	return runContext(context.Background(), args, stdout, stderr, version, prepare, func(_ context.Context, inv *lintInvocation, stdout, stderr io.Writer) int {
		return execute(inv, stdout, stderr)
	})
}

func runContext(ctx context.Context, args []string, stdout, stderr io.Writer, version string, prepare func(lintOptions) (*lintInvocation, error), execute func(context.Context, *lintInvocation, io.Writer, io.Writer) int) int {
	presentation := commandPresentation()
	fail := func(err error, code string, category result.ErrorCategory, format string, limits map[string]int64) int {
		return reportResultStreamsContext(presentation, failureResult(err, code, category), format, stdout, stderr, limits)
	}
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
	case "fix":
		return runFix(ctx, append(global, args[1:]...), stdout, stderr, presentation, version)
	case "inspect":
		return runInspect(ctx, append(global, args[1:]...), stdout, version)
	case "config":
		return runConfig(ctx, append(global, args[1:]...), stdout, stderr, presentation, version)
	case "lint":
		opts, err := parseLint(append(global, args[1:]...))
		if err != nil {
			return fail(err, "invalid-arguments", "usage", recoverFormat(args[1:]), nil)
		}
		if opts.help {
			return output(stdout, lintHelp, 0)
		}
		if opts.version {
			return output(stdout, "mdchk "+version+"\n", 0)
		}
		opts.presentation = presentation
		if err := invocationFeasibility(opts); err != nil {
			return fail(err, "response-manifest-too-large", "usage", opts.format, opts.limits)
		}
		invocation, err := prepare(opts)
		if err != nil {
			return fail(err, "config-definition", "configuration", opts.format, opts.limits)
		}
		status := execute(ctx, invocation, stdout, stderr)
		if invocation.result != nil {
			return reportResultStreamsContext(presentation, *invocation.result, opts.format, stdout, stderr, opts.limits)
		}
		return status
	default:
		return fail(fmt.Errorf("unknown command or global option %q; use --help", args[0]), "unknown-command", "usage", recoverFormat(args), nil)
	}
}

func output(w io.Writer, message string, status int) int {
	if n, err := io.WriteString(w, message); err != nil || n != len(message) {
		return exitIO
	}
	return status
}

// commandPresentation has an independent lifetime: expired operation contexts
// must not consume the reserved reporting budget. CWD is operator-declared
// checkout authority, not a Git-membership or filesystem-confinement check.
func commandPresentation() context.Context {
	ctx := context.Background()
	cwd, err := os.Getwd()
	if err != nil {
		return ctx
	}
	flavor := "unix"
	if runtime.GOOS == "windows" {
		flavor = "windows"
	}
	return withGitHubPaths(ctx, githubPaths{Root: cwd, CWD: cwd, Flavor: flavor})
}

type lintOptions struct {
	presentation  context.Context
	paths         []string
	config        string
	lines         string
	format        string
	limits        map[string]int64
	help, version bool
	dryRun        bool
}

func parseLint(args []string) (lintOptions, error) { return parseOptions(args, false) }

func parseOptions(args []string, validation bool) (lintOptions, error) {
	return parseCommandOptions(args, validation, false)
}

func parseCommandOptions(args []string, validation, inspect bool) (lintOptions, error) {
	return parseCommandOptionsWithFix(args, validation, inspect, false)
}
func parseCommandOptionsWithFix(args []string, validation, inspect, fix bool) (lintOptions, error) {
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
		if fix && arg == "--dry-run" {
			opts.dryRun = true
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
		case "--lines":
			if !inspect {
				return opts, fmt.Errorf("unsupported option %q", arg)
			}
		default:
			return opts, fmt.Errorf("unsupported option %q", arg)
		}
		if !inline {
			next := i + 1
			if next >= len(args) {
				return opts, fmt.Errorf("%s requires a value", name)
			}
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
		case "--lines":
			opts.lines = value
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
	var validationErr error
	if validation {
		validationErr = validateConfigLimits(opts.limits)
	} else {
		validationErr = validateLimits(opts.limits)
	}
	if err := validationErr; err != nil {
		return opts, err
	}
	return opts, nil
}

// recoverFormat recovers presentation independently from argument validation.
// Recognized operands cannot become format flags, and -- ends option parsing.
func recoverFormat(args []string) string {
	format := "text"
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		name, value, inline := strings.Cut(args[i], "=")
		switch name {
		case "--config", "--limit", "--format", "--lines":
		default:
			continue
		}
		if !inline {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				continue
			}
			i++
			value = args[i]
		}
		if name == "--format" {
			format = "text"
			if value == "json" || value == "vscode" || value == "github" {
				format = value
			}
		}
	}
	return format
}
