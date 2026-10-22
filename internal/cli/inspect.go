package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

const inspectHelp = `Usage: mdchk inspect [options] <file>

Reveal one immutable file snapshot without changing files or deciding validity.
Options may appear before or after the target. Use -- for dash-prefixed paths.
  --config PATH       Select configuration relative to the invocation directory
  --lines N[-M]       Select inclusive physical lines (positive decimal numbers)
  --format FORMAT     text (default) or json
  --limit NAME=VALUE  Set a finite applicable resource limit; repeat as needed
  --help              Show this help
  --version           Print version

Completed observations return 0; usage 11, target/output I/O 13, internal 14,
and runtime/reporting interruption 15. Only text and JSON are supported.
`

var inspectLimitNames = []string{"document-bytes", "total-content-bytes", "config-bytes", "total-config-bytes", "structured-input-depth", "syntax-depth", "document-nodes", "total-nodes", "work-units", "operation-time", "response-bytes", "reporting-time", "report-reserve-bytes"}

type inspectOptions struct {
	lintOptions
}

func parseInspect(args []string) (inspectOptions, error) {
	opts, err := parseCommandOptions(args, true, true)
	if err != nil {
		return inspectOptions{}, err
	}
	if reportLimit(opts.limits, "document-bytes") > reportLimit(opts.limits, "total-content-bytes") {
		return inspectOptions{}, errors.New("limit document-bytes must not exceed total-content-bytes")
	}
	for name := range opts.limits {
		if !inspectLimitApplicable(name) {
			return inspectOptions{}, fmt.Errorf("limit %q does not apply to inspect", name)
		}
	}
	if !opts.help && !opts.version && len(opts.paths) != 1 {
		return inspectOptions{}, errors.New("inspect requires exactly one file")
	}
	if _, err := inspection.ParseLineSelection(opts.lines); err != nil {
		return inspectOptions{}, err
	}
	return inspectOptions{opts}, nil
}
func inspectLimitApplicable(name string) bool {
	for _, n := range inspectLimitNames {
		if n == name {
			return true
		}
	}
	return false
}
func newInspectResult() result.Result {
	r := result.NewLint("cli")
	r.Operation = "inspect"
	r.Status = "inspected"
	return r
}

func runInspect(ctx context.Context, args []string, stdout io.Writer, version string) int {
	opts, err := parseInspect(args)
	if err != nil {
		return inspectFailure(err, "invalid-arguments", result.Usage, recoverInspectFormat(args), stdout, nil)
	}
	if opts.format != "text" && opts.format != "json" {
		return inspectFailure(fmt.Errorf("format %q is not supported; use text or json", opts.format), "unsupported-format", result.Usage, "text", stdout, opts.limits)
	}
	if opts.help {
		return output(stdout, inspectHelp, 0)
	}
	if opts.version {
		return output(stdout, "mdchk "+version+"\n", 0)
	}
	profile := parser.Limits{DocumentBytes: reportLimit(opts.limits, "document-bytes"), SyntaxDepth: reportLimit(opts.limits, "syntax-depth"), DocumentNodes: reportLimit(opts.limits, "document-nodes"), TotalNodes: reportLimit(opts.limits, "total-nodes"), WorkUnits: reportLimit(opts.limits, "work-units"), OperationTime: time.Duration(reportLimit(opts.limits, "operation-time"))}
	budget, err := parser.NewOperationBudget(profile)
	if err == nil && (reportLimit(opts.limits, "structured-input-depth") > 4096 || reportLimit(opts.limits, "total-config-bytes") > 1<<30) {
		err = errors.New("unsupported configuration limits")
	}
	if err != nil {
		return inspectFailure(err, "unsupported-limits", result.Usage, opts.format, stdout, opts.limits)
	}
	if ctx == nil {
		return inspectFailure(errors.New("nil inspection context"), "invalid-context", result.Internal, opts.format, stdout, opts.limits)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return inspectFailure(err, "working-directory", result.IO, opts.format, stdout, opts.limits)
	}
	registry, providers, caps, err := configuredProviders()
	if err != nil {
		return inspectFailure(err, "provider-initialization", result.Internal, opts.format, stdout, opts.limits)
	}
	out, p := executeInspect(ctx, opts, version, cwd, budget, registry, providers, caps)
	return reportInspection(out, p, opts.format, stdout, opts.limits)
}

func executeInspect(ctx context.Context, opts inspectOptions, version, cwd string, budget *parser.OperationBudget, registry *engine.Registry, providers engine.ValidationProviders, caps config.Capabilities) (output result.Result, projected *inspection.Projection) {
	defer func() { setInspectionLimits(&output, opts.limits) }()
	ctx, cancel := context.WithTimeout(ctx, budget.Limits().OperationTime)
	defer cancel()
	snapshot, err := inspection.Acquire(ctx, opts.paths[0], inspection.Options{WorkingDirectory: cwd, ConfigPath: opts.config, Version: version, Budget: budget, TotalContentBytes: reportLimit(opts.limits, "total-content-bytes"), ConfigBytes: reportLimit(opts.limits, "config-bytes"), TotalConfigBytes: reportLimit(opts.limits, "total-config-bytes"), StructuredDepth: int(reportLimit(opts.limits, "structured-input-depth")), Registry: registry, Providers: providers, Capabilities: caps})
	if snapshot == nil {
		out := newInspectResult()
		if isInspectInterruption(err) {
			out.Interruptions = append(out.Interruptions, inspectInterruption(err, "acquisition"))
		} else {
			category := result.IO
			var pathErr *fs.PathError
			if errors.Is(err, files.ErrNotRegularTarget) || errors.As(err, &pathErr) && pathErr.Op == "size" {
				category = result.Usage
			}
			out.Errors = append(out.Errors, result.Error{Category: category, Code: "acquisition-failed", Scope: "operation", Message: err.Error()})
		}
		finalizeInspection(&out)
		return out, nil
	}
	// Physical bounds are checked on the acquired immutable line index, before
	// analysis. Omitted lines are valid even for an empty source.
	if _, lineErr := inspection.ParseLines(snapshot.Lines(), opts.lines); lineErr != nil {
		out := newInspectResult()
		out.Errors = append(out.Errors, result.Error{Category: result.Usage, Code: "invalid-lines", Scope: "operation", Message: lineErr.Error()})
		finalizeInspection(&out)
		return out, nil
	}
	if err == nil {
		manifest, projected := inspectionManifest(snapshot)
		encoded, _ := json.Marshal(manifest)
		empty, _ := json.Marshal(newInspectResult())
		if int64(len(encoded)-len(empty))+reportLimit(opts.limits, "report-reserve-bytes") > reportLimit(opts.limits, "response-bytes") {
			manifest.Interruptions = append(manifest.Interruptions, result.Interruption{ID: "manifest-interruption", Reason: "limit-exceeded", Phase: "acquisition", Message: "known target identity plus reporting reserve exceeds response-bytes; analysis did not start"})
			if cause := snapshot.ConfigurationError(); cause != nil {
				manifest.Errors = append(manifest.Errors, result.Error{Category: result.Configuration, Code: "configuration-invalid", Scope: "observation", Path: snapshot.File().Path, Message: cause.Error()})
			} else if cause := snapshot.AdmissionError(); cause != nil {
				manifest.Errors = append(manifest.Errors, result.Error{Category: result.Configuration, Code: "capability-unavailable", Scope: "observation", Path: snapshot.File().Path, Message: cause.Error()})
			}
			finalizeInspection(&manifest)
			rebaseInspection(&manifest, &projected, cwd)
			manifest.Inspection, _ = json.Marshal(projected)
			return manifest, &projected
		}
	}
	full, analysisErr := inspection.Analyze(ctx, snapshot, inspection.AnalyzeOptions{})
	if full.Result.SchemaVersion == 0 {
		out := newInspectResult()
		out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "inspection-failed", Scope: "operation", Message: analysisErr.Error()})
		finalizeInspection(&out)
		return out, nil
	}
	if err == nil {
		err = analysisErr
	}
	if err == nil {
		filtered, filterErr := inspection.Filter(ctx, snapshot, full, opts.lines)
		if filterErr == nil {
			full = filtered
		} else {
			err = filterErr
		}
	}
	out, p := full.Result, full.Projection
	if err != nil && len(out.Interruptions) == 0 {
		if isInspectInterruption(err) {
			out.Interruptions = append(out.Interruptions, inspectInterruption(err, "presentation"))
		} else {
			out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "inspection-failed", Scope: "operation", Message: err.Error()})
		}
	}
	if err != nil && opts.lines != "" {
		note, _ := json.Marshal(result.Note{ID: "selection-note", Code: "selection-unavailable", Message: "Requested line selection was not constructed; retained data describes the full snapshot.", Path: snapshot.File().Path})
		out.Notes = append(out.Notes, note)
	}

	rebaseInspection(&out, &p, cwd)

	// Existing package records are already canonical. Path rebasing preserves IDs
	// and links; reporting must not run the lint outcome finalizer on these views.
	inspectionOutcome(&out)
	out.Inspection, _ = json.Marshal(p)
	// Observe the same elapsed meter after CLI path/projection publication, too;
	// available completed data remains justified if that final stage expires.
	if err == nil {
		stop := ctx.Err()
		if stop == nil {
			stop = budget.ChargeWork(0)
		}
		if stop != nil {
			out.Interruptions = append(out.Interruptions, inspectInterruption(stop, "presentation"))
			inspectionOutcome(&out)
		}
	}
	return out, &p
}

// inspectionManifest is a bounded identity-only view available before analysis.
// A refused manifest never claims that parsing, directive inventory or counts ran.
func inspectionManifest(snapshot *inspection.Snapshot) (result.Result, inspection.Projection) {
	f := snapshot.File()
	exclusion := snapshot.Exclusion()
	p := inspection.Projection{ProjectionVersion: 1, File: inspection.InspectionFile{ResolvedPath: f.Path, BinaryVersion: f.Version, SizeBytes: f.SizeBytes, SizeDisplay: f.SizeDisplay, SHA1: f.SHA1, SHA256: f.SHA256, NormallyExcluded: exclusion.Excluded, NormallyExcludedKnown: exclusion.Known, PhysicalLineCount: snapshot.Lines().LineCount()}, Configuration: omittedSection[result.ConfigurationData](), Flavor: omittedSection[string](), AST: omittedSection[inspection.AST](), Rules: omittedSection[[]inspection.Rule](), RuleCounts: omittedSection[json.RawMessage](), Directives: omittedSection[[]inspection.Directive](), RawCandidates: omittedSection[[]inspection.RawCandidate](), ParsingNotes: omittedSection[[]result.Note](), DocumentContext: inspection.Section[json.RawMessage]{Availability: "not-requested", Reasons: []string{}}, Resources: inspection.Section[json.RawMessage]{Availability: "not-requested", Reasons: []string{}}}
	r := newInspectResult()
	r.CheckedFiles = []string{f.Path}
	identity := result.Snapshot{View: "original", SHA256: f.SHA256, SizeBytes: int(f.SizeBytes)}
	r.Files = []result.File{{Path: f.Path, Analysis: "not-started", InputSnapshot: &identity, WriteStatus: "not-requested", ReturnStatus: "not-requested", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}}}
	r.Inspection, _ = json.Marshal(p)
	return r, p
}

func setInspectionLimits(out *result.Result, limits map[string]int64) {
	for _, name := range inspectLimitNames {
		source := "default"
		out.EffectiveLimits = slices.DeleteFunc(out.EffectiveLimits, func(v result.EffectiveLimit) bool { return v.Name == name })
		if _, ok := limits[name]; ok {
			source = "operator"
		}
		unit := result.LimitUnit(name)
		if name == "reporting-time" {
			unit = "nanoseconds"
		}
		if name == "response-bytes" || name == "report-reserve-bytes" {
			unit = "bytes"
		}
		out.EffectiveLimits = append(out.EffectiveLimits, result.EffectiveLimit{Name: name, Value: reportLimit(limits, name), Unit: unit, Source: source})
	}
}

func isInspectInterruption(err error) bool {
	var limit *parser.LimitExceeded
	return errors.As(err, &limit) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
func inspectInterruption(err error, phase string) result.Interruption {
	reason := "limit-exceeded"
	if errors.Is(err, context.Canceled) {
		reason = "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "deadline-exceeded"
	}
	stop := result.Interruption{ID: "inspection-interruption", Reason: reason, Phase: phase, Message: err.Error()}
	var limit *parser.LimitExceeded
	if errors.As(err, &limit) {
		stop.Limit = &result.LimitObservation{Name: limit.Name, Observed: limit.Observed, Allowed: limit.Allowed, Unit: result.LimitUnit(limit.Name)}
	}
	return stop
}
func finalizeInspection(out *result.Result) { engine.FinalizeLintResult(out); inspectionOutcome(out) }
func inspectionOutcome(out *result.Result) {
	out.Operation = "inspect"
	out.Status = "inspected"
	if len(out.Errors) > 0 || len(out.Coverage) > 0 {
		out.Complete = false
	}
	if len(out.Interruptions) > 0 || len(out.Omissions) > 0 {
		out.Complete = false
		out.Status = "incomplete"
	}
	for _, e := range out.Errors {
		if e.Scope == "operation" {
			out.Complete = false
			out.Status = "error"
		}
	}
}
func inspectionExit(out result.Result) int {
	status := 0
	if len(out.Interruptions) > 0 || len(out.Omissions) > 0 {
		status = exitIncomplete
	}
	for _, category := range []result.ErrorCategory{result.Usage, result.Configuration, result.IO, result.Internal} {
		for _, e := range out.Errors {
			if e.Scope == "operation" && e.Category == category {
				switch category {
				case result.Usage:
					status = exitUsage
				case result.Configuration:
					status = exitConfig
				case result.IO:
					status = exitIO
				case result.Internal:
					status = 14
				}
			}
		}
	}
	return status
}
func inspectFailure(err error, code string, category result.ErrorCategory, format string, w io.Writer, limits map[string]int64) int {
	out := newInspectResult()
	out.Errors = append(out.Errors, result.Error{Category: category, Code: code, Scope: "operation", Message: err.Error()})
	finalizeInspection(&out)
	return reportInspection(out, nil, format, w, limits)
}

func rebaseInspection(out *result.Result, p *inspection.Projection, cwd string) {
	logical := func(path string) string {
		if path == "" {
			return ""
		}
		if filepath.IsAbs(path) {
			if rel, err := filepath.Rel(cwd, path); err == nil {
				return filepath.ToSlash(rel)
			}
		}
		return filepath.ToSlash(path)
	}
	for i := range out.CheckedFiles {
		out.CheckedFiles[i] = logical(out.CheckedFiles[i])
	}
	for i := range out.Files {
		out.Files[i].Path = logical(out.Files[i].Path)
	}
	for i := range out.Diagnostics {
		out.Diagnostics[i].Path = logical(out.Diagnostics[i].Path)
	}
	for i := range out.Coverage {
		out.Coverage[i].Path = logical(out.Coverage[i].Path)
	}
	for i := range out.Errors {
		out.Errors[i].Path = logical(out.Errors[i].Path)
	}
	for i := range out.Interruptions {
		out.Interruptions[i].Path = logical(out.Interruptions[i].Path)
	}
	for i := range out.Omissions {
		out.Omissions[i].Path = logical(out.Omissions[i].Path)
	}
	for i := range out.SkippedFiles {
		out.SkippedFiles[i].Path = logical(out.SkippedFiles[i].Path)
	}
	for i, raw := range out.Notes {
		var n result.Note
		_ = json.Unmarshal(raw, &n)
		n.Path = logical(n.Path)
		out.Notes[i], _ = json.Marshal(n)
	}
	for i, raw := range out.SuppressedDiagnostics {
		var d result.SuppressedDiagnostic
		_ = json.Unmarshal(raw, &d)
		d.Diagnostic.Path = logical(d.Diagnostic.Path)
		out.SuppressedDiagnostics[i], _ = json.Marshal(d)
	}
	if p.Configuration.Data != nil {
		c := p.Configuration.Data
		for i := range c.Sources {
			c.Sources[i].Path = logical(c.Sources[i].Path)
		}
		for i := range c.Provenance {
			c.Provenance[i].Path = logical(c.Provenance[i].Path)
		}
	}
	if p.Directives.Data != nil {
		for i := range *p.Directives.Data {
			for j := range (*p.Directives.Data)[i].Effects {
				for k := range (*p.Directives.Data)[i].Effects[j].Provenance {
					v := &(*p.Directives.Data)[i].Effects[j].Provenance[k]
					v.Path = logical(v.Path)
				}
			}
		}
	}
	if p.ParsingNotes.Data != nil {
		for i := range *p.ParsingNotes.Data {
			n := &(*p.ParsingNotes.Data)[i]
			n.Path = logical(n.Path)
		}
	}
}

// Inspection has no editor projection, including on malformed arguments.
func recoverInspectFormat(args []string) string {
	format := recoverFormat(args)
	if format != "json" {
		return "text"
	}
	return format
}
