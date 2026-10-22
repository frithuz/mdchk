package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Acquisition keeps the exact analyzed source authority and binds all governing
// configuration reads and absences before candidate construction begins.
func acquireFixWrite(ctx context.Context, inv *lintInvocation, selected *files.Selection, options files.AcquireOptions) (*files.WriteSet, *files.Acquisition, []fix.ResourceObservation, error) {
	baselines := map[string]files.WriteBaseline{}
	var paths []string
	for _, source := range inv.selection.Sources() {
		paths = append(paths, source.Path)
		baselines[source.Path] = files.WriteBaseline{Info: source.FileInfo(), SHA256: source.SHA256, SizeBytes: source.SizeBytes}
	}
	resourceSelection := &files.Selection{}
	var err error
	if len(paths) > 0 {
		resourceSelection, err = files.SelectBounded(ctx, files.Options{WorkingDirectory: inv.workingDirectory, Paths: paths}, options.Budget)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(resourceSelection.Errors) > 0 {
			return nil, nil, nil, resourceSelection.Errors[0]
		}
		if len(resourceSelection.Targets) != len(paths) {
			return nil, nil, nil, errors.New("governing configuration is unavailable for native observation")
		}
	}
	required := map[string][]string{}
	absent := inv.selection.Absences()
	deps := append(append([]string(nil), paths...), absent...)
	for _, target := range selected.Targets {
		required[target.Path] = deps
	}
	native, acquired, resources, err := files.AcquireWrite(ctx, selected, files.WriteOptions{AcquireOptions: options, Resources: resourceSelection, ResourceDocumentBytes: reportLimit(inv.options.limits, "config-bytes"), Absences: absent, Required: required, Baselines: baselines})
	if err != nil {
		return nil, nil, nil, err
	}
	if resources != nil && len(resources.Errors)+len(resources.PreflightErrors)+len(resources.Interruptions) > 0 {
		var causes []error
		for _, failure := range resources.Errors {
			causes = append(causes, failure)
		}
		for _, failure := range resources.PreflightErrors {
			causes = append(causes, failure)
		}
		for _, stop := range resources.Interruptions {
			causes = append(causes, stop.Err)
		}
		causes = append(causes, native.Close())
		return nil, nil, nil, errors.Join(causes...)
	}
	// Configuration authority belongs to the admitted plan and native Required
	// manifest, not Markdown resource callbacks or the document-byte quota.

	return native, acquired, nil, nil
}

type fixApplied struct {
	status          string
	leftovers       []string
	saved           parser.Source
	observed        bool
	analysis        result.Result
	analysisStarted bool
	snapshot        *result.Snapshot
}

// This fixed production entry is also the narrow build-time boundary used by
// compiled lifecycle controls. There is no environment or runtime fixture mode.
func publishFixTarget(ctx context.Context, validated fix.Validated, native *files.WriteSet, path string, names map[string]string) files.Publication {
	return validated.PublishMapped(ctx, native, path, names)
}

func publishFix(ctx context.Context, inv *lintInvocation, budget *parser.OperationBudget, native *files.WriteSet, acquired *files.Acquisition, validated fix.Validated, validationErr error, out *result.Result) map[string]fixApplied {
	return publishFixUsing(ctx, inv, budget, native, acquired, validated, validationErr, out, publishFixTarget)
}

type fixPublisher func(context.Context, fix.Validated, *files.WriteSet, string, map[string]string) files.Publication

func publishFixUsing(ctx context.Context, inv *lintInvocation, budget *parser.OperationBudget, native *files.WriteSet, acquired *files.Acquisition, validated fix.Validated, validationErr error, out *result.Result, publish fixPublisher) map[string]fixApplied {
	states := map[string]fixApplied{}
	content, ledgerErr := files.NewContentLedger(reportLimit(inv.options.limits, "total-content-bytes"))
	if ledgerErr != nil {
		out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "saved-content-ledger", Scope: "operation", Message: ledgerErr.Error()})
		return states
	}
	names := map[string]string{}
	for _, doc := range acquired.Documents {
		names[inv.logicalPath(doc.Path)] = doc.Path
	}
	// All proposals were validated against the complete overlay before this loop.
	// A proof using another changed candidate cannot be published per-file. Defer
	// such targets conservatively, independent of traversal order and write errors.
	stopped := validationErr != nil || len(acquired.PreflightErrors) > 0
	for _, doc := range acquired.Documents {
		path := inv.logicalPath(doc.Path)
		state := fixApplied{status: "not-needed"}
		view, ok := validated.File(path)
		if !ok || !view.AnalysisStarted {
			states[path] = state
			continue
		}
		changed := view.Original != view.Candidate
		if changed {
			state.status = "refused"
			coupled, coupleErr := validated.ChangedDependency(ctx, path)
			if coupleErr != nil {
				stopped = true
				out.Interruptions = append(out.Interruptions, inspectInterruption(coupleErr, "writing"))
			}
			switch {
			case coupled:
				out.Coverage = append(out.Coverage, result.Coverage{Path: path, Reason: "coupled-publication-deferred", Message: "Validated proof depends on another changed candidate; no atomic multi-file publication is available."})
			case stopped:
				state.status = "cancelled"
			default:
				if stop := native.Checkpoint(ctx); stop != nil {
					stopped = true
					state.status = "cancelled"
					out.Interruptions = append(out.Interruptions, inspectInterruption(stop, "writing"))
				} else if stop := content.Check(int64(view.Candidate.Len())); stop != nil {
					stopped = true
					state.status = "cancelled"
					out.Interruptions = append(out.Interruptions, inspectInterruption(stop, "writing"))
				} else {
					publication := publish(ctx, validated, native, path, names)
					if publication.HardLinks > 1 && publication.Published {
						raw, _ := json.Marshal(result.Note{Code: "hard-link-replacement", Path: path, Message: fmt.Sprintf("Original link count %d: replacement changes only this directory entry; other aliases retain the original inode/content unless separately replaced.", publication.HardLinks)})
						out.Notes = append(out.Notes, raw)
					}
					if publication.Published {
						state.status = "written"
					} else if publication.Uncertain {
						state.status = "unknown"
					} else if publication.Started {
						state.status = "failed"
					}
					if publication.Saved != (parser.Source{}) {
						stop := content.Reserve(int64(publication.Saved.Len()))
						snapshot := observedSavedIdentity(publication.Saved)
						state.snapshot = &snapshot
						if stop != nil {
							stopped = true
							out.Interruptions = append(out.Interruptions, inspectInterruption(stop, "rechecking"))
						} else {
							state.saved = publication.Saved
							state.observed = true
						}
					}
					if publication.Err != nil {
						interrupted, ioFailure := publicationFailureKinds(publication.Err)
						if interrupted {
							stopped = true
							out.Interruptions = append(out.Interruptions, inspectInterruption(publication.Err, "writing"))
						}
						if ioFailure {
							out.Errors = append(out.Errors, result.Error{Category: result.IO, Code: "publication-failed", Scope: "operation", Path: path, Message: publication.Err.Error()})
						}
					}
					state.leftovers = append([]string(nil), publication.Leftovers...)
				}
			}
		}
		states[path] = state
	}
	// Confirm every otherwise unobserved target with a bounded stable read. In
	// particular NoChange never establishes Saved, and failed writes never borrow
	// their original/candidate snapshots as saved authority.
	for _, doc := range acquired.Documents {
		path := inv.logicalPath(doc.Path)
		state := states[path]
		if !state.observed {
			selected, e := files.SelectBounded(ctx, files.Options{WorkingDirectory: inv.workingDirectory, Paths: []string{doc.Path}}, budget)
			if e == nil && selected != nil {
				for _, failure := range selected.Errors {
					recordFixEffect(out, path, "saved-selection", "rechecking", failure)
				}
			}
			if e == nil && selected != nil && len(selected.Errors) == 0 && len(selected.Targets) == 1 {
				current, readErr := files.Acquire(ctx, selected, files.AcquireOptions{DocumentBytes: budget.Limits().DocumentBytes, TotalContentBytes: reportLimit(inv.options.limits, "total-content-bytes"), Budget: budget, ContentLedger: content})
				e = readErr
				if current != nil {
					for _, failure := range current.Errors {
						recordFixEffect(out, path, "saved-acquisition", "rechecking", failure)
					}
					for _, failure := range current.PreflightErrors {
						out.Coverage = append(out.Coverage, result.Coverage{Path: path, Reason: "saved-view-unavailable", Message: failure.Error()})
					}
					for _, stop := range current.Interruptions {
						out.Interruptions = append(out.Interruptions, inspectInterruption(stop.Err, "rechecking"))
					}
				}
				if e == nil && current != nil && len(current.Documents) == 1 && len(current.Errors)+len(current.PreflightErrors)+len(current.Interruptions) == 0 {
					state.saved = current.Documents[0].Source
					state.observed = true
					snapshot := sourceIdentity(state.saved, "saved")
					state.snapshot = &snapshot
				}
			}
			if !state.observed {
				out.Coverage = append(out.Coverage, result.Coverage{Path: path, Reason: "saved-view-unavailable", Message: "A current saved snapshot could not be established with the remaining operation budget."})
				if e != nil && !isInspectInterruption(e) {
					out.Errors = append(out.Errors, result.Error{Category: result.IO, Code: "saved-confirmation", Scope: "operation", Path: path, Message: e.Error()})
				}
				if e != nil && isInspectInterruption(e) {
					out.Interruptions = append(out.Interruptions, inspectInterruption(e, "rechecking"))
				}
			}
		}
		states[path] = state
	}
	// Construct the complete observed overlay before starting saved analysis.
	for _, doc := range acquired.Documents {
		path := inv.logicalPath(doc.Path)
		state := states[path]

		if state.observed {
			plan := inv.plan.CandidatePlan()
			input, e := plan.NewSourceInput(path, state.saved)
			if e == nil {
				state.analysis, _, e = plan.InspectSaved(ctx, input, engine.RunOptions{Budget: budget})
				state.analysisStarted = len(state.analysis.CheckedFiles) > 0
			}
			if e != nil {
				out.Coverage = append(out.Coverage, result.Coverage{Path: path, Reason: "saved-analysis-unavailable", Message: fmt.Sprint(e)})
				if isInspectInterruption(e) {
					out.Interruptions = append(out.Interruptions, inspectInterruption(e, "rechecking"))
				} else {
					out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "saved-analysis-failed", Scope: "operation", Path: path, Message: e.Error()})
				}
			}
		}
		states[path] = state
	}
	return states
}

// Joined native cleanup/effect errors retain I/O precedence even when another
// branch is cancellation or quota exhaustion. A wrapped join is traversed too.
func publicationFailureKinds(err error) (interrupted, ioFailure bool) {
	if err == nil {
		return false, false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			stop, effect := publicationFailureKinds(cause)
			interrupted = interrupted || stop
			ioFailure = ioFailure || effect
		}
		return interrupted, ioFailure
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return publicationFailureKinds(wrapped.Unwrap())
	}
	if isInspectInterruption(err) {
		return true, false
	}
	return false, true
}

func recordFixEffect(out *result.Result, path, code, phase string, err error) {
	interrupted, effect := publicationFailureKinds(err)
	if interrupted {
		stop := inspectInterruption(err, phase)
		stop.Path = path
		out.Interruptions = append(out.Interruptions, stop)
	}
	if effect {
		out.Errors = append(out.Errors, result.Error{Category: result.IO, Code: code, Scope: "operation", Path: path, Message: err.Error()})
	}
}

// Essential identity survives rejected content admission without allocating a
// second growing byte buffer or retaining the unadmitted Source in saved state.
func observedSavedIdentity(source parser.Source) result.Snapshot {
	span, _ := source.Span(0, source.Len())
	text, _ := source.Text(span)
	hash := sha256.New()
	var chunk [4096]byte
	for len(text) > 0 {
		n := copy(chunk[:], text)
		_, _ = hash.Write(chunk[:n])
		text = text[n:]
	}
	return result.Snapshot{View: "saved", SHA256: fmt.Sprintf("%x", hash.Sum(nil)), SizeBytes: source.Len()}
}
