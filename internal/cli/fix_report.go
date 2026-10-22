package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
	"io"
	"slices"
)

func omitFixDiffs(out *result.Result) {
	for i := range out.Changes {
		if out.Changes[i].Diff != nil {
			count := 1
			out.Omissions = append(out.Omissions, result.Omission{Section: fmt.Sprintf("/changes/%d/diff", i), Path: out.Changes[i].Path, Reason: "output-limit", Message: "whole optional final diff omitted", Count: &count})
			out.Changes[i].Diff = nil
		}
	}
}
func omitFixDeferred(out *result.Result) {
	if n := len(out.DeferredFixes); n > 0 {
		out.Omissions = append(out.Omissions, result.Omission{Section: "/deferred_fixes", Reason: "output-limit", Message: "whole deferred records omitted", Count: &n})
		out.DeferredFixes = []result.DeferredFix{}
	}
}

// emergencyFixReport encodes only essential owned outcomes, proposal identities,
// fixed category summaries and collection lengths before timed detail work. It
// never clones diff hunks, walks diagnostic strings, or marshals the full result.
// The outcome/proposal manifest was admitted before acquisition.
func emergencyFixReport(original result.Result, format string, limits map[string]int64) (encodedReport, error) {
	if format == "vscode" {
		return emergencyVSCodeReport(original, limits)
	}
	out := result.NewFix(original.DryRun != nil && *original.DryRun)
	out.Files = slices.Clone(original.Files)
	out.CheckedFiles = slices.Clone(original.CheckedFiles)
	out.EffectiveLimits = slices.Clone(original.EffectiveLimits)
	out.Omissions = slices.Clone(original.Omissions)
	out.Changes = make([]result.Change, len(original.Changes))
	for i, c := range original.Changes {
		c.Diff = nil
		c.Rules = slices.Clone(c.Rules)
		out.Changes[i] = c
	}
	for i, c := range original.Changes {
		if c.Diff != nil {
			n := 1
			out.Omissions = append(out.Omissions, result.Omission{Section: fmt.Sprintf("/changes/%d/diff", i), Path: c.Path, Reason: "output-limit", Message: "whole optional final diff omitted by reporting deadline", Count: &n})
		}
	}
	for _, f := range []struct {
		section string
		n       int
	}{{"/diagnostics", len(original.Diagnostics)}, {"/coverage", len(original.Coverage)}, {"/suppressed_diagnostics", len(original.SuppressedDiagnostics)}, {"/notes", len(original.Notes)}, {"/skipped_files", len(original.SkippedFiles)}, {"/deferred_fixes", len(original.DeferredFixes)}, {"/errors", len(original.Errors)}, {"/interruptions", len(original.Interruptions)}} {
		if f.n > 0 {
			n := f.n
			out.Omissions = append(out.Omissions, result.Omission{Section: f.section, Reason: "output-limit", Message: "whole detail records omitted by reporting deadline", Count: &n})
		}
	}
	out.Errors = original.ReportingErrorSummaries()
	for i := range out.Errors {
		out.Errors[i].Code = "fix-error-summary"
	}
	out.Interruptions = append(out.Interruptions, result.Interruption{Reason: "deadline-exceeded", Phase: "reporting", Message: "reporting-time exhausted while constructing result details"})
	setReportLimits(&out, limits)
	engine.FinalizeLintResult(&out)
	n, err := reportSize(context.Background(), out, format)
	if err != nil {
		return encodedReport{}, err
	}
	if n > reportLimit(limits, "response-bytes") {
		return encodedReport{}, errReportBudget
	}
	var data []byte
	if format == "json" {
		data, err = json.Marshal(out)
		data = append(data, '\n')
	} else {
		var b bytes.Buffer
		err = renderText(context.Background(), &b, out)
		data = b.Bytes()
	}
	return encodedReport{data: data, out: out}, err
}
func renderFixText(ctx context.Context, w io.Writer, r result.Result) error {
	line := func(format string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := fmt.Fprintf(w, format, args...)
		return err
	}
	for _, c := range r.Changes {
		if err := line("%s %s: %d validated edits (%s) %s -> %s\n", textField(c.Disposition), textField(c.Path), c.EditCount, textField(joinRules(c.Rules)), c.Before.SHA256, c.Candidate.SHA256); err != nil {
			return err
		}
		if c.Diff == nil {
			continue
		}
		if err := line("--- %s (original)\n+++ %s (candidate)\n", textField(c.Path), textField(c.Path)); err != nil {
			return err
		}
		for _, h := range c.Diff.Hunks {
			if err := line("@@ -%d,%d +%d,%d @@\n", h.OldStart, h.OldLines, h.NewStart, h.NewLines); err != nil {
				return err
			}
			for _, l := range h.Lines {
				prefix := " "
				if l.Kind == "add" {
					prefix = "+"
				}
				if l.Kind == "remove" {
					prefix = "-"
				}
				if err := line("%s%s [%s]\n", prefix, textField(l.Text), l.LineEnding); err != nil {
					return err
				}
				if l.LineEnding == "none" {
					if err := line("\\ No newline at end of file\n"); err != nil {
						return err
					}
				}
			}
		}
	}
	for _, d := range r.DeferredFixes {
		if err := line("deferred %s %s %s (%s): %s\n", textField(d.Path), textField(d.Rule), textField(d.GroupID), textField(d.Reason), textField(d.Message)); err != nil {
			return err
		}
	}
	for _, f := range r.Files {
		if err := line("outcome %s: %s\n", textField(f.Path), textField(f.WriteStatus)); err != nil {
			return err
		}
		for _, name := range f.Leftovers {
			if err := line("owned staging leftover for %s: %s\n", textField(f.Path), textField(name)); err != nil {
				return err
			}
		}
	}
	return nil
}
func joinRules(ids []string) string {
	var b bytes.Buffer
	for i, id := range ids {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(id)
	}
	return b.String()
}
