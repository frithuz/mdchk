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
	saved      bool
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
		if options.saved {
			snap.View = "saved"
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
		if in.plan != nil && in.plan.slides != nil {
			annotation, err := in.plan.slides.Slides(ctx, budget, in.path)
			if err == nil {
				err = annotation.validate()
			}
			if err != nil {
				if isInterruption(err) {
					stopped = recordInterruption(&out, in.path, "context", err)
				} else {
					out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "document-context", Scope: "operation", Path: in.path, Message: err.Error()})
					out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Reason: result.ContextUnavailable, Message: "Document annotation is unavailable."})
				}
				file.Analysis = "partial"
				out.Files = append(out.Files, file)
				continue
			}
			in.documentContext.Slides = annotation
		}
		if options.inspection != nil {
			options.inspection.documentContext = in.documentContext
			options.inspection.contextKnown = true
		}
		// Resolve only explicit context/option exceptions before syntax prerequisites.
		// No parser objects are supplied to this preassessment.
		preassessed := map[string]bool(nil)
		preassessCount := 0
		for id := range r.applies {
			if settings[i][id].Enabled || options.inspection != nil && options.inspection.observeInactive {
				preassessCount++
			}
		}
		if preassessCount > 0 {
			if err := budget.ChargeNodes(int64(preassessCount)); err != nil {
				stopped = recordInterruption(&out, in.path, "context", err)
				file.Analysis = "partial"
				out.Files = append(out.Files, file)
				continue
			}
			preassessed = make(map[string]bool, preassessCount)
			for _, id := range r.ids {
				if r.applies[id] == nil || !settings[i][id].Enabled && (options.inspection == nil || !options.inspection.observeInactive) {
					continue
				}
				rc := RuleContext{input: in, budget: budget, operation: ctx, options: settings[i][id].Options}
				applies, err := r.applicable(ctx, rc, r.metadata[id])
				if err != nil {
					if isInterruption(err) {
						stopped = recordInterruption(&out, in.path, "context", err)
					} else {
						out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "rule-applicability", Scope: "operation", Path: in.path, Rule: id, Message: err.Error()})
					}
					file.Analysis = "partial"
					break
				}
				preassessed[id] = applies
				if !applies && options.inspection != nil {
					options.inspection.skip(id, "inapplicable")
				}
			}
			if len(preassessed) != preassessCount {
				out.Files = append(out.Files, file)
				continue
			}
		}
		if options.inspection != nil {
			options.inspection.preassessed = preassessed
		}
		parsed := parser.ParseWithOptions(ctx, in.source, budget, in.documentContext.ParseOptions())
		if options.inspection != nil {
			options.inspection.parsed = parsed
			options.inspection.lines = index
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
		syntaxReliable := parsed.Status == parser.Complete && parsed.AST == parser.Available && len(parsed.Uncertainty) == 0 && (in.documentContext.Flavor == "commonmark" || in.documentContext.Flavor == "gfm")
		if !syntaxReliable {
			out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Reason: result.ParserUncertain, Message: "The requested syntax is not completely recognized."})
		}
		projection := projector{ctx: ctx, budget: budget, reporting: reporting, source: in.source, index: index}
		for _, uncertainty := range parsed.Uncertainty {
			if uncertainty.Reason == parser.UnterminatedFrontmatter {
				if err := appendDirectiveNote(&out, in.path, snap, &projection, DirectiveNote{Code: string(parser.UnterminatedFrontmatter), Message: "Frontmatter has no closing delimiter; its original region remains protected and analysis is incomplete.", Span: uncertainty.Region}); err != nil {
					count := 1
					out.Omissions = append(out.Omissions, result.Omission{Section: "/notes", Reason: "output-limit", Path: in.path, Count: &count, Message: err.Error()})
				}
			}
		}
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
		rc := RuleContext{input: in, index: index, document: parsed.Document, budget: budget, operation: ctx, parsed: parsed, directives: directives, syntaxReliable: syntaxReliable}
		audit, auditErr := planSuppressionAudit(ctx, r, rc, &projection, snap, &out, options.inspection)
		if auditErr != nil {
			if isInterruption(auditErr) {
				stopped = recordInterruption(&out, in.path, "analysis", auditErr)
			} else {
				out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "suppression-audit", Scope: "operation", Path: in.path, Rule: "MD087", Message: auditErr.Error()})
			}
			out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Rule: "MD087", Reason: result.ContextUnavailable, Message: "Suppression audit preparation did not complete."})
			out.RetainAnalysis(analysis)
			file.Analysis = "partial"
			file.ResultSnapshot = &snap
			out.Files = append(out.Files, file)
			continue
		}
		analysis.audits = audit.decisions
		if len(audit.decisions) != 0 {
			analysis.auditOperation = ctx
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
		auditStartFailed := false
		if err := audit.start(ctx); err != nil {
			auditStartFailed = true
			if isInterruption(err) {
				stopped = recordInterruption(&out, in.path, "analysis", err)
			} else {
				out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "suppression-audit", Scope: "operation", Path: in.path, Rule: "MD087", Message: err.Error()})
			}
		}

		for _, id := range r.ids {
			if stopped != "" || auditStartFailed {
				break
			}
			if id == "MD087" && audit.registered {
				if options.inspection != nil && len(audit.decisions) == 0 && !directives.rules[id].Enabled {
					options.inspection.skip(id, "inactive")
				}
				continue
			}
			if len(audit.decisions) != 0 || options.inspection != nil && options.inspection.observeInactive {
				// Catalog/interval scans and trace entry lookups are charged
				// before scheduling, even when no callback can run.
				if err := rc.auditCharge(ctx, int64(len(directives.intervals[id]))+4*int64(len(r.ids))+8, 0); err != nil {
					stopped = recordInterruption(&out, in.path, "analysis", err)
					break
				}
			}
			state := settings[i][id]
			m := r.metadata[id]
			active := directives.EnabledSomewhere(id)
			observational := !active
			demanded := audit.demanded(id)
			observe := options.inspection != nil && options.inspection.observeInactive && id != "MD087"
			if observational && (demanded || observe) && options.inspection != nil && id != "MD094" {
				options.inspection.entry(id).Observational = "skipped"
			}
			if id == "MD094" || !active && !demanded && !observe {
				if options.inspection != nil && id != "MD094" {
					options.inspection.skip(id, "inactive")
				}
				continue
			}
			if demanded || observational {
				if err := reserveAuditFailures(ctx, rc, &out); err != nil {
					if isInterruption(err) {
						stopped = recordInterruption(&out, in.path, "analysis", err)
					} else {
						out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "suppression-audit", Scope: "operation", Path: in.path, Rule: id, Message: err.Error()})
						out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Rule: id, Reason: result.ContextUnavailable, Message: "Audit retention could not be represented safely."})
					}
					break
				}
			}
			rc.options = state.Options
			rc.observational = observational
			rc.document = parsed.Document
			if !syntaxReliable {
				rc.document = nil
			}
			applies, assessed := preassessed[id]
			var applyErr error
			if !assessed {
				applies, applyErr = r.applicable(ctx, rc, m)
			}
			if applyErr != nil {
				if isInterruption(applyErr) {
					stopped = recordInterruption(&out, in.path, "analysis", applyErr)
					break
				}
				out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "rule-applicability", Scope: "operation", Path: in.path, Rule: id, Message: applyErr.Error()})
				out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Rule: id, Reason: result.ContextUnavailable, Message: "Rule applicability is unresolved."})
				continue
			}
			if !applies {
				if err := audit.completeTarget(ctx, id); err != nil {
					stopped = recordInterruption(&out, in.path, "analysis", err)
					break
				}
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
				if active {
					out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Rule: id, Reason: result.RuleUnavailable, Message: "No executable implementation is registered."})
				}
				continue
			}
			if m.RequiresSyntax && !syntaxReliable {
				if options.inspection != nil {
					options.inspection.skip(id, "parser-uncertain")
				}
				if active {
					out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Rule: id, Reason: result.ParserUncertain, Message: "Rule requires reliable complete parser context."})
				}
				continue
			}
			rc.options = state.Options
			rc.frontmatterAware = m.UsesFrontmatter
			// A source-only callback cannot accidentally trust a partial syntax tree.
			rc.document = parsed.Document
			if !syntaxReliable {
				rc.document = nil
			}
			if demanded || observational {
				if err := rc.auditCharge(ctx, 32, 16); err != nil {
					stopped = recordInterruption(&out, in.path, "analysis", err)
					break
				}
			}
			outcome := check(ctx, rc)
			targetComplete := outcome.Err == nil && outcome.IncompleteReason == ""
			// Independent presentation and MD087 evidence have distinct authority.
			// The callback is shared, but a reporting-funded record is never proof.
			present := active || observe
			strictProjection := projection
			strictProjection.strict = true
			findingProjection := &projection
			if !present {
				findingProjection = &strictProjection
			}
			proofLive := demanded && !isInterruption(outcome.Err)
			if demanded || observational {
				if err := rc.auditCharge(ctx, 0, 0); err != nil {
					outcome.Err = errors.Join(outcome.Err, err)
					proofLive, targetComplete = false, false
				}
			}
			findings := outcome.Findings
			if !present && !proofLive {
				findings = nil
			}
			if isInterruption(outcome.Err) {
				projection.stop = outcome.Err
				findingProjection.stop = outcome.Err
			}
			if len(findings) != 0 && (demanded || observational) {
				if err := findingProjection.charge(513, 257); err != nil {
					if !omittedProjection(err) {
						outcome.Err = errors.Join(outcome.Err, err)
					}
					targetComplete = false
					if present && omittedProjection(err) {
						count := len(findings)
						out.Omissions = append(out.Omissions, result.Omission{Section: "/diagnostics", Reason: "output-limit", Path: in.path, Count: &count, Message: err.Error()})
					}
					findings = nil
				}
			}

			if options.inspection != nil {
				recorded := outcome
				if recorded.Err == nil {
					recorded.Err = rc.Checkpoint(ctx, 0)
				}
				options.inspection.ran(id, recorded, observational, in.source.Len(), m.UsesFrontmatter)
			}
			if isInterruption(outcome.Err) {
				projection.stop = outcome.Err
			}
			stopFinding := func(err error, index int) {
				targetComplete = false
				if omittedProjection(err) {
					count := len(findings) - index
					out.Omissions = append(out.Omissions, result.Omission{Section: "/diagnostics", Reason: "output-limit", Path: in.path, Count: &count, Message: err.Error()})
					return
				}
				projection.stop = err
			}

			for findingIndex, finding := range findings {
				if demanded || observational {
					if err := findingProjection.charge(directiveLookupWork(len(directives.intervals[id]))+int64(len(r.ids))+32, 16); err != nil {
						stopFinding(err, findingIndex)
						break
					}
				}
				diagnostic, err := makeDiagnostic(in, findingProjection, snap, m, finding)
				if omittedProjection(err) {
					stopFinding(err, findingIndex)
					break
				}
				if err != nil {
					targetComplete = false
					if isInterruption(err) {
						projection.stop = err
						break
					}
					if options.inspection != nil {
						options.inspection.invalid(id, observational)
					}
					invalidRecord := result.Error{Category: result.Internal, Code: "invalid-finding", Scope: "operation", Path: in.path, Rule: id, Message: err.Error()}
					invalidCoverage := result.Coverage{Path: in.path, Rule: id, Reason: result.ContextUnavailable, Message: "Rule returned an invalid finding."}
					if demanded || observational {
						if retainedErr := publishInvalidFinding(&out, invalidRecord, invalidCoverage, findingProjection.charge); retainedErr != nil {
							stopFinding(retainedErr, findingIndex)
							break
						}
					} else {
						out.Errors = append(out.Errors, invalidRecord)
						out.Coverage = append(out.Coverage, invalidCoverage)
					}
					continue
				}
				if !frontmatterFindingAllowed(in.source, parsed, m, finding) {
					continue
				}
				effective, stateErr := directives.anchorState(id, finding.Span.Start())
				if stateErr != nil {
					out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "directive-state", Scope: "operation", Path: in.path, Rule: id, Message: stateErr.Error()})
					continue
				}
				if demanded || observational {
					serializationWork, err := auditSerializationWork(diagnostic.Message, diagnostic.Path, snap.SHA256)
					if err != nil {
						stopFinding(err, findingIndex)
						break
					}
					if err := findingProjection.charge(serializationWork, 1); err != nil {
						stopFinding(err, findingIndex)
						break
					}
				}
				if proofLive && findingProjection.stop == nil {
					if err := audit.positive(ctx, id, finding.Span); err != nil {
						projection.stop = err
						findingProjection.stop = err
						proofLive, targetComplete = false, false
						if !isInterruption(err) || !present {
							break
						}
					}
				}
				if observational {
					if options.inspection != nil {
						retained, err := reservePresentedSlice(findingProjection, options.inspection.suppressed)
						if err != nil {
							stopFinding(err, findingIndex)
							break
						}
						options.inspection.suppressed = retained
						options.inspection.suppressed = append(options.inspection.suppressed, DirectiveSuppression{Diagnostic: diagnostic, Owner: effective.Owner, Token: effective.Token})
					}
				} else if !effective.Enabled {
					if demanded {
						retained, err := reservePresentedSlice(findingProjection, analysis.suppressed)
						if err != nil {
							stopFinding(err, findingIndex)
							break
						}
						analysis.suppressed = retained
						raw, err := reservePresentedSlice(findingProjection, out.SuppressedDiagnostics)
						if err != nil {
							stopFinding(err, findingIndex)
							break
						}
						out.SuppressedDiagnostics = raw
					}
					analysis.suppressed = append(analysis.suppressed, DirectiveSuppression{Diagnostic: diagnostic, Owner: effective.Owner, Token: effective.Token})
					appendSuppressed(&out, diagnostic, effective.Owner)
				} else {
					if demanded {
						retained, err := reservePresentedSlice(findingProjection, out.Diagnostics)
						if err != nil {
							stopFinding(err, findingIndex)
							break
						}
						out.Diagnostics = retained
					}
					out.Diagnostics = append(out.Diagnostics, diagnostic)
				}
			}
			if targetComplete && projection.stop == nil && findingProjection.stop == nil {
				if err := audit.completeTarget(ctx, id); err != nil {
					projection.stop = err
				}
			}
			if projection.stop != nil {
				if !isInterruption(projection.stop) {
					if options.inspection != nil && (demanded || observational) {
						e := options.inspection.entry(id)
						if observational {
							e.Observational = "failed"
							e.ObservationalCoverage = []parser.Span{}
						} else {
							e.Active = "failed"
							e.ActiveCoverage = []parser.Span{}
						}
						if len(e.Reasons) == 0 || e.Reasons[len(e.Reasons)-1] != "invalid-finding" {
							e.Reasons = []string{"rule-failure"}
						}
					}
					out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "suppression-audit", Scope: "operation", Path: in.path, Rule: id, Message: projection.stop.Error()})
					out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Rule: id, Reason: result.ContextUnavailable, Message: "Audit retention could not be represented safely."})
					// A retention failure does not erase a provider's already
					// published noninterrupt failure or incomplete context.
					if outcome.IncompleteReason != "" {
						out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Rule: id, Reason: result.ContextUnavailable, Message: outcome.IncompleteReason})
					}
					if outcome.Err != nil {
						out.Errors = append(out.Errors, result.Error{Category: result.Internal, Code: "rule-failure", Scope: "operation", Path: in.path, Rule: id, Message: outcome.Err.Error()})
						out.Coverage = append(out.Coverage, result.Coverage{Path: in.path, Rule: id, Reason: result.ContextUnavailable, Message: "Rule did not complete."})
					}
					break
				}
				if options.inspection != nil && (demanded || observational) {
					e := options.inspection.entry(id)
					if observational {
						if e.Observational != "failed" {
							e.Observational = "partial"
							e.Reasons = []string{"interrupted"}
						}
						e.ObservationalCoverage = []parser.Span{}
					} else {
						if e.Active != "failed" {
							e.Active = "partial"
							e.Reasons = []string{"interrupted"}
						}
						e.ActiveCoverage = []parser.Span{}
					}
				}
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
		audit.finish()
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
	// Deferred correction causes may refer to shared runtime validation failures.
	// Preserve those links through the same canonical error/interruption IDs.
	for i := range out.DeferredFixes {
		for j, id := range out.DeferredFixes[i].CauseIDs {
			if canonical, ok := causeIDs[id]; ok {
				out.DeferredFixes[i].CauseIDs[j] = canonical
			}
		}
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

// Invalid findings still reach ordinary validation. A valid ordinary finding
// may not lie in or cross the opaque header; its end boundary belongs to the
// body, so a physical EOF observation can remain report-only for header-only
// files. Explicit source-metadata rules retain their own header observations.
func frontmatterFindingAllowed(source parser.Source, parsed parser.ParseResult, metadata Metadata, finding Finding) bool {
	if metadata.UsesFrontmatter || parsed.Document == nil {
		return true
	}
	if _, err := source.Text(finding.Span); err != nil {
		return true
	}
	header, present := parsed.Document.Frontmatter()
	if !present {
		return true
	}
	return finding.Span.Start() >= header.BodyOffset()
}
