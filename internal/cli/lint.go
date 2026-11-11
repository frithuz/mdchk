package cli

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/report"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

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
	budget, err := newOperationBudget(limits, inv.options.limits)
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
	var fragments *fragmentResources
	resources := resourceRules(inv.plan.Rules())
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
			if resources {
				if fragments == nil {
					fragments = newFragmentResources(ctx, budget, inv.plan.CandidatePlan().Context(), resource.RootCLI, inv.workingDirectory, inv.selectedConfig(), "")
				}
				fragments.targets = append(fragments.targets, fragmentTarget{id: in.Path(), path: document.Path, source: in.Source()})
			}
		}
	}
	options := engine.RunOptions{Mode: "cli", Budget: budget}
	// Assign only a non-nil pointer: a typed nil would make the interface non-nil
	// and send rules to a provider that does not exist.
	if fragments != nil {
		options.Fragments = fragments
	}
	out, err := inv.plan.Lint(ctx, inputs, options)
	if fragments != nil {
		// Join the service worker before the native root closes; a late close
		// failure cannot alter findings already projected.
		_ = fragments.Close()
	}
	if err != nil {
		return inv.fail(err, "execution-failed", result.Internal)
	}
	inv.mergeAcquisition(&out, selected, acquired)
	engine.FinalizeLintResult(&out)
	inv.result = &out
	return report.LintExit(out)
}
