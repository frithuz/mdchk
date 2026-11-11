package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/report"
	"github.com/frithuz/mdchk/internal/result"
)

func (inv *lintInvocation) limit(name string) int64 {
	if value, ok := inv.options.limits[name]; ok {
		return value
	}
	return report.LimitDefaults[name]
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
	if inv.limit("total-content-bytes") != report.LimitDefaults["total-content-bytes"] {
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

// selectedConfig is the already resolved configuration filename, or empty.
func (inv *lintInvocation) selectedConfig() string {
	if inv.selection == nil {
		return ""
	}
	return inv.selection.SelectedPath
}
