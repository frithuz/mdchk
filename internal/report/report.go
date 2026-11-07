// Package report encodes mdchk results as text, JSON, VS Code problem matcher
// output and GitHub annotations within their response bounds. It also owns the
// exit statuses and the limit defaults those encodings report.
package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

var ErrReportBudget = errors.New("essential result manifest exceeds response-bytes")

// ReportResult owns presentation only. Its cleanup deadline starts after analysis;
// an expired analysis context cannot consume this independently reserved budget.
func ReportResult(out result.Result, format string, stdout io.Writer, limits map[string]int64) int {
	return ReportResultStreams(out, format, stdout, io.Discard, limits)
}

// ReportResultStreams owns both presentation streams without path authority.
// Commands use the context-bearing variant; legacy text/JSON remain stdout-only.
func ReportResultStreams(out result.Result, format string, stdout, stderr io.Writer, limits map[string]int64) int {
	return ReportResultStreamsContext(context.Background(), out, format, stdout, stderr, limits)
}

// ReportResultStreamsContext takes a presentation context that carries
// caller-supplied GitHub path authority.
func ReportResultStreamsContext(parent context.Context, out result.Result, format string, stdout, stderr io.Writer, limits map[string]int64) int {
	// Encode a fixed-work emergency reserve before starting the operator's
	// timed full-fallback/detail construction. It uses only precomputed counts
	// and bounded categories, never walks the potentially large source manifest.
	var reserve EncodedReport
	var reserveErr error
	if format == "github" {
		reserve, reserveErr = ReservedEditorReport(parent, out, limits, EncodeGitHub)
	} else if format == "vscode" {
		reserve, reserveErr = EmergencyVSCodeReport(out, limits)
	} else if out.Operation == "config validate" {
		reserve, reserveErr = EmergencyConfigReport(out, format, limits)
	} else if out.Operation == "fix" {
		reserve, reserveErr = emergencyFixReport(out, format, limits)
	} else if out.Operation == "lint" {
		reserve, reserveErr = MinimalLintReport(parent, out, format, limits, ReportingDeadlineStop())
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(ReportLimit(limits, "reporting-time")))
	defer cancel()
	encoded, err := BuildReport(ctx, out, format, limits)
	reserved := format == "github" || format == "vscode" || out.Operation == "config validate" || out.Operation == "fix" || out.Operation == "lint"
	if err != nil && ctx.Err() != nil && reserved && reserveErr == nil {
		encoded, err = reserve, nil
	}
	status := 0
	if reserved && reserveErr == nil {
		status = LintExit(reserve.Out)
	} else {
		status = LintExit(out)
	}
	if err != nil {
		if status == 14 {
			return status
		}
		return ExitIO
	}
	status = LintExit(encoded.Out)
	n, m := 0, 0
	var stderrErr error
	if len(encoded.Data) > 0 {
		n, err = stdout.Write(encoded.Data)
	}
	if len(encoded.Stderr) > 0 {
		m, stderrErr = stderr.Write(encoded.Stderr)
	}
	if err != nil || n != len(encoded.Data) || stderrErr != nil || m != len(encoded.Stderr) {
		if status == 14 {
			return status
		}
		return ExitIO
	}
	return status
}

type EncodedReport struct {
	Data   []byte
	Stderr []byte
	Out    result.Result
}

func ReportLimit(limits map[string]int64, name string) int64 {
	if v, ok := limits[name]; ok {
		return v
	}
	return LimitDefaults[name]
}

// reportFeasibility checks an already known manifest without discarding file or
// error identities. A caller may use this before acquisition when those records
// are known. Runtime failure is delivery IO, never a retroactive usage error.
func reportFeasibility(out result.Result, format string, limits map[string]int64) error {
	return ReportFeasibilityContext(context.Background(), out, format, limits)
}
func ReportFeasibilityContext(presentation context.Context, out result.Result, format string, limits map[string]int64) error {
	if presentation == nil {
		presentation = context.Background()
	}
	ctx, cancel := context.WithTimeout(presentation, time.Duration(ReportLimit(limits, "reporting-time")))
	defer cancel()
	// Reserve the minimum envelope/interruption/omission capacity in addition
	// to the entire known per-target identity/outcome manifest. The final fully
	// encoded report can then use all response bytes; this is an admission check,
	// not a permanent subtraction from successful presentation capacity.
	manifest := newReportOperation(out)
	owned := CloneReport(out)
	manifest.Files, manifest.CheckedFiles = owned.Files, owned.CheckedFiles
	setReportLimits(&manifest, limits)
	engine.FinalizeLintResult(&manifest)
	empty := newReportOperation(out)
	setReportLimits(&empty, limits)
	fullSize, err := ReportSize(ctx, manifest, "json")
	if err != nil {
		return err
	}
	emptySize, err := ReportSize(ctx, empty, "json")
	if err != nil {
		return err
	}
	bound, reserve := ReportLimit(limits, "response-bytes"), ReportLimit(limits, "report-reserve-bytes")
	if bound < reserve || fullSize-emptySize > bound-reserve {
		return ErrReportBudget
	}
	// Use the direct builder so a deadline fallback cannot count as proof that
	// the complete known manifest admission check finished.
	_, err = BuildReportCore(ctx, out, format, limits)
	return err
}

func BuildReport(ctx context.Context, original result.Result, format string, limits map[string]int64) (EncodedReport, error) {
	if format == "github" {
		return buildEditorReport(ctx, original, limits, EncodeGitHub)
	}
	if format == "vscode" {
		return buildVSCodeReport(ctx, original, limits)
	}
	// Construct the reserved partial envelope before spending the remaining time
	// on optional detail counting. If counting/encoding reaches the deadline,
	// this complete, already encoded envelope remains available for delivery.
	fallback, fallbackErr := DeadlineReport(ctx, original, format, limits)
	if errors.Is(fallbackErr, ErrReportBudget) {
		fallback, fallbackErr = MinimalLintReport(ctx, original, format, limits, ReportingDeadlineStop())
	}
	encoded, err := BuildReportCore(ctx, original, format, limits)
	if errors.Is(err, ErrReportBudget) {
		encoded, err = MinimalLintReport(ctx, original, format, limits, result.Interruption{Reason: "limit-exceeded", Phase: "reporting", Message: "essential detail records exceed response-bytes; whole records omitted"})
	}
	if err != nil && ctx.Err() != nil && fallbackErr == nil {
		return fallback, nil
	}
	return encoded, err
}

func ReportingDeadlineStop() result.Interruption {
	return result.Interruption{Reason: "deadline-exceeded", Phase: "reporting", Message: "reporting-time exhausted while constructing result details"}
}

// MinimalLintReport is lint's last resort when each file's first error and outcome
// do not fit, and its reserve for the reporting deadline, so an operator never gets
// an exit code with no explanation. It borrows the detail records and only counts
// them, so its work is linear with a small constant and has no search; the reserve
// is built before the reporting timer starts. Error records become category
// summaries that keep exit precedence. JSON then gives up checked_files, which
// files repeats, and file snapshots. File paths and outcomes stay: the pre-analysis
// manifest check reserved room for those, so it is not a builder for feasibility
// checks, which must still see the full essential report.
func MinimalLintReport(ctx context.Context, original result.Result, format string, limits map[string]int64, stop result.Interruption) (EncodedReport, error) {
	if original.Operation != "lint" {
		return EncodedReport{}, ErrReportBudget
	}
	if err := ctx.Err(); err != nil {
		return EncodedReport{}, err
	}
	bound := ReportLimit(limits, "response-bytes")
	out := newReportOperation(original)
	out.Files = slices.Clone(original.Files)
	out.CheckedFiles = slices.Clone(original.CheckedFiles)
	out.Omissions = slices.Clone(original.Omissions)
	out.Interruptions = slices.Clone(original.Interruptions)
	out.EffectiveLimits = slices.Clone(original.EffectiveLimits)
	setReportLimits(&out, limits)
	omit := func(section, message string, count int) {
		if count > 0 {
			out.Omissions = append(out.Omissions, result.Omission{Section: section, Reason: "output-limit", Message: message, Count: &count})
		}
	}
	const detail = "whole records omitted from the minimal report"
	omit("/diagnostics", detail, len(original.Diagnostics))
	omit("/suppressed_diagnostics", detail, len(original.SuppressedDiagnostics))
	omit("/notes", detail, len(original.Notes))
	omit("/coverage", detail, len(original.Coverage))
	omit("/skipped_files", detail, len(original.SkippedFiles))
	omit("/errors", "located error records replaced by category summaries", len(original.Errors))
	collapseEncodingRetention(&out)
	out.Errors = lintErrorSummaries(original.Errors)
	out.Interruptions = append(out.Interruptions, stop)
	engine.FinalizeLintResult(&out)
	n, err := ReportSize(ctx, out, format)
	if err != nil {
		return EncodedReport{}, err
	}
	// Text prints neither field, and its summary line counts checked_files.
	if n > bound && format == "json" {
		omit("/checked_files", "checked paths omitted; files lists every target and its outcome", len(out.CheckedFiles))
		out.CheckedFiles = []string{}
		engine.FinalizeLintResult(&out)
		if n, err = ReportSize(ctx, out, format); err != nil {
			return EncodedReport{}, err
		}
	}
	if n > bound && format == "json" {
		count := 0
		for i := range out.Files {
			if out.Files[i].InputSnapshot != nil || out.Files[i].ResultSnapshot != nil {
				out.Files[i].InputSnapshot, out.Files[i].ResultSnapshot = nil, nil
				count++
			}
		}
		omit("/files", "snapshot identities omitted from these file records; paths and outcomes remain", count)
		engine.FinalizeLintResult(&out)
		if n, err = ReportSize(ctx, out, format); err != nil {
			return EncodedReport{}, err
		}
	}
	if n > bound {
		return EncodedReport{}, ErrReportBudget
	}
	var data []byte
	if format == "json" {
		data, err = json.Marshal(out)
		data = append(data, '\n')
	} else {
		var b bytes.Buffer
		err = RenderText(ctx, &b, out)
		data = b.Bytes()
	}
	if err != nil {
		return EncodedReport{}, err
	}
	return EncodedReport{Data: data, Out: out}, ctx.Err()
}

// lintErrorSummaries keeps one record per encountered category and scope, so a
// summary cannot change the exit precedence or status the located errors had.
func lintErrorSummaries(records []result.Error) []result.Error {
	type key struct {
		category result.ErrorCategory
		scope    string
	}
	counts := map[key]int{}
	for _, e := range records {
		counts[key{e.Category, e.Scope}]++
	}
	summaries := []result.Error{}
	for _, category := range []result.ErrorCategory{result.Usage, result.Configuration, result.IO, result.Internal} {
		for _, scope := range []string{"operation", "observation"} {
			if count := counts[key{category, scope}]; count > 0 {
				summaries = append(summaries, result.Error{Category: category, Scope: scope, Code: "lint-error-summary", Message: fmt.Sprintf("%d encountered %s errors; located details omitted by bounded reporting.", count, category)})
			}
		}
	}
	return summaries
}

func BuildReportCore(ctx context.Context, original result.Result, format string, limits map[string]int64) (EncodedReport, error) {
	if format == "github" {
		return buildEditorReport(ctx, original, limits, EncodeGitHub)
	}
	if format == "vscode" {
		return buildVSCodeReport(ctx, original, limits)
	}
	if err := ctx.Err(); err != nil {
		return EncodedReport{}, err
	}
	out := CloneReport(original)
	setReportLimits(&out, limits)
	engine.FinalizeLintResult(&out)
	bound := ReportLimit(limits, "response-bytes")
	fits := func() (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		n, err := ReportSize(ctx, out, format)
		return n <= bound, err
	}
	ok, err := fits()
	if err != nil {
		return EncodedReport{}, err
	}
	if !ok {
		// Keep every actual error, interruption and target outcome. Reducing a
		// coverage detail cannot turn missing analysis into successful coverage.
		observed, sizeErr := ReportSize(ctx, out, format)
		if sizeErr != nil {
			return EncodedReport{}, sizeErr
		}
		out.Interruptions = append(out.Interruptions, result.Interruption{Reason: "limit-exceeded", Phase: "reporting", Message: "result details exceed response-bytes", Limit: &result.LimitObservation{Name: "response-bytes", Observed: observed, Allowed: bound, Unit: "bytes"}})
		out, err = reduceEncodingErrors(ctx, out, format, bound, true)
		if err != nil {
			return EncodedReport{}, err
		}
		if fitsNow, e := fits(); e != nil {
			return EncodedReport{}, e
		} else if !fitsNow && out.Operation == "fix" {
			omitFixDiffs(&out)
			engine.FinalizeLintResult(&out)
		}
		notes, suppressed, diagnostics, coverage, skipped := out.Notes, out.SuppressedDiagnostics, out.Diagnostics, out.Coverage, out.SkippedFiles
		deferred := out.DeferredFixes
		families := []struct {
			section string
			length  int
			trim    func(int)
		}{
			{"/deferred_fixes", len(out.DeferredFixes), func(n int) { out.DeferredFixes = deferred[:n] }},
			{"/notes", len(out.Notes), func(n int) { out.Notes = notes[:n] }},
			{"/suppressed_diagnostics", len(out.SuppressedDiagnostics), func(n int) { out.SuppressedDiagnostics = suppressed[:n] }},
			{"/diagnostics", len(out.Diagnostics), func(n int) { out.Diagnostics = diagnostics[:n] }},
			{"/coverage", len(out.Coverage), func(n int) { out.Coverage = coverage[:n] }},
			{"/skipped_files", len(out.SkippedFiles), func(n int) { out.SkippedFiles = skipped[:n] }},
		}
		for _, f := range families {
			if fitsNow, e := fits(); e != nil {
				return EncodedReport{}, e
			} else if fitsNow {
				break
			}
			if f.length == 0 {
				continue
			}
			count := f.length
			out.Omissions = append(out.Omissions, result.Omission{Section: f.section, Reason: "output-limit", Message: "whole ordered records omitted to satisfy response-bytes", Count: &count})
			// First discard the whole family. If that suffices, find the largest
			// ordered prefix by logarithmic counting passes, encoding only once.
			f.trim(0)
			engine.FinalizeLintResult(&out)
			ok, err = fits()
			if err != nil {
				return EncodedReport{}, err
			}
			if !ok {
				continue
			}
			lo, hi := 0, f.length
			for lo < hi {
				mid := lo + (hi-lo+1)/2
				f.trim(mid)
				count = f.length - mid
				engine.FinalizeLintResult(&out)
				fitsNow, e := fits()
				if e != nil {
					return EncodedReport{}, e
				}
				if fitsNow {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			f.trim(lo)
			count = f.length - lo
			if count == 0 {
				out.Omissions = out.Omissions[:len(out.Omissions)-1]
			}
			engine.FinalizeLintResult(&out)
			break
		}
		ok, err = fits()
		if err != nil {
			return EncodedReport{}, err
		}
		if !ok && collapseEncodingRetention(&out) {
			engine.FinalizeLintResult(&out)
			ok, err = fits()
			if err != nil {
				return EncodedReport{}, err
			}
		}
		if !ok {
			summarizeErrors(&out)
			engine.FinalizeLintResult(&out)
			ok, err = fits()
			if err != nil {
				return EncodedReport{}, err
			}
			if !ok && out.Operation == "config validate" {
				if err := reduceConfigManifest(ctx, &out, format, bound); err != nil {
					return EncodedReport{}, err
				}
				ok, err = fits()
				if err != nil {
					return EncodedReport{}, err
				}
			}
			if !ok {
				return EncodedReport{}, ErrReportBudget
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return EncodedReport{}, err
	}
	var data []byte
	if format == "json" {
		data, err = json.Marshal(out)
		data = append(data, '\n')
	} else {
		var b bytes.Buffer
		err = RenderText(ctx, &b, out)
		data = b.Bytes()
	}
	if err != nil {
		return EncodedReport{}, err
	}
	if int64(len(data)) > bound {
		return EncodedReport{}, ErrReportBudget
	}
	if err := ctx.Err(); err != nil {
		return EncodedReport{}, err
	}
	return EncodedReport{Data: data, Out: out}, nil
}

func CloneReport(r result.Result) result.Result {
	r.Changes = slices.Clone(r.Changes)
	for i := range r.Changes {
		r.Changes[i].Rules = slices.Clone(r.Changes[i].Rules)
		if r.Changes[i].Diff != nil {
			d := *r.Changes[i].Diff
			d.Hunks = slices.Clone(d.Hunks)
			for j := range d.Hunks {
				d.Hunks[j].Lines = slices.Clone(d.Hunks[j].Lines)
			}
			r.Changes[i].Diff = &d
		}
	}
	r.DeferredFixes = slices.Clone(r.DeferredFixes)
	for i := range r.DeferredFixes {
		r.DeferredFixes[i].CauseIDs = slices.Clone(r.DeferredFixes[i].CauseIDs)
		r.DeferredFixes[i].ConflictGroupIDs = slices.Clone(r.DeferredFixes[i].ConflictGroupIDs)
	}
	if r.ConfigValidation != nil {
		summary := *r.ConfigValidation
		r.ConfigValidation = &summary
	}
	r.CheckedFiles = slices.Clone(r.CheckedFiles)
	r.SkippedFiles = slices.Clone(r.SkippedFiles)
	r.Diagnostics = slices.Clone(r.Diagnostics)
	r.Coverage = slices.Clone(r.Coverage)
	for i := range r.Coverage {
		r.Coverage[i].CauseIDs = slices.Clone(r.Coverage[i].CauseIDs)
	}
	r.Notes = slices.Clone(r.Notes)
	r.SuppressedDiagnostics = slices.Clone(r.SuppressedDiagnostics)
	r.Errors = slices.Clone(r.Errors)
	r.Interruptions = slices.Clone(r.Interruptions)
	r.Omissions = slices.Clone(r.Omissions)
	r.EffectiveLimits = slices.Clone(r.EffectiveLimits)
	r.Files = slices.Clone(r.Files)
	for i := range r.Files {
		r.Files[i].Leftovers = slices.Clone(r.Files[i].Leftovers)
		r.Files[i].ErrorIDs = slices.Clone(r.Files[i].ErrorIDs)
		r.Files[i].CoverageIDs = slices.Clone(r.Files[i].CoverageIDs)
		r.Files[i].ChangeIDs = slices.Clone(r.Files[i].ChangeIDs)
	}
	return r
}

func TextField(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\v':
			b.WriteString(`\v`)
		default:
			if terminalControl(r) {
				if r < 128 {
					fmt.Fprintf(&b, `\x%02x`, r)
				} else {
					fmt.Fprintf(&b, `\u%04x`, r)
				}
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
func terminalControl(r rune) bool {
	return unicode.IsControl(r) || r == 0x2028 || r == 0x2029 || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069
}

func RenderText(ctx context.Context, w io.Writer, r result.Result) error {
	if r.Operation == "fix" {
		if err := renderFixText(ctx, w, r); err != nil {
			return err
		}
	}
	if r.Operation == "inspect" && len(r.Inspection) > 0 {
		if err := renderInspectionRawText(ctx, w, r.Inspection); err != nil {
			return err
		}
	}
	line := func(format string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := fmt.Fprintf(w, format, args...)
		return err
	}
	for _, d := range r.Diagnostics {
		fix := "manual"
		if d.FixAvailable {
			fix = "fixable"
		}
		if err := line("%s:%d:%d %s: %s [%s]\n", TextField(d.Path), d.Range.Start.Line, d.Range.Start.Column, TextField(d.Rule), TextField(d.Message), fix); err != nil {
			return err
		}
	}
	for _, raw := range r.Notes {
		var n result.Note
		if err := json.Unmarshal(raw, &n); err != nil {
			return err
		}
		location := TextField(n.Path)
		if n.Range != nil {
			location += fmt.Sprintf(":%d:%d", n.Range.Start.Line, n.Range.Start.Column)
		}
		if err := line("%s note %s %s: %s\n", location, TextField(n.Code), TextField(n.Rule), TextField(n.Message)); err != nil {
			return err
		}
	}
	for _, raw := range r.SuppressedDiagnostics {
		var s result.SuppressedDiagnostic
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		d := s.Diagnostic
		if err := line("%s:%d:%d suppressed %s (%s): %s\n", TextField(d.Path), d.Range.Start.Line, d.Range.Start.Column, TextField(d.Rule), TextField(s.SuppressedBy.Kind), TextField(d.Message)); err != nil {
			return err
		}
	}
	for _, e := range r.Errors {
		location := ""
		if e.Path != "" {
			location = TextField(e.Path)
			if e.Range != nil {
				location += fmt.Sprintf(":%d:%d", e.Range.Start.Line, e.Range.Start.Column)
			}
			location += " "
		}
		if err := line("%serror %s (%s): %s\n", location, TextField(e.Code), TextField(string(e.Category)), TextField(e.Message)); err != nil {
			return err
		}
	}
	for _, c := range r.Coverage {
		location := TextField(c.Path)
		if c.Path != "" && c.Range != nil {
			location += fmt.Sprintf(":%d:%d", c.Range.Start.Line, c.Range.Start.Column)
		}
		if err := line("coverage %s %s %s: %s\n", location, TextField(c.Rule), TextField(c.Reason), TextField(c.Message)); err != nil {
			return err
		}
	}
	for _, s := range r.SkippedFiles {
		if err := line("skipped %s (%s): %s\n", TextField(s.Path), TextField(s.Reason), TextField(s.Message)); err != nil {
			return err
		}
	}
	for _, s := range r.Interruptions {
		if err := line("interrupted %s %s (%s): %s\n", TextField(s.Phase), TextField(s.Path), TextField(s.Reason), TextField(s.Message)); err != nil {
			return err
		}
	}
	for _, o := range r.Omissions {
		count := "unknown"
		if o.Count != nil {
			count = fmt.Sprint(*o.Count)
		}
		if err := line("omitted %s %s %s (%s): %s\n", count, TextField(o.Section), TextField(o.Path), TextField(o.Reason), TextField(o.Message)); err != nil {
			return err
		}
	}
	for _, f := range r.Files {
		if f.Analysis != "complete" || r.Operation == "config validate" || r.Operation == "fix" {
			kind := "file"
			if r.Operation == "config validate" {
				kind = "definition"
			}
			if err := line("%s %s: analysis %s\n", kind, TextField(f.Path), TextField(f.Analysis)); err != nil {
				return err
			}
		}
	}
	if r.Operation == "inspect" {
		return line("Inspected %d files. %d findings, %d suppressed observations; %s (complete: %t).\n", len(r.CheckedFiles), len(r.Diagnostics), len(r.SuppressedDiagnostics), TextField(r.Status), r.Complete)
	}
	if r.ConfigValidation != nil {
		s := r.ConfigValidation
		return line("Checked %d definitions: %d valid, %d invalid, %d known definitions skipped. %d skipped paths, %d errors; %s (scan complete: %t, complete: %t).\n", s.DefinitionsChecked, s.ValidDefinitions, s.InvalidDefinitions, s.SkippedDefinitions, len(r.SkippedFiles), len(r.Errors), TextField(r.Status), s.ScanComplete, r.Complete)
	}
	return line("Checked %d files. Found %d violations. %d skipped, %d errors, %d coverage deficits; %s (complete: %t).\n", len(r.CheckedFiles), len(r.Diagnostics), len(r.SkippedFiles), len(r.Errors), len(r.Coverage), TextField(r.Status), r.Complete)
}

func setReportLimits(out *result.Result, limits map[string]int64) {
	// Only limits enforced by this adapter belong here. Preserve other applicable
	// limits from acquisition, replacing the engine's fixed reporting default.
	for _, name := range []string{"response-bytes", "reporting-time", "report-reserve-bytes"} {
		out.EffectiveLimits = slices.DeleteFunc(out.EffectiveLimits, func(v result.EffectiveLimit) bool { return v.Name == name })
		unit := "bytes"
		if name == "reporting-time" {
			unit = "nanoseconds"
		}
		source := "default"
		if _, ok := limits[name]; ok {
			source = "operator"
		}
		out.EffectiveLimits = append(out.EffectiveLimits, result.EffectiveLimit{Name: name, Value: ReportLimit(limits, name), Unit: unit, Source: source})
	}
	sort.Slice(out.EffectiveLimits, func(i, j int) bool { return out.EffectiveLimits[i].Name < out.EffectiveLimits[j].Name })
}

// Error identities and semantic categories remain essential even when their
// detailed prose does not fit. Replace an entire detailed message with an
// explicit summary; never return a truncated prefix as if it were the message.
func summarizeErrors(out *result.Result) {
	const summary = "Error details omitted to satisfy response-bytes."
	count := 0
	for i := range out.Errors {
		if len(out.Errors[i].Message) > len(summary) {
			out.Errors[i].Message = summary
			count++
		}
	}
	if count > 0 {
		out.Omissions = append(out.Omissions, result.Omission{Section: "/errors", Reason: "output-limit", Message: "detailed error records replaced by identity-preserving summaries", Count: &count})
	}
}

// reduceEncodingErrors omits a lint or fix result's excess invalid-utf8 records and
// always keeps each file's first. Past that first record they repeat one failure,
// so they go before any other detail. With search it keeps the largest prefix that
// fits, by a logarithmic number of whole-report attempts. Without search it omits
// them all in one attempt, for the deadline fallback, whose job is to be ready. The
// caller has established that the whole result does not fit. If nothing fits, every
// excess record is omitted and the later reductions still apply.
func reduceEncodingErrors(ctx context.Context, out result.Result, format string, bound int64, search bool) (result.Result, error) {
	if out.Operation != "lint" && out.Operation != "fix" {
		return out, nil
	}
	var excess []int
	first := map[string]bool{}
	for i, e := range out.Errors {
		if e.Code == "invalid-utf8" {
			if first[e.Path] {
				excess = append(excess, i)
			}
			first[e.Path] = true
		}
	}
	if len(excess) == 0 {
		return out, nil
	}
	// Finalizing renumbers errors and rewrites links in place, so each attempt
	// starts from its own copy of the same canonical records.
	attempt := func(keep int) (result.Result, bool, error) {
		r := CloneReport(out)
		dropped := map[string]bool{}
		for _, i := range excess[keep:] {
			dropped[out.Errors[i].ID] = true
		}
		isDropped := func(id string) bool { return dropped[id] }
		r.Errors = slices.DeleteFunc(r.Errors, func(e result.Error) bool { return dropped[e.ID] })
		// An old ID left here could later alias a renumbered surviving error.
		for i := range r.Coverage {
			r.Coverage[i].CauseIDs = slices.DeleteFunc(r.Coverage[i].CauseIDs, isDropped)
		}
		for i := range r.DeferredFixes {
			r.DeferredFixes[i].CauseIDs = slices.DeleteFunc(r.DeferredFixes[i].CauseIDs, isDropped)
		}
		count := len(excess) - keep
		r.Omissions = append(r.Omissions, result.Omission{Section: "/errors", Reason: "output-limit", Message: "trailing invalid UTF-8 records omitted to satisfy response-bytes", Count: &count})
		engine.FinalizeLintResult(&r)
		n, err := ReportSize(ctx, r, format)
		return r, n <= bound, err
	}
	best, ok, err := attempt(0)
	if err != nil || !ok || !search {
		return best, err
	}
	for lo, hi := 0, len(excess)-1; lo < hi; {
		mid := lo + (hi-lo+1)/2
		r, ok, err := attempt(mid)
		if err != nil {
			return out, err
		}
		if ok {
			best, lo = r, mid
		} else {
			hi = mid - 1
		}
	}
	return best, nil
}

// collapseEncodingRetention replaces the file-scoped retention omissions with one
// path-less omission holding their exact total. They grow with the number of
// invalid files, so they must not be what stops a report from fitting. It reports
// whether there was anything to collapse.
func collapseEncodingRetention(out *result.Result) bool {
	total, found := 0, false
	kept := make([]result.Omission, 0, len(out.Omissions))
	for _, o := range out.Omissions {
		if !o.EncodingRetention() {
			kept = append(kept, o)
			continue
		}
		found = true
		if o.Count != nil {
			total += *o.Count
		}
	}
	if !found {
		return false
	}
	out.Omissions = append(kept, result.Omission{Section: "/errors", Reason: "output-limit", Message: "invalid UTF-8 records past the retention cap omitted; per-file counts combined by bounded reporting", Count: &total})
	return true
}

func DeadlineReport(ctx context.Context, original result.Result, format string, limits map[string]int64) (EncodedReport, error) {
	if format == "github" {
		return ReservedEditorReport(ctx, original, limits, EncodeGitHub)
	}
	if format == "vscode" {
		if err := ctx.Err(); err != nil {
			return EncodedReport{}, err
		}
		return reservedVSCodeReport(ctx, original, limits)
	}
	if err := ctx.Err(); err != nil {
		return EncodedReport{}, err
	}
	out := CloneReport(original)
	setReportLimits(&out, limits)
	engine.FinalizeLintResult(&out)
	for _, family := range []struct {
		section string
		count   int
	}{
		{"/diagnostics", len(out.Diagnostics)}, {"/coverage", len(out.Coverage)}, {"/notes", len(out.Notes)}, {"/suppressed_diagnostics", len(out.SuppressedDiagnostics)}, {"/skipped_files", len(out.SkippedFiles)},
	} {
		if family.count > 0 {
			count := family.count
			out.Omissions = append(out.Omissions, result.Omission{Section: family.section, Reason: "output-limit", Message: "reserved partial report omits whole detail records", Count: &count})
		}
	}
	if out.Operation == "fix" {
		omitFixDiffs(&out)
		omitFixDeferred(&out)
	}
	out.Diagnostics = []result.Diagnostic{}
	out.Coverage = []result.Coverage{}
	out.Notes = []json.RawMessage{}
	out.SuppressedDiagnostics = []json.RawMessage{}
	out.SkippedFiles = []result.SkippedFile{}
	out.Interruptions = append(out.Interruptions, result.Interruption{Reason: "deadline-exceeded", Phase: "reporting", Message: "reporting-time exhausted while constructing result details"})
	engine.FinalizeLintResult(&out)
	n, err := ReportSize(ctx, out, format)
	if err != nil {
		return EncodedReport{}, err
	}
	if n > ReportLimit(limits, "response-bytes") {
		out, err = reduceEncodingErrors(ctx, out, format, ReportLimit(limits, "response-bytes"), false)
		if err != nil {
			return EncodedReport{}, err
		}
		n, err = ReportSize(ctx, out, format)
		if err != nil {
			return EncodedReport{}, err
		}
	}
	if n > ReportLimit(limits, "response-bytes") && collapseEncodingRetention(&out) {
		engine.FinalizeLintResult(&out)
		n, err = ReportSize(ctx, out, format)
		if err != nil {
			return EncodedReport{}, err
		}
	}
	if n > ReportLimit(limits, "response-bytes") {
		summarizeErrors(&out)
		engine.FinalizeLintResult(&out)
		n, err = ReportSize(ctx, out, format)
		if err != nil {
			return EncodedReport{}, err
		}
	}
	if n > ReportLimit(limits, "response-bytes") && out.Operation == "config validate" {
		if err := reduceConfigManifest(ctx, &out, format, ReportLimit(limits, "response-bytes")); err != nil {
			return EncodedReport{}, err
		}
		n, err = ReportSize(ctx, out, format)
		if err != nil {
			return EncodedReport{}, err
		}
	}
	if n > ReportLimit(limits, "response-bytes") {
		return EncodedReport{}, ErrReportBudget
	}
	var data []byte
	if format == "json" {
		data, err = json.Marshal(out)
		data = append(data, '\n')
	} else {
		var b bytes.Buffer
		err = RenderText(ctx, &b, out)
		data = b.Bytes()
	}
	if err != nil {
		return EncodedReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return EncodedReport{}, err
	}
	return EncodedReport{Data: data, Out: out}, nil
}

func newReportOperation(out result.Result) result.Result {
	if out.Operation == "fix" {
		return result.NewFix(out.DryRun != nil && *out.DryRun)
	}
	if out.Operation == "config validate" {
		r := result.NewConfigValidation()
		if out.ConfigValidation != nil {
			s := *out.ConfigValidation
			r.ConfigValidation = &s
		}
		return r
	}
	return result.NewLint(out.Mode)
}

// Configuration scans are read-only and can enumerate more completed outcomes
// than a response can retain. Preserve authoritative counts and scan state,
// explicitly accounting for whole known records omitted from the presentation.
// Lint's pre-admitted manifest and any future write outcomes are not reduced here.
func reduceConfigManifest(ctx context.Context, out *result.Result, format string, bound int64) error {
	omit := func(section string, count int) {
		if count > 0 {
			n := count
			out.Omissions = append(out.Omissions, result.Omission{Section: section, Reason: "output-limit", Message: "whole known configuration records omitted to satisfy response-bytes; aggregate scan counts are retained", Count: &n})
		}
	}
	omit("/files", len(out.Files))
	omit("/checked_files", len(out.CheckedFiles))
	out.Files = []result.File{}
	out.CheckedFiles = []string{}
	engine.FinalizeLintResult(out)
	n, err := ReportSize(ctx, *out, format)
	if err != nil || n <= bound {
		return err
	}
	// Retain every encountered category even when individual source identities
	// cannot fit. The explicit omissions distinguish these aggregate summaries
	// from complete located errors, and preserve operation-specific exit precedence.
	counts := map[result.ErrorCategory]int{}
	representatives := map[result.ErrorCategory]result.Error{}
	for _, e := range out.Errors {
		counts[e.Category]++
		if _, ok := representatives[e.Category]; !ok {
			representatives[e.Category] = e
		}
	}
	original := len(out.Errors)
	out.Errors = []result.Error{}
	for _, category := range []result.ErrorCategory{result.Usage, result.Configuration, result.IO, result.Internal} {
		if count := counts[category]; count > 0 {
			e := representatives[category]
			e.Path = ""
			e.Rule = ""
			e.Range = nil
			e.Snapshot = nil
			e.ByteOffset = nil
			e.ByteRange = nil
			e.Code = "config-error-summary"
			e.Message = fmt.Sprintf("%d encountered %s errors; located details omitted to satisfy response-bytes.", count, category)
			out.Errors = append(out.Errors, e)
		}
	}
	omit("/errors", original)
	engine.FinalizeLintResult(out)
	return nil
}

// EmergencyConfigReport has work and output bounded by a fixed record count.
// Source identities and prose are replaced by exact omissions; actual scan and
// validity counts and every encountered error category remain authoritative.
func EmergencyConfigReport(original result.Result, format string, limits map[string]int64) (EncodedReport, error) {
	if format == "github" {
		return ReservedEditorReport(context.Background(), original, limits, EncodeGitHub)
	}
	if format == "vscode" {
		return EmergencyVSCodeReport(original, limits)
	}
	out := newReportOperation(original)
	out.Errors = original.ReportingErrorSummaries()
	omit := func(section string, count int) {
		if count > 0 {
			n := count
			out.Omissions = append(out.Omissions, result.Omission{Section: section, Reason: "output-limit", Message: "reserved deadline report omits known source details; aggregate counts are retained", Count: &n})
		}
	}
	omit("/files", len(original.Files))
	omit("/checked_files", len(original.CheckedFiles))
	omit("/skipped_files", len(original.SkippedFiles))
	omit("/errors", len(original.Errors))
	// A configuration scan has at most one operation interruption. Reconstruct
	// its bounded semantic fields instead of copying source-derived prose.
	if len(original.Interruptions) > 0 {
		stop := original.Interruptions[0]
		stop.ID = ""
		stop.Path = ""
		stop.Message = "Configuration scanning was interrupted; detailed reporting was interrupted as well."
		out.Interruptions = append(out.Interruptions, stop)
		if len(original.Interruptions) > 1 {
			omit("/interruptions", len(original.Interruptions)-1)
		}
	}
	out.Interruptions = append(out.Interruptions, result.Interruption{Reason: "deadline-exceeded", Phase: "reporting", Message: "reporting-time exhausted while constructing result details"})
	// Applicable limits have a fixed vocabulary in this adapter; copy only those
	// records and rebuild reporting limits from the validated operator profile.
	for _, limit := range original.EffectiveLimits {
		if ValidationLimitApplicable(limit.Name) {
			out.EffectiveLimits = append(out.EffectiveLimits, limit)
		}
	}
	setReportLimits(&out, limits)
	engine.FinalizeLintResult(&out)
	var data []byte
	var err error
	if format == "json" {
		data, err = json.Marshal(out)
		data = append(data, '\n')
	} else {
		var b bytes.Buffer
		err = RenderText(context.Background(), &b, out)
		data = b.Bytes()
	}
	if err != nil {
		return EncodedReport{}, err
	}
	if int64(len(data)) > ReportLimit(limits, "response-bytes") {
		return EncodedReport{}, ErrReportBudget
	}
	return EncodedReport{Data: data, Out: out}, nil
}
