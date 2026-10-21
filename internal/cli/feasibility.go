package cli

import (
	"context"
	"errors"
	"slices"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/result"
)

// The response envelope is known before project access. CLI paths are selection
// requests (possibly directories or exclusions), not known target identities;
// reserve actual target outcomes only after discovery establishes that list.
func invocationFeasibility(options lintOptions) error {
	out := result.NewLint("cli")
	err := reportFeasibility(out, options.format, options.limits)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

// Discovery is execution work. Reject its newly known infeasible target list
// before acquisition/analysis, recording the exact omitted not-started list.
// No acquired file, actual error, or attempted write outcome is discarded.
func (inv *lintInvocation) selectionFeasibility(selected *files.Selection) (int, bool) {
	out := result.NewLint("cli")
	inv.mergeAcquisition(&out, selected, &files.Acquisition{})
	engine.FinalizeLintResult(&out)
	err := reportFeasibility(out, inv.options.format, inv.options.limits)
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		return 0, false
	}
	if !errors.Is(err, errReportBudget) {
		return inv.fail(err, "report-preparation-failed", result.Internal), true
	}
	count := 0
	for _, f := range out.Files {
		if f.Analysis == "not-started" {
			count++
		}
	}
	out.Files = slices.DeleteFunc(out.Files, func(f result.File) bool { return f.Analysis == "not-started" })
	coverageCount := len(out.Coverage)
	out.Coverage = []result.Coverage{}
	if coverageCount > 0 {
		out.Omissions = append(out.Omissions, result.Omission{Section: "/coverage", Reason: "output-limit", Message: "pre-analysis target coverage records omitted with the rejected manifest", Count: &coverageCount})
	}
	out.Omissions = append(out.Omissions, result.Omission{Section: "/files", Reason: "output-limit", Message: "discovered target identities cannot fit the response; acquisition and analysis did not start", Count: &count})
	out.Interruptions = append(out.Interruptions, result.Interruption{Reason: "limit-exceeded", Phase: "discovery", Message: "mandatory discovered target manifest exceeds response-bytes"})
	engine.FinalizeLintResult(&out)
	inv.result = &out
	return lintExit(out), true
}
