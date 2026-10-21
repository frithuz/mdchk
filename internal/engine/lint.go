package engine

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

type RunOptions struct {
	Mode       string
	Budget     *parser.OperationBudget
	Validation ValidationProviders
	inspection *InspectionTrace
	candidate  bool
}

// Lint preflights the complete supplied batch before entering validation. Its
// returned error is a usage/configuration admission failure; runtime errors and
// interruptions belong to the v1 result. All parser and callback work shares
// one budget. Results contain no edits, output strings, or process exit codes.
func (r *Registry) Lint(ctx context.Context, inputs []Input, options RunOptions) (result.Result, error) {
	if r == nil || ctx == nil {
		return result.Result{}, fmt.Errorf("nil registry or context")
	}
	mode := options.Mode
	if mode == "" {
		mode = "cli"
	}
	if mode != "cli" && mode != "project" && mode != "standalone" {
		return result.Result{}, fmt.Errorf("invalid mode %q", mode)
	}
	budget := options.Budget
	if budget == nil {
		budget, _ = parser.NewOperationBudget(parser.DefaultLimits())
	}
	if err := budget.Limits().Validate(); err != nil {
		return result.Result{}, fmt.Errorf("invalid operation budget: %w", err)
	}
	ordered := slices.Clone(inputs)
	slices.SortFunc(ordered, func(a, b Input) int { return strings.Compare(a.path, b.path) })
	settings := make([]map[string]ResolvedRule, len(ordered))
	for i, in := range ordered {
		if in.path == "" || i > 0 && ordered[i-1].path == in.path {
			return result.Result{}, fmt.Errorf("invalid or duplicate input path %q", in.path)
		}
		if int64(in.source.Len()) > budget.Limits().DocumentBytes {
			return result.Result{}, fmt.Errorf("input %s exceeds document-bytes limit", in.path)
		}
		var resolved map[string]ResolvedRule
		if in.plan != nil {
			if in.plan.registry != r {
				return result.Result{}, fmt.Errorf("%s: execution plan belongs to another registry", in.path)
			}
			resolved = in.plan.rules
		} else {
			var err error
			resolved, err = r.ResolveSettings(in.settings, options.Validation)
			if err != nil {
				return result.Result{}, fmt.Errorf("%s: %w", in.path, err)
			}
		}
		for _, id := range r.ids {
			state := resolved[id]
			if state.Enabled && state.Explicit && !state.Available {
				return result.Result{}, fmt.Errorf("%s: explicitly enabled rule %s is unavailable", in.path, id)
			}
		}
		settings[i] = resolved
	}
	out := result.NewLint(mode)
	out.EffectiveLimits = effectiveLimits(budget.Limits())
	stopped := ""
	reporting := &reportingReserve{}
	for i, in := range ordered {
		file := result.File{Path: in.path, Analysis: "not-started", WriteStatus: "not-requested", ReturnStatus: "not-requested", ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}}
		if stopped != "" {
			out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Reason: stopped, Message: "Target validation did not start."})
			out.Files = append(out.Files, file)
			continue
		}
		// Observe an externally cancelled operation before entering validation or
		// hashing/decoding an acquired target. Known targets remain not-started.
		if err := ctx.Err(); err != nil {
			stopped = recordInterruption(&out, in.path, "acquisition", err)
			out.Files = append(out.Files, file)
			continue
		}
		out.CheckedFiles = append(out.CheckedFiles, in.path)
		snap := snapshot(in.source)
		if options.candidate {
			snap.View = "candidate"
		}
		file.InputSnapshot = &snap
		file.Analysis = "complete"
		// Reserve indexing, decoding and snapshot hashing/copies. Diagnostic
		// coordinates are charged separately by the bounded projector.
		// Admission bounds each scan. No index with a parser's expired private
		// metering context escapes its parse call.
		if err := budget.ChargeWork(int64(in.source.Len())*4 + 1); err != nil {
			stopped = recordInterruption(&out, in.path, "acquisition", err)
			file.Analysis = "partial"
			out.Files = append(out.Files, file)
			continue
		}
		if options.inspection != nil {
			options.inspection.initialize(r)
		}
		validation := validateEncoding(in.path, in.source, settings[i]["MD094"].Enabled, snap)
		index := validation.Index
		if options.inspection != nil {
			options.inspection.encoding(validation.Error, in.source)
		}
		if validation.Error != nil {
			out.Errors = append(out.Errors, *validation.Error)
			out.Coverage = append(out.Coverage, result.Coverage{Reason: "decoding-failed", Path: in.path, Message: "Markdown validation is unavailable."})
			file.Analysis = "failed"
			out.Files = append(out.Files, file)
			continue
		}
		parsed := parser.ParseWithBudget(ctx, in.source, budget)
		if options.inspection != nil {
			options.inspection.parsed = parsed
		}
		if parsed.Failure != nil {
			out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "parser-failure", Scope: "operation", Path: in.path, Message: parsed.Failure.Error()})
			out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Reason: result.ParserUncertain, Message: "Parser context is unavailable."})
			file.Analysis = "failed"
			out.Files = append(out.Files, file)
			continue
		}
		if parsed.Interruption != "" {
			err := ctx.Err()
			if parsed.Limit != nil {
				err = parsed.Limit
			}
			if err == nil {
				err = errors.New(string(parsed.Interruption))
			}
			stopped = recordInterruption(&out, in.path, "parsing", err)
			file.Analysis = "partial"
			out.Files = append(out.Files, file)
			continue
		}
		syntaxReliable := parsed.Status == parser.Complete && parsed.AST == parser.Available && len(parsed.Uncertainty) == 0 && in.documentContext.Flavor == "commonmark"
		if !syntaxReliable {
			out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Reason: result.ParserUncertain, Message: "The requested syntax is not completely recognized."})
		}
		projection := projector{ctx: ctx, budget: budget, reporting: reporting, source: in.source, index: index}
		policy := DirectivePolicy(ruleDirectivePolicy{rules: settings[i]})
		if in.plan != nil && in.plan.directivePolicy != nil {
			policy = in.plan.directivePolicy
		}
		directives := ProcessDirectives(ctx, parsed, policy, budget)
		analysis := DirectiveAnalysis{path: in.path, state: directives}
		if options.inspection != nil {
			options.inspection.directives = directives
		}
		for _, diagnostic := range directives.Diagnostics() {
			diagnostic.Path, diagnostic.Snapshot = in.path, snap
			out.Diagnostics = append(out.Diagnostics, diagnostic)
		}
		directiveNotes := directives.Notes()
		for noteIndex, note := range directiveNotes {
			if err := appendDirectiveNote(&out, in.path, snap, &projection, note); err != nil {
				count := len(directiveNotes) - noteIndex
				out.Omissions = append(out.Omissions, result.Omission{Section: "/notes", Reason: "output-limit", Path: in.path, Count: &count, Message: err.Error()})
				break
			}
		}
		if !directives.Complete() || projection.stop != nil {
			out.RetainAnalysis(analysis)
			if projection.stop != nil {
				stopped = recordInterruption(&out, in.path, "analysis", projection.stop)
			} else if isInterruption(directives.Err()) {
				stopped = recordInterruption(&out, in.path, "analysis", directives.Err())
			} else {
				out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Reason: result.ContextUnavailable, Message: "Directive state is unresolved; ordinary rule analysis is withheld."})
			}
			file.Analysis = "partial"
			file.ResultSnapshot = &snap
			out.Files = append(out.Files, file)
			continue
		}
		if options.inspection != nil && options.inspection.prepare != nil {
			if err := options.inspection.prepare(options.inspection); err != nil {
				if isInterruption(err) {
					stopped = recordInterruption(&out, in.path, "analysis", err)
				} else {
					out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "inspection-projection", Scope: "operation", Path: in.path, Message: err.Error()})
				}
				file.Analysis = "partial"
				file.ResultSnapshot = &snap
				out.Files = append(out.Files, file)
				out.RetainAnalysis(analysis)
				continue
			}
		}
		rc := RuleContext{input: in, index: index, document: parsed.Document, budget: budget}

		for _, id := range r.ids {
			state := settings[i][id]
			m := r.metadata[id]
			active := directives.EnabledSomewhere(id)
			if !active || id == "MD094" {
				if options.inspection != nil && id != "MD094" {
					options.inspection.skip(id, "inactive")
				}
				continue
			}
			if m.AppliesTo != nil && !m.AppliesTo(in.documentContext) {
				if options.inspection != nil {
					options.inspection.skip(id, "inapplicable")
				}
				continue
			}
			if err := rc.Checkpoint(ctx, 1); err != nil {
				stopped = recordInterruption(&out, in.path, "analysis", err)
				break
			}
			check := r.checks[id]
			if check == nil {
				if options.inspection != nil {
					options.inspection.skip(id, "rule-unavailable")
				}
				out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Rule: id, Reason: result.RuleUnavailable, Message: "No executable implementation is registered."})
				continue
			}
			if m.RequiresSyntax && !syntaxReliable {
				if options.inspection != nil {
					options.inspection.skip(id, "parser-uncertain")
				}
				out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Rule: id, Reason: result.ParserUncertain, Message: "Rule requires reliable complete parser context."})
				continue
			}
			rc.options = state.Options
			// A source-only callback cannot accidentally trust a partial syntax tree.
			rc.document = parsed.Document
			if !syntaxReliable {
				rc.document = nil
			}
			outcome := check(ctx, rc)
			if options.inspection != nil {
				recorded := outcome
				if recorded.Err == nil {
					recorded.Err = rc.Checkpoint(ctx, 0)
				}
				options.inspection.ran(id, recorded, false, in.source.Len())
			}
			if isInterruption(outcome.Err) {
				projection.stop = outcome.Err
			}
			for findingIndex, finding := range outcome.Findings {
				diagnostic, err := makeDiagnostic(in, &projection, snap, m, finding)
				if omittedProjection(err) {
					count := len(outcome.Findings) - findingIndex
					out.Omissions = append(out.Omissions, result.Omission{Section: "/diagnostics", Reason: "output-limit", Path: in.path, Count: &count, Message: err.Error()})
					break
				}
				if err != nil {
					if options.inspection != nil {
						options.inspection.invalid(id, false)
					}
					out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "invalid-finding", Scope: "operation", Path: in.path, Rule: id, Message: err.Error()})
					out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Rule: id, Reason: result.ContextUnavailable, Message: "Rule returned an invalid finding."})
					continue
				}
				effective, stateErr := directives.StateAt(id, finding.Span.Start())
				if stateErr != nil {
					out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "directive-state", Scope: "operation", Path: in.path, Rule: id, Message: stateErr.Error()})
					continue
				}
				if !effective.Enabled {
					analysis.suppressed = append(analysis.suppressed, DirectiveSuppression{Diagnostic: diagnostic, Owner: effective.Owner, Token: effective.Token})
					appendSuppressed(&out, diagnostic, effective.Owner)
				} else {
					out.Diagnostics = append(out.Diagnostics, diagnostic)
				}
			}
			if projection.stop != nil {
				stopped = recordInterruption(&out, in.path, "analysis", projection.stop)
			}
			if outcome.IncompleteReason != "" {
				out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Rule: id, Reason: result.ContextUnavailable, Message: outcome.IncompleteReason})
			}
			if outcome.Err != nil {
				if isInterruption(outcome.Err) {
					if stopped == "" {
						stopped = recordInterruption(&out, in.path, "analysis", outcome.Err)
					}
					break
				}
				out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "rule-failure", Scope: "operation", Path: in.path, Rule: id, Message: outcome.Err.Error()})
				out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Rule: id, Reason: result.ContextUnavailable, Message: "Rule did not complete."})
			}
			if stopped != "" {
				break
			}
			if err := rc.Checkpoint(ctx, 0); err != nil {
				stopped = recordInterruption(&out, in.path, "analysis", err)
				break
			}
		}
		if options.inspection != nil && stopped == "" {
			options.inspection.observe(ctx, r, rc, parsed, directives, &projection, snap, syntaxReliable)
		}
		out.RetainAnalysis(analysis)
		file.ResultSnapshot = &snap
		out.Files = append(out.Files, file)
	}
	FinalizeLintResult(&out)
	return out, nil
}
func snapshot(source parser.Source) result.Snapshot {
	return result.Snapshot{View: "original", SHA256: fmt.Sprintf("%x", sha256.Sum256(source.Bytes())), SizeBytes: source.Len()}
}
func makeDiagnostic(in Input, projection *projector, snap result.Snapshot, m Metadata, f Finding) (result.Diagnostic, error) {
	if err := projection.begin(f); err != nil {
		return result.Diagnostic{}, err
	}
	if strings.TrimSpace(f.Message) == "" {
		return result.Diagnostic{}, fmt.Errorf("empty finding message")
	}
	a, err := projection.position(f.Span.Start())
	if err != nil {
		return result.Diagnostic{}, err
	}
	b, err := projection.position(f.Span.End())
	if err != nil {
		return result.Diagnostic{}, err
	}
	return result.Diagnostic{Path: in.path, Rule: m.ID, Severity: "error", Message: f.Message, Range: result.Range{Start: a, End: b}, ByteRange: result.ByteRange{Start: f.Span.Start(), End: f.Span.End()}, Snapshot: snap, FixAvailable: m.Fix == ImplementedFix && f.FixEligible}, nil
}
func recordInterruption(out *result.Result, path, phase string, err error) string {
	reason := "limit-exceeded"
	if errors.Is(err, context.Canceled) {
		reason = "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "deadline-exceeded"
	}
	interruption := result.Interruption{Path: path, Reason: reason, Phase: phase, Message: err.Error()}
	var limit *parser.LimitExceeded
	if errors.As(err, &limit) {
		interruption.Limit = &result.LimitObservation{Name: limit.Name, Observed: limit.Observed, Allowed: limit.Allowed, Unit: result.LimitUnit(limit.Name)}
	}
	out.Interruptions = append(out.Interruptions, interruption)
	out.Coverage = append(out.Coverage, result.Coverage{Path: path, Reason: reason, Message: "Remaining validation is unassessed."})
	return reason
}
func effectiveLimits(l parser.Limits) []result.EffectiveLimit {
	d := parser.DefaultLimits()
	items := []struct {
		name       string
		value, def int64
	}{{"document-bytes", l.DocumentBytes, d.DocumentBytes}, {"syntax-depth", l.SyntaxDepth, d.SyntaxDepth}, {"document-nodes", l.DocumentNodes, d.DocumentNodes}, {"total-nodes", l.TotalNodes, d.TotalNodes}, {"work-units", l.WorkUnits, d.WorkUnits}, {"operation-time", int64(l.OperationTime), int64(d.OperationTime)}}
	out := make([]result.EffectiveLimit, 0, len(items))
	for _, v := range items {
		source := "default"
		if v.value != v.def {
			source = "operator"
		}
		out = append(out, result.EffectiveLimit{Name: v.name, Value: v.value, Unit: result.LimitUnit(v.name), Source: source})
	}
	out = append(out, result.EffectiveLimit{Name: "reporting-time", Value: 2, Unit: "seconds", Source: "default"})
	slices.SortFunc(out, func(a, b result.EffectiveLimit) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// FinalizeLintResult assigns canonical IDs, cause links and per-file references
// to the retained lint observations, then derives the shared lint/fix outcome.
// Adapters may append acquisition records and remap logical paths before calling
// it again. Explicit cause IDs must identify unique errors or interruptions
// across the combined records; unreferenced new records may have empty IDs.
// It mutates the supplied result and preserves observations and snapshots.
func FinalizeLintResult(out *result.Result) {
	finalizeDirectiveRecords(out)
	slices.Sort(out.CheckedFiles)
	out.CheckedFiles = slices.Compact(out.CheckedFiles)
	slices.SortFunc(out.Files, func(a, b result.File) int { return cmp.Compare(a.Path, b.Path) })
	slices.SortFunc(out.SkippedFiles, func(a, b result.SkippedFile) int {
		if c := cmp.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Reason, b.Reason); c != 0 {
			return c
		}
		return cmp.Compare(a.Message, b.Message)
	})
	slices.SortFunc(out.Diagnostics, func(a, b result.Diagnostic) int {
		if c := cmp.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		if c := cmp.Compare(a.ByteRange.Start, b.ByteRange.Start); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Rule, b.Rule); c != 0 {
			return c
		}
		if c := cmp.Compare(a.ByteRange.End, b.ByteRange.End); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Message, b.Message); c != 0 {
			return c
		}
		return cmp.Compare(strconv.FormatBool(a.FixAvailable), strconv.FormatBool(b.FixAvailable))
	})
	slices.SortFunc(out.Errors, func(a, b result.Error) int {
		if c := cmp.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		if c := compareRange(a.Range, b.Range); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Rule, b.Rule); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Code, b.Code); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Category, b.Category); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Message, b.Message); c != 0 {
			return c
		}
		a.ID, b.ID = "", ""
		return cmp.Compare(errorKey(a), errorKey(b))
	})
	for i := range out.Diagnostics {
		out.Diagnostics[i].ID = fmt.Sprintf("diagnostic-%d", i+1)
	}
	// Preserve explicit cause links while assigning canonical generated IDs.
	causeIDs := make(map[string]string)
	for i := range out.Errors {
		id := fmt.Sprintf("error-%d", i+1)
		if old := out.Errors[i].ID; old != "" {
			causeIDs[old] = id
		}
		out.Errors[i].ID = id
	}
	slices.SortFunc(out.Interruptions, func(a, b result.Interruption) int {
		// All semantic fields participate, so equal path/reason interruptions
		// cannot inherit callback order. ID is generated, not semantic.
		a.ID, b.ID = "", ""
		return cmp.Compare(interruptionKey(a), interruptionKey(b))
	})
	for i := range out.Interruptions {
		id := fmt.Sprintf("interruption-%d", i+1)
		if old := out.Interruptions[i].ID; old != "" {
			causeIDs[old] = id
		}
		out.Interruptions[i].ID = id
	}
	// Index once after canonical sorting/ID assignment. Linking is linear in
	// records plus emitted references, rather than scanning every error and
	// coverage record again for every target.
	type key struct{ path, kind string }
	errorsByPath := make(map[string][]string)
	errorsByRule := make(map[key][]int)
	decodeErrors := make(map[string][]int)
	stopsByReason := make(map[key][]string)
	coverageByPath := make(map[string][]string)
	out.ResetReportingErrors()
	for i, e := range out.Errors {
		out.CountReportingError(e.Category)
		errorsByPath[e.Path] = append(errorsByPath[e.Path], e.ID)
		if e.Rule != "" {
			k := key{e.Path, e.Rule}
			errorsByRule[k] = append(errorsByRule[k], i)
		}
		if e.Code == "invalid-utf8" {
			decodeErrors[e.Path] = append(decodeErrors[e.Path], i)
		}
	}
	for _, stop := range out.Interruptions {
		k := key{stop.Path, stop.Reason}
		stopsByReason[k] = append(stopsByReason[k], stop.ID)
	}
	for i := range out.Coverage {
		c := &out.Coverage[i]
		for j, id := range c.CauseIDs {
			if canonical, ok := causeIDs[id]; ok {
				c.CauseIDs[j] = canonical
			}
		}
		causes := errorsByRule[key{c.Path, c.Rule}]
		if c.Reason == "decoding-failed" {
			causes = mergeIndices(causes, decodeErrors[c.Path])
		}
		for _, cause := range causes {
			c.CauseIDs = append(c.CauseIDs, out.Errors[cause].ID)
		}
		c.CauseIDs = append(c.CauseIDs, stopsByReason[key{c.Path, c.Reason}]...)
	}
	// Cause links are part of semantic identity. Link first, then canonicalize
	// and deduplicate before assigning IDs or publishing file references.
	out.CanonicalizeCoverage()
	for i := range out.Coverage {
		c := &out.Coverage[i]
		id := fmt.Sprintf("coverage-%d", i+1)
		c.ID = id
		coverageByPath[c.Path] = append(coverageByPath[c.Path], id)
	}
	for i := range out.Files {
		f := &out.Files[i]
		// File references are derived from the retained canonical records.
		f.ErrorIDs = append([]string{}, errorsByPath[f.Path]...)
		f.CoverageIDs = append([]string{}, coverageByPath[f.Path]...)
		if len(f.CoverageIDs) > 0 && f.Analysis == "complete" {
			f.Analysis = "partial"
		}
		if len(f.ErrorIDs) > 0 && out.Operation != "config validate" {
			f.Analysis = "failed"
		}
	}
	out.FinalizeLintFixOutcome()
	if out.Operation == "config validate" {
		out.Complete = out.ConfigValidation != nil && out.ConfigValidation.ScanComplete && len(out.Interruptions) == 0 && len(out.Omissions) == 0
		out.Status = "clean"
		if !out.Complete {
			out.Status = "incomplete"
		}
		if len(out.Errors) > 0 {
			out.Status = "error"
		}
	}
}
func compareRange(a, b *result.Range) int {
	if a == nil {
		if b == nil {
			return 0
		}
		return -1
	}
	if b == nil {
		return 1
	}
	if c := cmp.Compare(a.Start.Line, b.Start.Line); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Start.Column, b.Start.Column); c != 0 {
		return c
	}
	if c := cmp.Compare(a.End.Line, b.End.Line); c != 0 {
		return c
	}
	return cmp.Compare(a.End.Column, b.End.Column)
}

// mergeIndices retains canonical record order and avoids duplicate references
// when one error matches both a rule and a decoding cause.
func mergeIndices(a, b []int) []int {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make([]int, 0, len(a)+len(b))
	for len(a) > 0 && len(b) > 0 {
		switch {
		case a[0] < b[0]:
			out = append(out, a[0])
			a = a[1:]
		case b[0] < a[0]:
			out = append(out, b[0])
			b = b[1:]
		default:
			out = append(out, a[0])
			a = a[1:]
			b = b[1:]
		}
	}
	out = append(out, a...)
	out = append(out, b...)
	return out
}

// These concrete records contain only JSON-marshalable scalar fields. The
// encoded tie-break includes optional semantic fields without generated IDs.
func errorKey(e result.Error) string {
	b, _ := json.Marshal(e)
	return string(b)
}
func interruptionKey(i result.Interruption) string {
	b, _ := json.Marshal(i)
	return string(b)
}
