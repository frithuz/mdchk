package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

const fixHelp = `Usage: mdchk fix [options] [paths...]

Apply validated corrections using verified native per-file replacement.
Use --dry-run to preview without acquiring write locks or creating stages.
Options may appear before or after paths. Use -- for dash-prefixed paths.
  --dry-run           Report proposals without saving
  --config PATH       Select configuration relative to the invocation directory
  --format FORMAT     text (default) or json
  --limit NAME=VALUE  Set a finite applicable resource limit
  --help              Show this help
  --version           Print version

Exit 0/10 describes saved content (candidate content for --dry-run).
Incomplete analysis returns 15; apply requires Darwin arm64+cgo on local APFS.
Usage returns 11, configuration 12, I/O 13 and internal failures 14.
MD047 corrects safe final-newline changes; ambiguous contexts remain manual.
`

func parseFix(args []string) (lintOptions, bool, error) {
	opts, err := parseCommandOptionsWithFix(args, true, false, true)
	dry := opts.dryRun
	if err == nil && reportLimit(opts.limits, "document-bytes") > reportLimit(opts.limits, "total-content-bytes") {
		err = errors.New("document-bytes must not exceed total-content-bytes")
	}
	for name := range opts.limits {
		if !inspectLimitApplicable(name) && err == nil {
			err = fmt.Errorf("limit %q does not apply to fix", name)
		}
	}
	return opts, dry, err
}
func fixFailure(err error, code string, category result.ErrorCategory, dry bool, format string, w io.Writer, limits map[string]int64) int {
	out := failureResult(err, code, category)
	out.Operation = "fix"
	out.DryRun = &dry
	return reportResult(out, format, w, limits)
}
func runFix(ctx context.Context, args []string, w io.Writer, version string) int {
	opts, dry, err := parseFix(args)
	if err != nil {
		return fixFailure(err, "invalid-arguments", result.Usage, dry, recoverFormat(args), w, nil)
	}
	if opts.help {
		return output(w, fixHelp, 0)
	}
	if opts.version {
		return output(w, "mdchk "+version+"\n", 0)
	}
	if opts.format != "json" && opts.format != "text" {
		return fixFailure(errors.New("unsupported format; use text or json"), "unsupported-format", result.Usage, dry, "text", w, opts.limits)
	}
	profile := parser.Limits{DocumentBytes: reportLimit(opts.limits, "document-bytes"), SyntaxDepth: reportLimit(opts.limits, "syntax-depth"), DocumentNodes: reportLimit(opts.limits, "document-nodes"), TotalNodes: reportLimit(opts.limits, "total-nodes"), WorkUnits: reportLimit(opts.limits, "work-units"), OperationTime: time.Duration(reportLimit(opts.limits, "operation-time"))}
	budget, err := parser.NewOperationBudget(profile)
	if err == nil && (reportLimit(opts.limits, "structured-input-depth") > 4096 || reportLimit(opts.limits, "total-config-bytes") > 1<<30) {
		err = errors.New("unsupported configuration limits")
	}
	if err != nil {
		return fixFailure(err, "unsupported-limits", result.Usage, dry, opts.format, w, opts.limits)
	}
	if err = fixFeasibility(result.NewFix(true), opts.format, opts.limits); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return fixFailure(err, "response-manifest-too-large", result.Usage, dry, opts.format, w, opts.limits)
	}
	if ctx == nil {
		return fixFailure(errors.New("nil execution context"), "invalid-context", result.Internal, dry, opts.format, w, opts.limits)
	}
	ctx, cancel := context.WithTimeout(ctx, profile.OperationTime)
	defer cancel()
	registry, providers, caps, err := configuredProviders()
	if err != nil {
		return fixFailure(err, "provider-initialization", result.Internal, dry, opts.format, w, opts.limits)
	}
	validator, produce, err := configuredCorrections(registry)
	if err != nil {
		return fixFailure(err, "correction-initialization", result.Internal, dry, opts.format, w, opts.limits)
	}
	out := executeFix(ctx, opts, budget, registry, providers, caps, validator, produce)
	return reportResult(out, opts.format, w, opts.limits)
}

func executeFix(ctx context.Context, opts lintOptions, budget *parser.OperationBudget, registry *engine.Registry, providers engine.ValidationProviders, caps config.Capabilities, validator *fix.Validator, produce fix.Producer) (out result.Result) {
	out = result.NewFix(opts.dryRun)
	defer func() { setInspectionLimits(&out, opts.limits); engine.FinalizeLintResult(&out) }()
	fail := func(err error, code string, category result.ErrorCategory) result.Result {
		if isInspectInterruption(err) {
			out.Interruptions = append(out.Interruptions, inspectInterruption(err, "configuration"))
			if category == result.IO {
				_, effect := publicationFailureKinds(err)
				if effect {
					out.Errors = append(out.Errors, result.Error{Category: result.IO, Code: code, Scope: "operation", Message: err.Error()})
				}
			}
			return out
		}
		r := failureResult(err, code, category)
		r.Operation = "fix"
		r.DryRun = out.DryRun
		return r
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err, "working-directory", result.IO)
	}
	selectedConfig, err := config.ResolveValidatedBounded(config.SelectionOptions{WorkingDirectory: cwd, ExplicitPath: opts.config}, registry, providers, config.DiscoveryBudget{Context: ctx, Budget: budget, SourceBytes: reportLimit(opts.limits, "config-bytes"), TotalBytes: reportLimit(opts.limits, "total-config-bytes"), StructuredDepth: int(reportLimit(opts.limits, "structured-input-depth"))})
	if err != nil {
		return fail(err, "config-definition", result.Configuration)
	}
	resolution, err := selectedConfig.Admit(caps)
	if err != nil {
		return fail(err, "capability-unavailable", result.Configuration)
	}
	selectedConfig.Resolution = resolution
	base := cwd
	if selectedConfig.SelectedPath != "" {
		base = filepath.Dir(selectedConfig.SelectedPath)
	}
	inv := &lintInvocation{options: opts, selection: selectedConfig, plan: resolution.ExecutionPlan(), workingDirectory: cwd, exclusionBase: base, exclusions: resolution.Exclude}
	selection, err := files.SelectBounded(ctx, files.Options{WorkingDirectory: cwd, ExclusionBase: base, Paths: opts.paths, Exclusions: resolution.Exclude, Quarto: inv.plan.Configuration().Settings.QuartoEnabled}, budget)
	if err != nil {
		return fail(err, "selection-failed", result.IO)
	}
	acquisition := &files.Acquisition{}
	// Typed discovery interruptions retain independent known targets/errors.
	ordinary := selection.Errors[:0]
	for _, e := range selection.Errors {
		if isInspectInterruption(e) {
			acquisition.Interruptions = append(acquisition.Interruptions, files.Interruption{Path: e.Path, Err: e})
		} else {
			ordinary = append(ordinary, e)
		}
	}
	selection.Errors = ordinary
	manifest := result.NewFix(opts.dryRun)
	inv.mergeAcquisition(&manifest, selection, acquisition)
	engine.FinalizeLintResult(&manifest)
	if err := fixFeasibility(reserveFixManifest(manifest), opts.format, opts.limits); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		if !errors.Is(err, errReportBudget) {
			return fail(err, "manifest-failed", result.Internal)
		}
		count := len(manifest.Files)
		coverage := len(manifest.Coverage)
		manifest.Files = []result.File{}
		manifest.Coverage = []result.Coverage{}
		manifest.Omissions = append(manifest.Omissions, result.Omission{Section: "/files", Reason: "output-limit", Message: "discovered manifest cannot fit; acquisition and validation did not start", Count: &count}, result.Omission{Section: "/coverage", Reason: "output-limit", Message: "unstarted target coverage omitted", Count: &coverage})
		manifest.Interruptions = append(manifest.Interruptions, result.Interruption{Reason: "limit-exceeded", Phase: "discovery", Message: "mandatory target manifest exceeds response-bytes"})
		return manifest
	}
	// Discovery already owns the terminal outcome when there is no target.
	// In particular, no native write authority is needed for empty work.
	if selection.Empty() {
		return manifest
	}
	var native *files.WriteSet
	var acquired *files.Acquisition
	var resources []fix.ResourceObservation
	acquireOptions := files.AcquireOptions{DocumentBytes: budget.Limits().DocumentBytes, TotalContentBytes: reportLimit(opts.limits, "total-content-bytes"), Budget: budget}
	if opts.dryRun {
		acquired, err = files.Acquire(ctx, selection, acquireOptions)
	} else {
		native, acquired, resources, err = acquireFixWrite(ctx, inv, selection, acquireOptions)
	}
	if err != nil {
		return fail(err, "write-acquisition", result.IO)
	}
	if native != nil {
		defer func() {
			if closeErr := native.Close(); closeErr != nil {
				out.Errors = append(out.Errors, result.Error{Category: result.IO, Code: "write-shutdown", Scope: "operation", Message: closeErr.Error()})
			}
		}()
	}

	acquired.Interruptions = append(acquired.Interruptions, acquisition.Interruptions...)
	targets := make([]fix.Target, 0, len(acquired.Documents))
	if len(acquired.PreflightErrors) == 0 {
		for _, doc := range acquired.Documents {
			targets = append(targets, fix.Target{Path: inv.logicalPath(doc.Path), Source: doc.Source, Plan: inv.plan.CandidatePlan(), Produce: produce})
		}
	}
	var validated fix.Validated
	if len(targets) > 0 {
		validated, err = validator.Validate(ctx, targets, fix.ValidationOptions{Budget: budget, TotalContentBytes: reportLimit(opts.limits, "total-content-bytes"), Resources: resources})
	}
	if err != nil {
		if isInspectInterruption(err) {
			out.Interruptions = append(out.Interruptions, validationStop(err))
		} else {
			category := result.Internal
			var failure *fix.AnalysisFailure
			if errors.As(err, &failure) {
				category = failure.Category
			}
			out.Errors = append(out.Errors, result.Error{ID: "fix-validation-cause", Category: category, Code: "validation-failed", Scope: "operation", Message: err.Error()})
		}
	}
	var applied map[string]fixApplied
	if !opts.dryRun {
		applied = publishFix(ctx, inv, budget, native, acquired, validated, err, &out)
	}

	for _, doc := range acquired.Documents {
		path := inv.logicalPath(doc.Path)
		original := sourceIdentity(doc.Source, "original")
		file := result.File{Path: path, Analysis: "not-started", WriteStatus: "not-needed", ReturnStatus: "not-requested", InputSnapshot: &original, ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}}
		if view, ok := validated.File(path); ok && view.AnalysisStarted {
			analysis := view.Analysis
			savedAvailable := true
			if !opts.dryRun {
				state := applied[path]
				savedAvailable = state.observed
				analysis = state.analysis
			}
			if savedAvailable {
				mergeFixAnalysis(&out, analysis)
			}
			candidate := sourceIdentity(view.Candidate, "candidate")
			if opts.dryRun {
				file.ResultSnapshot = &candidate
			} else {
				file.ResultSnapshot = applied[path].snapshot
			}
			file.Analysis = "complete"
			if !savedAvailable {
				file.Analysis = "not-started"
			} else if !analysis.Complete {
				file.Analysis = "partial"
			}
			if !opts.dryRun && !applied[path].analysisStarted {
				file.Analysis = "not-started"
			}
			if savedAvailable && (opts.dryRun || applied[path].analysisStarted) {
				out.CheckedFiles = append(out.CheckedFiles, path)
			}
			for _, reason := range view.Reasons {
				out.Coverage = append(out.Coverage, result.Coverage{Path: path, Reason: reason, Message: "Correction computation withheld this component; retained content is reported."})
			}
			for _, obs := range view.Observations {
				if obs.Source == view.Candidate && len(obs.Analysis.Errors) == 0 && len(obs.Analysis.Interruptions) == 0 {
					continue
				}
				// Rejected provisional observations retain their actual source identity;
				// they do not establish a final candidate or authorize a change.
				mergeFixObservation(&out, obs.Analysis)
				snap := sourceIdentity(obs.Source, "candidate")
				raw, _ := json.Marshal(result.Note{Code: "provisional-observation", Path: path, Snapshot: &snap, Message: "Reliable observations from rejected provisional content; this snapshot is not the retained candidate."})
				out.Notes = append(out.Notes, raw)
			}
			if deferredErr := appendFixDecisions(ctx, budget, &out, view, validationCause(err), validationReason(err)); deferredErr != nil {
				out.Interruptions = append(out.Interruptions, inspectInterruption(deferredErr, "planning"))
			}
			if original.SHA256 != candidate.SHA256 {
				change := result.Change{ID: fmt.Sprintf("change-%d", len(out.Changes)+1), Path: path, Disposition: "proposed", Before: original, Candidate: candidate, Rules: []string{}}
				for _, attempt := range view.Attempts {
					type editKey struct {
						start, end  int
						replacement string
					}
					applied := map[editKey]bool{}
					for _, decision := range attempt.Decisions {
						contributes := false
						if decision.Accepted {
							for _, edit := range decision.Group.Edits {
								if bytes.Equal(edit.Expected, edit.Replacement) {
									continue
								}
								contributes = true
								key := editKey{edit.Span.Start(), edit.Span.End(), string(edit.Replacement)}
								if !applied[key] {
									applied[key] = true
									change.EditCount++
								}
							}
							if contributes {
								change.Rules = append(change.Rules, decision.Group.Rule)
							}
						}
					}
				}
				slices.Sort(change.Rules)
				change.Rules = slices.Compact(change.Rules)
				diff, diffErr := buildFixDiff(ctx, budget, doc.Source, view.Candidate)
				if diffErr != nil {
					out.Interruptions = append(out.Interruptions, inspectInterruption(diffErr, "planning"))
					count := 1
					out.Omissions = append(out.Omissions, result.Omission{Section: fmt.Sprintf("/changes/%d/diff", len(out.Changes)), Path: path, Reason: "output-limit", Message: "Final diff was interrupted; validated proposal identities are retained.", Count: &count})
				} else {
					change.Diff = &diff
				}
				file.WriteStatus = "proposed"
				if !opts.dryRun {
					file.WriteStatus = applied[path].status
					if file.WriteStatus == "written" {
						change.Disposition = "written"
					} else if file.WriteStatus == "unknown" {
						change.Disposition = "uncertain"
					}
				}
				if change.ID != "" {
					file.ChangeIDs = []string{change.ID}
					out.Changes = append(out.Changes, change)
				}
			}
		}
		if !opts.dryRun {
			file.WriteStatus = applied[path].status
			file.Leftovers = applied[path].leftovers
			if file.WriteStatus == "" {
				file.WriteStatus = "not-needed"
			}
		}
		if file.Analysis == "not-started" {
			out.Coverage = append(out.Coverage, result.Coverage{Path: path, Reason: "not-started", Message: "Acquired target validation did not start or retain an analysis."})
		}
		out.Files = append(out.Files, file)
	}

	if len(targets) > 0 && !validated.Complete() && err == nil {
		out.Coverage = append(out.Coverage, result.Coverage{Reason: "fix-incomplete", Message: "Correction validation did not complete."})
	}
	inv.mergeAcquisition(&out, selection, acquired)
	// Result reconciliation cannot silently outrun the same operation deadline.
	// Already validated identities and observations remain reliable on expiry.
	stop := ctx.Err()
	if stop == nil {
		stop = budget.ChargeWork(int64(len(out.Files)+len(out.Changes)+len(out.DeferredFixes)) + 1)
	}
	if stop != nil {
		out.Interruptions = append(out.Interruptions, inspectInterruption(stop, "rechecking"))
	}
	return out
}
func sourceIdentity(source parser.Source, view string) result.Snapshot {
	return result.Snapshot{View: view, SHA256: fmt.Sprintf("%x", sha256.Sum256(source.Bytes())), SizeBytes: source.Len()}
}
func mergeFixAnalysis(out *result.Result, in result.Result) {
	prefix := fmt.Sprintf("fix-%d-%d-", len(out.Errors), len(out.Interruptions))
	causes := map[string]string{}
	for _, e := range in.Errors {
		causes[e.ID] = prefix + e.ID
		e.ID = causes[e.ID]
		out.Errors = append(out.Errors, e)
	}
	for _, stop := range in.Interruptions {
		causes[stop.ID] = prefix + stop.ID
		stop.ID = causes[stop.ID]
		out.Interruptions = append(out.Interruptions, stop)
	}
	for _, c := range in.Coverage {
		c.CauseIDs = slices.Clone(c.CauseIDs)
		for i, id := range c.CauseIDs {
			if renamed := causes[id]; renamed != "" {
				c.CauseIDs[i] = renamed
			}
		}
		out.Coverage = append(out.Coverage, c)
	}
	out.Diagnostics = append(out.Diagnostics, in.Diagnostics...)
	out.Notes = append(out.Notes, in.Notes...)
	out.SuppressedDiagnostics = append(out.SuppressedDiagnostics, in.SuppressedDiagnostics...)
}
func appendFixDecisions(ctx context.Context, budget *parser.OperationBudget, out *result.Result, view fix.FileView, cause, runtimeReason string) error {
	rollback := ""
	if view.Original == view.Candidate && len(view.Reasons) > 0 {
		rollback = view.Reasons[0]
		for _, reason := range view.Reasons {
			if reason == "fix-nonconvergent" {
				rollback = reason
				break
			}
		}
	}
	if view.RuntimeRollback && runtimeReason != "" {
		rollback = runtimeReason
	}
	reasons := func(decision fix.Decision) []fix.Reason {
		if decision.Accepted {
			if rollback != "" {
				return []fix.Reason{{Code: rollback}}
			}
			return nil
		}
		return decision.Reasons
	}
	records := []result.DeferredFix{}
	total := 0
	for _, attempt := range view.Attempts {
		for _, decision := range attempt.Decisions {
			total += len(reasons(decision))
		}
	}
	omit := func(err error) error {
		if total > 0 {
			out.Omissions = append(out.Omissions, result.Omission{Section: "/deferred_fixes", Path: view.Path, Reason: "output-limit", Message: "whole deferred correction records omitted after shared operation interruption", Count: &total})
		}
		return err
	}
	for _, attempt := range view.Attempts {
		count := 0
		for _, decision := range attempt.Decisions {
			count += len(reasons(decision))
		}
		if count == 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return omit(err)
		}
		if err := budget.ChargeWork(int64(attempt.Source.Len())*4 + int64(count)*16 + 1); err != nil {
			return omit(err)
		}
		span, _ := attempt.Source.Span(0, attempt.Source.Len())
		text, _ := attempt.Source.Text(span)
		linesCount := 0
		for i := 0; i < len(text); i++ {
			if text[i] == '\r' {
				linesCount++
				if i+1 < len(text) && text[i+1] == '\n' {
					i++
				}
			} else if text[i] == '\n' {
				linesCount++
			}
		}
		if len(text) > 0 && text[len(text)-1] != '\r' && text[len(text)-1] != '\n' {
			linesCount++
		}
		if err := budget.ChargeNodes(int64(linesCount) + int64(count)*4 + 1); err != nil {
			return omit(err)
		}
		lines := parser.NewLineIndex(attempt.Source)
		snapshot := attemptIdentity(view, attempt)
		for _, decision := range attempt.Decisions {
			for _, reason := range reasons(decision) {
				a, err := lines.PositionBounded(ctx, budget, decision.Group.Anchor.Start())
				if err != nil {
					return omit(err)
				}
				b, err := lines.PositionBounded(ctx, budget, decision.Group.Anchor.End())
				if err != nil {
					return omit(err)
				}
				if err := budget.ChargeWork(int64(len(reason.Code)+len(decision.Group.ID)+len(view.Path)) + int64(len(reason.Related))*8 + 1); err != nil {
					return omit(err)
				}
				records = append(records, result.DeferredFix{ID: fmt.Sprintf("deferred-%d", len(out.DeferredFixes)+len(records)+1), Path: view.Path, Rule: decision.Group.Rule, GroupID: fmt.Sprintf("round-%d:%s", attempt.Round, decision.Group.ID), Reason: reason.Code, Message: "Atomic correction group deferred: " + reason.Code, Range: &result.Range{Start: a, End: b}, Snapshot: snapshot, ConflictGroupIDs: roundRelated(attempt.Round, reason.Related), CauseIDs: deferredRuntimeCauses(decision.Accepted, view.RuntimeRollback, cause)})
			}
		}
	}
	out.DeferredFixes = append(out.DeferredFixes, records...)
	return nil
}

func attemptIdentity(view fix.FileView, attempt fix.Attempt) result.Snapshot {
	name := "candidate"
	if attempt.Source == view.Original {
		name = "original"
	}
	return sourceIdentity(attempt.Source, name)
}
func roundRelated(round int, ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = fmt.Sprintf("round-%d:%s", round, id)
	}
	return out
}

// Admission is operation work, independent of the later reporting timer. Reserve
// the full worst-case per-target identities and proposed-change metadata before
// loading bytes; optional diff/detail families use the separate reporting reserve.
func fixFeasibility(out result.Result, format string, limits map[string]int64) error {
	admission := make(map[string]int64, len(limits)+1)
	for k, v := range limits {
		admission[k] = v
	}
	admission["reporting-time"] = limitDefaults["reporting-time"]
	return reportFeasibility(out, format, admission)
}
func reserveFixManifest(out result.Result) result.Result {
	out = cloneReport(out)
	rules := []string{}
	for _, m := range engine.Catalog() {
		rules = append(rules, m.ID)
	}
	for i := range out.Files {
		f := &out.Files[i]
		if f.Analysis == "failed" {
			continue
		}
		original := result.Snapshot{View: "original", SHA256: strings.Repeat("a", 64), SizeBytes: 1 << 30}
		candidate := original
		candidate.View = "candidate"
		f.InputSnapshot = &original
		f.ResultSnapshot = &candidate
		f.WriteStatus = "proposed"
		id := fmt.Sprintf("change-%d", i+1)
		f.ChangeIDs = []string{id}
		out.CheckedFiles = append(out.CheckedFiles, f.Path)
		out.Changes = append(out.Changes, result.Change{ID: id, Path: f.Path, Disposition: "proposed", Before: original, Candidate: candidate, Rules: rules, EditCount: int(^uint(0) >> 1)})
		if out.DryRun != nil && !*out.DryRun {
			saved := candidate
			saved.View = "saved"
			f.ResultSnapshot = &saved
			f.WriteStatus = "written"
			f.Leftovers = []string{strings.Repeat("x", 255)}
			out.Changes[len(out.Changes)-1].Disposition = "written"
			out.Errors = append(out.Errors, result.Error{ID: fmt.Sprintf("write-error-%d", i), Path: f.Path, Category: result.IO, Code: "publication-failed", Scope: "operation", Message: strings.Repeat("e", 1024)})
			out.Coverage = append(out.Coverage, result.Coverage{Path: f.Path, Reason: "saved-view-unavailable", Message: strings.Repeat("c", 256)})
		}
	}
	return out
}

// Reliable provisional findings are explicit located observations, not remaining
// findings in the retained candidate. Notes retain their original identities and
// coordinates without inflating candidate diagnostic counts or inventing a final
// result_snapshot for rejected content.
func mergeFixObservation(out *result.Result, observation result.Result) {
	diagnostics, suppressed, notes := observation.Diagnostics, observation.SuppressedDiagnostics, observation.Notes
	observation.Diagnostics = nil
	observation.SuppressedDiagnostics = nil
	observation.Notes = nil
	mergeFixAnalysis(out, observation)
	add := func(d result.Diagnostic, code, message string) {
		snapshot := d.Snapshot
		location := d.Range
		bytes := d.ByteRange
		raw, _ := json.Marshal(result.Note{Code: code, Path: d.Path, Rule: d.Rule, Snapshot: &snapshot, Range: &location, ByteRange: &bytes, Message: fmt.Sprintf("Provisional %s %s (%s): %s", snapshot.View, snapshot.SHA256, d.Severity, message)})
		out.Notes = append(out.Notes, raw)
	}
	for _, d := range diagnostics {
		add(d, "provisional-finding", d.Message)
	}
	for _, raw := range suppressed {
		var d result.SuppressedDiagnostic
		_ = json.Unmarshal(raw, &d)
		add(d.Diagnostic, "provisional-suppressed-finding", d.SuppressedBy.Kind+": "+d.Diagnostic.Message)
	}
	for _, raw := range notes {
		var n result.Note
		_ = json.Unmarshal(raw, &n)
		n.ID = ""
		n.Message = "Provisional observation: " + n.Message
		owned, _ := json.Marshal(n)
		out.Notes = append(out.Notes, owned)
	}
}

func validationCause(err error) string {
	if err != nil {
		return "fix-validation-cause"
	}
	return ""
}
func validationStop(err error) result.Interruption {
	stop := inspectInterruption(err, "rechecking")
	stop.ID = "fix-validation-cause"
	return stop
}
func deferredCauses(cause string) []string {
	if cause == "" {
		return nil
	}
	return []string{cause}
}

func validationReason(err error) string {
	if err == nil {
		return ""
	}
	if isInspectInterruption(err) {
		return inspectInterruption(err, "rechecking").Reason
	}
	return "validation-failed"
}

func deferredRuntimeCauses(accepted, runtimeRollback bool, cause string) []string {
	if accepted && runtimeRollback {
		return deferredCauses(cause)
	}
	return nil
}
