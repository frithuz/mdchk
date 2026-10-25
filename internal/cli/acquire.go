package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func (inv *lintInvocation) limit(name string) int64 {
	if value, ok := inv.options.limits[name]; ok {
		return value
	}
	return limitDefaults[name]
}

func acquireLint(inv *lintInvocation, _ io.Writer, stderr io.Writer) int {
	return acquireLintContext(context.Background(), inv, stderr)
}

// acquireLintContext gives immutable acquired snapshots to the admitted plan.
// The acquisition meter and caller context continue through the engine's single
// mandatory encoding gate, parser and callbacks; no adapter decoding scan occurs.
func acquireLintContext(ctx context.Context, inv *lintInvocation, _ io.Writer) int {
	limits := parser.Limits{
		DocumentBytes: inv.limit("document-bytes"), SyntaxDepth: inv.limit("syntax-depth"),
		DocumentNodes: inv.limit("document-nodes"), TotalNodes: inv.limit("total-nodes"),
		WorkUnits: inv.limit("work-units"), OperationTime: time.Duration(inv.limit("operation-time")),
	}
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		return inv.fail(err, "unsupported-limits", result.Usage)
	}
	if ctx == nil {
		return inv.fail(errors.New("nil execution context"), "invalid-context", result.Internal)
	}
	ctx, cancel := context.WithTimeout(ctx, limits.OperationTime)
	defer cancel()
	selected, err := files.Select(files.Options{WorkingDirectory: inv.workingDirectory, ExclusionBase: inv.exclusionBase, Paths: inv.options.paths, Exclusions: inv.exclusions})
	if err != nil {
		return inv.fail(err, "selection-failed", result.IO)
	}
	inv.targetSelection = selected
	if status, rejected := inv.selectionFeasibility(selected); rejected {
		return status
	}
	acquired, err := files.Acquire(ctx, selected, files.AcquireOptions{DocumentBytes: limits.DocumentBytes, TotalContentBytes: inv.limit("total-content-bytes"), Budget: budget})
	if err != nil {
		return inv.fail(err, "invalid-acquisition", result.Usage)
	}
	inv.acquisition = acquired
	var inputs []engine.Input
	// A known oversized input is a batch preflight rejection: no parser or rule
	// callback starts. Independent runtime failures otherwise retain good work.
	if len(acquired.PreflightErrors) == 0 {
		inputs = make([]engine.Input, 0, len(acquired.Documents))
		for _, document := range acquired.Documents {
			in, err := inv.plan.NewInput(inv.logicalPath(document.Path), document.Source.Bytes())
			if err != nil {
				return inv.fail(err, "invalid-input", result.Internal)
			}
			inputs = append(inputs, in)
		}
	}
	out, err := inv.plan.Lint(ctx, inputs, engine.RunOptions{Mode: "cli", Budget: budget})
	if err != nil {
		return inv.fail(err, "execution-failed", result.Internal)
	}
	inv.mergeAcquisition(&out, selected, acquired)
	engine.FinalizeLintResult(&out)
	inv.result = &out
	return lintExit(out)
}

func (inv *lintInvocation) logicalPath(path string) string {
	if path == "" {
		return ""
	}
	rel, err := filepath.Rel(inv.workingDirectory, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// mergeAcquisition adds provider observations before the authoritative engine
// finalizer reassigns IDs, resolves causes and derives file references/outcome.
// Native acquisition names stay private; all public records use cwd-relative
// paths, exactly as the engine inputs do.
func (inv *lintInvocation) mergeAcquisition(out *result.Result, selected *files.Selection, acquired *files.Acquisition) {
	source := "default"
	if inv.limit("total-content-bytes") != limitDefaults["total-content-bytes"] {
		source = "operator"
	}
	out.EffectiveLimits = append(out.EffectiveLimits, result.EffectiveLimit{Name: "total-content-bytes", Value: inv.limit("total-content-bytes"), Unit: "bytes", Source: source})
	slices.SortFunc(out.EffectiveLimits, func(a, b result.EffectiveLimit) int { return cmp.Compare(a.Name, b.Name) })

	for _, skip := range selected.Skipped {
		out.SkippedFiles = append(out.SkippedFiles, result.SkippedFile{Path: inv.logicalPath(skip.Path), Reason: string(skip.Reason)})
	}
	addErrors := func(failures []*fs.PathError, category result.ErrorCategory, code string) {
		for _, failure := range failures {
			path := inv.logicalPath(failure.Path)
			id := fmt.Sprintf("acquisition-error-%d", len(out.Errors)+1)
			out.Errors = append(out.Errors, result.Error{ID: id, Category: category, Code: code, Scope: "operation", Path: path, Message: failure.Op + ": " + failure.Err.Error()})
			out.Coverage = append(out.Coverage, result.Coverage{Path: path, Reason: code, CauseIDs: []string{id}, Message: "Target processing could not complete."})
		}
	}
	addErrors(selected.Errors, result.IO, "selection-failed")
	addErrors(acquired.Errors, result.IO, "acquisition-failed")
	addErrors(acquired.PreflightErrors, result.Usage, "input-too-large")
	for _, stop := range acquired.Interruptions {
		reason := "limit-exceeded"
		if errors.Is(stop.Err, context.Canceled) {
			reason = "cancelled"
		}
		if errors.Is(stop.Err, context.DeadlineExceeded) {
			reason = "deadline-exceeded"
		}
		record := result.Interruption{ID: fmt.Sprintf("acquisition-interruption-%d", len(out.Interruptions)+1), Path: inv.logicalPath(stop.Path), Reason: reason, Phase: "acquisition", Message: stop.Err.Error()}
		var limit *parser.LimitExceeded
		if errors.As(stop.Err, &limit) {
			unit := "metered work units"
			if limit.Name == "document-bytes" || limit.Name == "total-content-bytes" {
				unit = "bytes"
			}
			if limit.Name == "operation-time" {
				unit = "nanoseconds"
			}
			record.Limit = &result.LimitObservation{Name: limit.Name, Observed: limit.Observed, Allowed: limit.Allowed, Unit: unit}
		}
		out.Interruptions = append(out.Interruptions, record)
		out.Coverage = append(out.Coverage, result.Coverage{Path: record.Path, Reason: reason, CauseIDs: []string{record.ID}, Message: "Source acquisition was interrupted."})
	}
	known := make(map[string]bool, len(out.Files))
	for _, file := range out.Files {
		known[file.Path] = true
	}
	// A selection failure names an observed/requested entry, even when no
	// readable Target could be constructed. Retain that entry's failed outcome;
	// never infer any children of it.
	for _, failure := range selected.Errors {
		path := inv.logicalPath(failure.Path)
		if known[path] {
			continue
		}
		out.Files = append(out.Files, result.File{Path: path, Analysis: "failed", WriteStatus: "not-requested", ReturnStatus: "not-requested", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
		known[path] = true
	}
	for _, target := range selected.Targets {
		path := inv.logicalPath(target.Path)
		if known[path] {
			continue
		}
		out.Files = append(out.Files, result.File{Path: path, Analysis: "not-started", WriteStatus: "not-requested", ReturnStatus: "not-requested", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}})
		// Every known unfinished target remains explicit without inventing children
		// of pruned directories. Error-bearing files become failed in finalization.
		out.Coverage = append(out.Coverage, result.Coverage{Path: path, Reason: "not-started", Message: "Target validation did not start."})
	}
	if selected.Empty() && len(out.Errors) == 0 && len(out.Interruptions) == 0 {
		out.Errors = append(out.Errors, result.Error{Category: result.Usage, Code: "no-files", Scope: "operation", Message: "no files selected"})
	}
}

func lintExit(out result.Result) int {
	status := 0
	if len(out.Diagnostics) > 0 {
		status = 10
	}
	if !out.Complete {
		status = exitIncomplete
	}
	// Usage errors here are preflight/empty selection only. Runtime failures
	// retain semantic precedence, including IO over an empty selection.
	for _, e := range out.Errors {
		if e.Category == result.Configuration {
			status = exitConfig
		}
		if e.Category == result.Usage {
			status = exitUsage
		}
	}
	for _, e := range out.Errors {
		if e.Category == result.IO {
			status = exitIO
		}
	}
	for _, e := range out.Errors {
		if e.Category == result.Internal {
			status = 14
		}
	}
	return status
}
