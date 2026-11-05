package cli

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/integration"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
)

// mkdocsRule returns MD074's resolved settings and whether lint must run it.
func mkdocsRule(rules map[string]engine.ResolvedRule) (engine.ResolvedRule, bool) {
	rule := rules["MD074"]
	return rule, rule.Enabled && rule.Available
}

// checkMkDocs runs MD074 once for the operation. It binds the MkDocs context
// to the lint session's root, operation and service, reads only through them
// and projects the rule's records at the configuration's cwd-relative path.
// The configuration is a finding location, never a lint target.
func (inv *lintInvocation) checkMkDocs(ctx context.Context, budget *parser.OperationBudget, session *fragmentResources, rule engine.ResolvedRule, out *result.Result) {
	configPath, _ := rule.Options["config_path"].(string)
	checkAbsolute, _ := rule.Options["check_absolute"].(bool)
	checkOmitted, _ := rule.Options["check_omitted"].(bool)
	path, readFailure := configPath, false
	outcome, source, err := func() (rules.MkDocsOutcome, parser.Source, error) {
		if err := session.ensure(); err != nil {
			return rules.MkDocsOutcome{}, parser.Source{}, err
		}
		path = inv.resourcePath(session.rootPath, configPath)
		view, err := session.operation.NewView(resource.Original, nil)
		if err != nil {
			return rules.MkDocsOutcome{}, parser.Source{}, err
		}
		scope, err := integration.NewScope(ctx, session.operation, view, integration.Settings{MkDocs: integration.MkDocsPolicy{Enabled: true, ConfigPath: configPath, CheckAbsolute: checkAbsolute, CheckOmitted: checkOmitted}})
		if err != nil {
			return rules.MkDocsOutcome{}, parser.Source{}, err
		}
		if scope, err = scope.WithMkDocs(ctx, session.service); err != nil {
			return rules.MkDocsOutcome{}, parser.Source{}, err
		}
		// Retained facts stay usable next to an acquisition error.
		mkdocs, acquireErr := scope.MkDocs(ctx)
		readFailure = mkdocsReadFailure(mkdocs.ConfigObservation().Problem)
		for i := 0; i < mkdocs.KnownTargetCount(); i++ {
			target, _ := mkdocs.KnownTarget(i)
			readFailure = readFailure || mkdocsReadFailure(target.Problem())
		}
		outcome, ruleErr := rules.MkDocsNavigation(ctx, scope, mkdocs)
		return outcome, mkdocs.ConfigSource(), errors.Join(acquireErr, ruleErr)
	}()
	// One projector for the operation: a stopped budget, deadline or
	// cancellation is an interruption, and the shared reserve bounds the
	// records still reported after it.
	projector := engine.NewSpanProjector(ctx, budget, source)
	project := func(record rules.MkDocsRecord) (*result.Range, result.ByteRange, *result.Snapshot, error) {
		if !record.Located {
			return nil, result.ByteRange{}, nil, nil
		}
		span, bytes, snap, projectErr := projector.Project(record.Span)
		return &span, bytes, &snap, projectErr
	}
	var projectErr error
	for _, finding := range outcome.Findings {
		var span *result.Range
		var bytes result.ByteRange
		var snap *result.Snapshot
		if span, bytes, snap, projectErr = project(finding); projectErr != nil {
			break
		}
		out.Diagnostics = append(out.Diagnostics, result.Diagnostic{Path: path, Rule: "MD074", Severity: "error", Message: finding.Message, Range: *span, ByteRange: bytes, Snapshot: *snap})
	}
	for _, coverage := range outcome.Coverage {
		if projectErr != nil {
			break
		}
		var span *result.Range
		var snap *result.Snapshot
		if span, _, snap, projectErr = project(coverage); projectErr != nil {
			break
		}
		out.Coverage = append(out.Coverage, result.Coverage{Path: path, Rule: "MD074", Reason: result.ContextUnavailable, Range: span, Snapshot: snap, Message: coverage.Message})
	}
	if len(outcome.Findings)+len(outcome.Coverage) > 0 {
		err = errors.Join(err, projectErr, projector.Stop())
	}
	if err == nil {
		return
	}
	// The finalizer links these causes to the coverage by path, rule and reason.
	if interrupted(err) {
		reason := "limit-exceeded"
		if errors.Is(err, context.Canceled) {
			reason = "cancelled"
		}
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "deadline-exceeded"
		}
		record := result.Interruption{Path: path, Reason: reason, Phase: "analysis", Message: err.Error()}
		var limit *parser.LimitExceeded
		if errors.As(err, &limit) {
			record.Limit = &result.LimitObservation{Name: limit.Name, Observed: limit.Observed, Allowed: limit.Allowed, Unit: result.LimitUnit(limit.Name)}
		}
		out.Interruptions = append(out.Interruptions, record)
		out.Coverage = append(out.Coverage, result.Coverage{Path: path, Rule: "MD074", Reason: reason, Message: "MkDocs navigation analysis was interrupted."})
		return
	}
	// An actual read failure is an I/O error, as for MD057 lookups; anything
	// else the producer returns is unexpected.
	category := result.Internal
	if readFailure {
		category = result.IO
	}
	out.Errors = append(out.Errors, result.Error{Category: category, Code: "mkdocs-context", Scope: "operation", Path: path, Rule: "MD074", Message: err.Error()})
	if len(outcome.Coverage) == 0 {
		out.Coverage = append(out.Coverage, result.Coverage{Path: path, Rule: "MD074", Reason: result.ContextUnavailable, Message: "MkDocs navigation analysis did not complete."})
	}
}

func mkdocsReadFailure(problem resource.Problem) bool {
	return problem.Kind == resource.ReadFailure || problem.Kind == resource.DecodeFailure
}

// resourcePath is the cwd-relative report path of a key below the native root.
// The root is canonical, so a symbolic-link working directory is resolved too.
func (inv *lintInvocation) resourcePath(root, key string) string {
	base := inv.workingDirectory
	if canonical, err := filepath.EvalSymlinks(base); err == nil {
		base = canonical
	}
	rel, err := filepath.Rel(base, filepath.Join(root, filepath.FromSlash(key)))
	if err != nil {
		return key
	}
	return filepath.ToSlash(rel)
}
