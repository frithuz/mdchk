package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/parser"
)

type targetEncoding struct {
	path       string
	validation engine.EncodingValidation
}

func (inv *lintInvocation) limit(name string) int64 {
	if value, ok := inv.options.limits[name]; ok {
		return value
	}
	return limitDefaults[name]
}

func acquireLint(inv *lintInvocation, _ io.Writer, stderr io.Writer) int {
	return acquireLintContext(context.Background(), inv, stderr)
}

// acquireLintContext retains the separate selection, acquisition and mandatory
// gate outcomes. Entering the gate counts as checked, but never as completed
// Markdown analysis. Reporters and analysis consume these states in later work.
func acquireLintContext(ctx context.Context, inv *lintInvocation, stderr io.Writer) int {
	limits := parser.Limits{
		DocumentBytes: inv.limit("document-bytes"), SyntaxDepth: inv.limit("syntax-depth"),
		DocumentNodes: inv.limit("document-nodes"), TotalNodes: inv.limit("total-nodes"),
		WorkUnits: inv.limit("work-units"), OperationTime: time.Duration(inv.limit("operation-time")),
	}
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		return output(stderr, fmt.Sprintf("mdchk: unsupported execution limits: %v\n", err), exitUsage)
	}
	ctx, cancel := context.WithTimeout(ctx, limits.OperationTime)
	defer cancel()
	selected, err := files.Select(files.Options{WorkingDirectory: inv.workingDirectory, ExclusionBase: inv.exclusionBase, Paths: inv.options.paths, Exclusions: inv.exclusions, Quarto: inv.selection.Resolution.Settings.QuartoEnabled})
	if err != nil {
		return output(stderr, "mdchk: "+err.Error()+"\n", exitIO)
	}
	inv.targetSelection = selected
	acquired, err := files.Acquire(ctx, selected, files.AcquireOptions{DocumentBytes: limits.DocumentBytes, TotalContentBytes: inv.limit("total-content-bytes"), Budget: budget})
	if err != nil {
		return output(stderr, "mdchk: "+err.Error()+"\n", exitUsage)
	}
	inv.acquisition = acquired
	status := exitIncomplete
	writeError := false
	report := func(message string) {
		if _, err := io.WriteString(stderr, "mdchk: "+message+"\n"); err != nil {
			writeError = true
		}
	}
	for _, failure := range selected.Errors {
		status = exitIO
		report(failure.Error())
	}
	for _, failure := range acquired.Errors {
		status = exitIO
		report(failure.Error())
	}
	for _, failure := range acquired.PreflightErrors {
		if status != exitIO {
			status = exitUsage
		}
		report(failure.Error())
	}
	for _, interruption := range acquired.Interruptions {
		report(interruption.Path + ": " + interruption.Err.Error())
	}
	for _, document := range acquired.Documents {
		if err := ctx.Err(); err != nil {
			acquired.Interruptions = append(acquired.Interruptions, files.Interruption{Path: document.Path, Err: err})
			report(document.Path + ": " + err.Error())
			break
		}
		if err := budget.ChargeWork(int64(document.Source.Len())*4 + 1); err != nil {
			acquired.Interruptions = append(acquired.Interruptions, files.Interruption{Path: document.Path, Err: err})
			report(document.Path + ": " + err.Error())
			break
		}
		inv.checkedPaths = append(inv.checkedPaths, document.Path)
		validation := engine.ValidateEncoding(document.Path, document.Source, inv.selection.Resolution.Rules["MD094"].Enabled)
		inv.encoding = append(inv.encoding, targetEncoding{document.Path, validation})
		if validation.Error != nil {
			status = exitIO
			report(document.Path + ": " + validation.Error.Message)
		}
		// Observe time/cancellation consumed by the bounded index/hash scan.
		// An established decoding error retains its I/O precedence.
		stop := ctx.Err()
		if stop == nil {
			stop = budget.ChargeWork(0)
		}
		if stop != nil {
			acquired.Interruptions = append(acquired.Interruptions, files.Interruption{Path: document.Path, Err: stop})
			report(document.Path + ": " + stop.Error())
			break
		}
	}
	if writeError {
		return exitIO
	}
	if status == exitIO {
		return status
	}
	if len(acquired.Interruptions) > 0 {
		status = exitIncomplete
	}
	if status == exitUsage {
		return status
	}
	if len(acquired.Interruptions) == 0 && selected.Empty() {
		return output(stderr, "mdchk: no files selected\n", exitUsage)
	}
	return output(stderr, "mdchk: Markdown analysis is not available in this build\n", exitIncomplete)
}
