package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frithuz/mdchk/internal/configscan"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

const configHelp = `Usage: mdchk config <subcommand> [options] [project]

Subcommands:
  validate   Validate every eligible .mdchk.yaml beneath a project directory
`
const validateHelp = `Usage: mdchk config validate [options] [project]

The project defaults to the working directory. Use -- for dash-prefixed paths.
  --format FORMAT     text (default) or json
  --limit NAME=VALUE  Set a finite applicable resource limit; repeat as needed
  --help              Show this help
  --version           Print version

Validation is read-only and stays beneath the selected project.
Invalid definitions return 12, traversal/ignore I/O 13, internal failures 14,
and runtime interruption 15. Precedence: 14 > 13 > 12 > 15 > 0.
The --config and --dry-run options and vscode/github formats are unsupported.
`

var validationLimitNames = []string{"config-bytes", "total-config-bytes", "directory-entries", "path-bytes", "path-depth", "structured-input-depth", "syntax-depth", "document-nodes", "total-nodes", "work-units", "operation-time", "response-bytes", "reporting-time", "report-reserve-bytes"}

func runConfig(ctx context.Context, args []string, stdout io.Writer, version string) int {
	var globals []string
	for len(args) > 0 && (args[0] == "--help" || args[0] == "--version") {
		globals = append(globals, args[0])
		args = args[1:]
	}
	if len(args) == 0 {
		for _, g := range globals {
			if g == "--help" {
				return output(stdout, configHelp, 0)
			}
		}
		if len(globals) > 0 {
			return output(stdout, "mdchk "+version+"\n", 0)
		}
		return output(stdout, configHelp, 0)
	}
	fail := func(err error, code string, category result.ErrorCategory, format string, limits map[string]int64) int {
		r := failureResult(err, code, category)
		r.Operation = "config validate"
		r.ConfigValidation = &result.ConfigValidation{}
		return reportResult(r, format, stdout, limits)
	}
	if args[0] != "validate" {
		return fail(fmt.Errorf("unknown config subcommand %q", args[0]), "invalid-arguments", result.Usage, recoverFormat(args), nil)
	}
	opts, err := parseOptions(append(globals, args[1:]...), true)
	if err == nil && opts.config != "" {
		err = errors.New("config validate does not accept --config")
	}
	if err == nil && len(opts.paths) > 1 {
		err = errors.New("config validate accepts at most one directory")
	}
	if err == nil {
		for name := range opts.limits {
			if !validationLimitApplicable(name) {
				err = fmt.Errorf("limit %q does not apply to config validate", name)
				break
			}
		}
	}
	if err != nil {
		return fail(err, "invalid-arguments", result.Usage, recoverFormat(args[1:]), nil)
	}
	if opts.help {
		return output(stdout, validateHelp, 0)
	}
	if opts.version {
		return output(stdout, "mdchk "+version+"\n", 0)
	}
	if opts.format != "text" && opts.format != "json" {
		return fail(fmt.Errorf("format %q is not yet supported; use text or json", opts.format), "unsupported-format", result.Usage, "text", opts.limits)
	}
	r := result.NewConfigValidation()
	if err := reportFeasibility(r, opts.format, opts.limits); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return fail(err, "response-manifest-too-large", result.Usage, opts.format, opts.limits)
	}
	profile := parser.Limits{DocumentBytes: 1048576, SyntaxDepth: reportLimit(opts.limits, "syntax-depth"), DocumentNodes: reportLimit(opts.limits, "document-nodes"), TotalNodes: reportLimit(opts.limits, "total-nodes"), WorkUnits: reportLimit(opts.limits, "work-units"), OperationTime: time.Duration(reportLimit(opts.limits, "operation-time"))}
	budget, err := parser.NewOperationBudget(profile)
	if err == nil && reportLimit(opts.limits, "structured-input-depth") > 4096 {
		err = errors.New("structured-input-depth cannot exceed 4096")
	}
	scanLimits := configscan.Limits{Entries: reportLimit(opts.limits, "directory-entries"), PathBytes: reportLimit(opts.limits, "path-bytes"), Depth: reportLimit(opts.limits, "path-depth"), SourceBytes: reportLimit(opts.limits, "config-bytes"), TotalSourceBytes: reportLimit(opts.limits, "total-config-bytes")}
	if scanLimits.Entries > 1<<30 || scanLimits.PathBytes > 1<<20 || scanLimits.Depth > 4096 || scanLimits.TotalSourceBytes > 1<<30 {
		err = errors.New("unsupported configuration scan limits")
	}
	if err != nil {
		return fail(err, "unsupported-limits", result.Usage, opts.format, opts.limits)
	}
	root := "."
	if len(opts.paths) > 0 {
		root = opts.paths[0]
	}
	info, err := os.Lstat(root)
	if err != nil {
		return fail(err, "invalid-project", result.Usage, opts.format, opts.limits)
	}
	if !info.IsDir() {
		return fail(errors.New("project must be a nonsymlink directory"), "invalid-project", result.Usage, opts.format, opts.limits)
	}
	registry, providers, capabilities, err := configuredProviders()
	if err != nil {
		return fail(err, "provider-initialization", result.Internal, opts.format, opts.limits)
	}
	if ctx == nil {
		return fail(errors.New("nil execution context"), "invalid-context", result.Internal, opts.format, opts.limits)
	}
	ctx, cancel := context.WithTimeout(ctx, profile.OperationTime)
	defer cancel()
	scanned := configscan.Validate(ctx, configscan.Options{Root: root, Limits: scanLimits, Budget: budget, StructuredDepth: int(reportLimit(opts.limits, "structured-input-depth"))}, registry, providers, capabilities)
	r = validationResult(scanned)
	for _, name := range validationLimitNames {
		if name == "response-bytes" || name == "reporting-time" || name == "report-reserve-bytes" {
			continue
		}

		source := "default"
		if _, ok := opts.limits[name]; ok {
			source = "operator"
		}
		r.EffectiveLimits = append(r.EffectiveLimits, result.EffectiveLimit{Name: name, Value: reportLimit(opts.limits, name), Unit: validationLimitUnit(name), Source: source})
	}
	return reportResult(r, opts.format, stdout, opts.limits)
}
func validationLimitApplicable(name string) bool {
	for _, n := range validationLimitNames {
		if name == n {
			return true
		}
	}
	return false
}

func validationResult(scanned configscan.ValidationResult) result.Result {
	r := result.NewConfigValidation()
	r.ConfigValidation.ScanComplete = scanned.Scan.Complete
	logical := func(path string) string {
		cwd, e := os.Getwd()
		if e == nil {
			if rel, e := filepath.Rel(cwd, path); e == nil {
				return filepath.ToSlash(rel)
			}
		}
		return filepath.ToSlash(path)
	}
	snapshots := map[string]result.Snapshot{}
	for _, o := range scanned.Outcomes {
		snapshots[o.Source.Path] = result.Snapshot{View: "original", SHA256: o.Source.SHA256, SizeBytes: int(o.Source.SizeBytes)}
	}
	acquired := map[string]bool{}
	for _, o := range scanned.Outcomes {
		path := logical(o.Source.Path)
		snapshot := snapshots[o.Source.Path]
		acquired[o.Source.Path] = true
		r.CheckedFiles = append(r.CheckedFiles, path)
		analysis := "complete"
		var limit *parser.LimitExceeded
		interrupted := errors.As(o.Err, &limit) || errors.Is(o.Err, context.Canceled) || errors.Is(o.Err, context.DeadlineExceeded)
		if interrupted {
			analysis = "partial"
		} else {
			r.ConfigValidation.DefinitionsChecked++
			if o.Err == nil {
				r.ConfigValidation.ValidDefinitions++
			} else {
				r.ConfigValidation.InvalidDefinitions++
			}
		}
		r.Files = append(r.Files, result.File{Path: path, Analysis: analysis, WriteStatus: "not-requested", ReturnStatus: "not-requested", InputSnapshot: &snapshot, ResultSnapshot: &snapshot, ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
	}
	for _, f := range scanned.Scan.Errors {
		category := result.IO
		if f.Role == configscan.DefinitionSource {
			category = result.Configuration
		}
		base := result.Error{Category: category, Code: "config-" + string(f.Role), Scope: "operation", Path: logical(f.Path), Message: f.Error()}
		if acquired[f.Path] {
			details := configurationFailure(f.Err, base, snapshots)
			r.Errors = append(r.Errors, details.Errors...)
		} else {
			r.Errors = append(r.Errors, base)
			if category == result.Configuration {
				r.Files = append(r.Files, result.File{Path: logical(f.Path), Analysis: "failed", WriteStatus: "not-requested", ReturnStatus: "not-requested", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
			}
		}
	}
	for _, s := range scanned.Scan.Skipped {
		message := ""
		if len(s.Origins) > 0 {
			var origins []string
			for _, o := range s.Origins {
				location := logical(o.Path)
				if o.Position != nil {
					location += fmt.Sprintf(":%d:%d", o.Position.Line, o.Position.Column)
				}
				origins = append(origins, location)
			}
			message = "from " + strings.Join(origins, ", ")
		} else if s.Source != "" {
			message = fmt.Sprintf("from %s:%d", logical(s.Source), s.Line)
		}
		r.SkippedFiles = append(r.SkippedFiles, result.SkippedFile{Path: logical(s.Path), Reason: strings.ReplaceAll(s.Reason, " ", "-"), Message: message})
		if s.Definition {
			r.ConfigValidation.SkippedDefinitions++
		}
	}
	if err := scanned.Scan.Interruption; err != nil {
		stop := result.Interruption{Reason: "cancelled", Phase: "configuration", Message: err.Error()}
		var limit *parser.LimitExceeded
		if errors.As(err, &limit) {
			stop.Reason = "limit-exceeded"

			stop.Limit = &result.LimitObservation{Name: limit.Name, Observed: limit.Observed, Allowed: limit.Allowed, Unit: validationLimitUnit(limit.Name)}
		} else if errors.Is(err, context.DeadlineExceeded) {
			stop.Reason = "deadline-exceeded"
		}
		r.Interruptions = append(r.Interruptions, stop)
	}
	engine.FinalizeLintResult(&r)
	return r
}

func validateConfigLimits(limits map[string]int64) error {
	for _, pair := range [][2]string{{"config-bytes", "total-config-bytes"}, {"report-reserve-bytes", "response-bytes"}, {"document-nodes", "total-nodes"}} {
		if reportLimit(limits, pair[0]) > reportLimit(limits, pair[1]) {
			return fmt.Errorf("limit %s must not exceed %s", pair[0], pair[1])
		}
	}
	if reportLimit(limits, "report-reserve-bytes") < 65536 {
		return fmt.Errorf("report-reserve-bytes must be at least 65536")
	}
	for name, n := range limits {
		if n > 9007199254740991 {
			return fmt.Errorf("limit %s exceeds supported result precision", name)
		}
	}
	return nil
}

// validationLimitUnit keeps effective and observed quotas in the same units.
// Time observations already carry nanoseconds, unlike the seconds in limits.json.
func validationLimitUnit(name string) string {
	switch name {
	case "config-bytes", "total-config-bytes", "path-bytes", "response-bytes", "report-reserve-bytes":
		return "bytes"
	case "operation-time", "reporting-time":
		return "nanoseconds"
	case "directory-entries":
		return "observed entries"
	case "path-depth":
		return "segments"
	case "structured-input-depth":
		return "nested containers"
	case "syntax-depth":
		return "nested constructs"
	case "document-nodes", "total-nodes":
		return "nodes or source records"
	case "work-units", "configuration-pattern-work":
		return "metered work units"
	default:
		return "count"
	}
}
