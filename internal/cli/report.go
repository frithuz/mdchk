package cli

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

var errReportBudget = errors.New("essential result manifest exceeds response-bytes")

// reportResult owns presentation only. Its cleanup deadline starts after analysis;
// an expired analysis context cannot consume this independently reserved budget.
func reportResult(out result.Result, format string, stdout io.Writer, limits map[string]int64) int {
	return reportResultStreams(out, format, stdout, io.Discard, limits)
}

// reportResultStreams owns both presentation streams. Command selection is wired
// separately; legacy text/JSON retain their existing stdout-only behavior.
func reportResultStreams(out result.Result, format string, stdout, stderr io.Writer, limits map[string]int64) int {
	// Encode a fixed-work emergency reserve before starting the operator's
	// timed full-fallback/detail construction. It uses only precomputed counts
	// and bounded categories, never walks the potentially large source manifest.
	var reserve encodedReport
	var reserveErr error
	if format == "vscode" {
		reserve, reserveErr = emergencyVSCodeReport(out, limits)
	} else if out.Operation == "config validate" {
		reserve, reserveErr = emergencyConfigReport(out, format, limits)
	} else if out.Operation == "fix" {
		reserve, reserveErr = emergencyFixReport(out, format, limits)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(reportLimit(limits, "reporting-time")))
	defer cancel()
	encoded, err := buildReport(ctx, out, format, limits)
	if err != nil && ctx.Err() != nil && (format == "vscode" || out.Operation == "config validate" || out.Operation == "fix") && reserveErr == nil {
		encoded, err = reserve, nil
	}
	status := 0
	if (format == "vscode" || out.Operation == "config validate" || out.Operation == "fix") && reserveErr == nil {
		status = lintExit(reserve.out)
	} else {
		status = lintExit(out)
	}
	if err != nil {
		if status == 14 {
			return status
		}
		return exitIO
	}
	status = lintExit(encoded.out)
	n, m := 0, 0
	var stderrErr error
	if len(encoded.data) > 0 {
		n, err = stdout.Write(encoded.data)
	}
	if len(encoded.stderr) > 0 {
		m, stderrErr = stderr.Write(encoded.stderr)
	}
	if err != nil || n != len(encoded.data) || stderrErr != nil || m != len(encoded.stderr) {
		if status == 14 {
			return status
		}
		return exitIO
	}
	return status
}

type encodedReport struct {
	data   []byte
	stderr []byte
	out    result.Result
}

func reportLimit(limits map[string]int64, name string) int64 {
	if v, ok := limits[name]; ok {
		return v
	}
	return limitDefaults[name]
}

// reportFeasibility checks an already known manifest without discarding file or
// error identities. A caller may use this before acquisition when those records
// are known. Runtime failure is delivery IO, never a retroactive usage error.
func reportFeasibility(out result.Result, format string, limits map[string]int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(reportLimit(limits, "reporting-time")))
	defer cancel()
	// Reserve the minimum envelope/interruption/omission capacity in addition
	// to the entire known per-target identity/outcome manifest. The final fully
	// encoded report can then use all response bytes; this is an admission check,
	// not a permanent subtraction from successful presentation capacity.
	manifest := newReportOperation(out)
	owned := cloneReport(out)
	manifest.Files, manifest.CheckedFiles = owned.Files, owned.CheckedFiles
	setReportLimits(&manifest, limits)
	engine.FinalizeLintResult(&manifest)
	empty := newReportOperation(out)
	setReportLimits(&empty, limits)
	fullSize, err := reportSize(ctx, manifest, "json")
	if err != nil {
		return err
	}
	emptySize, err := reportSize(ctx, empty, "json")
	if err != nil {
		return err
	}
	bound, reserve := reportLimit(limits, "response-bytes"), reportLimit(limits, "report-reserve-bytes")
	if bound < reserve || fullSize-emptySize > bound-reserve {
		return errReportBudget
	}
	// Use the direct builder so a deadline fallback cannot count as proof that
	// the complete known manifest admission check finished.
	_, err = buildReportCore(ctx, out, format, limits)
	return err
}

func buildReport(ctx context.Context, original result.Result, format string, limits map[string]int64) (encodedReport, error) {
	if format == "vscode" {
		return buildVSCodeReport(ctx, original, limits)
	}
	// Construct the reserved partial envelope before spending the remaining time
	// on optional detail counting. If counting/encoding reaches the deadline,
	// this complete, already encoded envelope remains available for delivery.
	fallback, fallbackErr := deadlineReport(ctx, original, format, limits)
	encoded, err := buildReportCore(ctx, original, format, limits)
	if err != nil && ctx.Err() != nil && fallbackErr == nil {
		return fallback, nil
	}
	return encoded, err
}

func buildReportCore(ctx context.Context, original result.Result, format string, limits map[string]int64) (encodedReport, error) {
	if format == "vscode" {
		return buildVSCodeReport(ctx, original, limits)
	}
	if err := ctx.Err(); err != nil {
		return encodedReport{}, err
	}
	out := cloneReport(original)
	setReportLimits(&out, limits)
	engine.FinalizeLintResult(&out)
	bound := reportLimit(limits, "response-bytes")
	fits := func() (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		n, err := reportSize(ctx, out, format)
		return n <= bound, err
	}
	ok, err := fits()
	if err != nil {
		return encodedReport{}, err
	}
	if !ok {
		// Keep every actual error, interruption and target outcome. Reducing a
		// coverage detail cannot turn missing analysis into successful coverage.
		observed, sizeErr := reportSize(ctx, out, format)
		if sizeErr != nil {
			return encodedReport{}, sizeErr
		}
		out.Interruptions = append(out.Interruptions, result.Interruption{Reason: "limit-exceeded", Phase: "reporting", Message: "result details exceed response-bytes", Limit: &result.LimitObservation{Name: "response-bytes", Observed: observed, Allowed: bound, Unit: "bytes"}})
		if out.Operation == "fix" {
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
				return encodedReport{}, e
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
				return encodedReport{}, err
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
					return encodedReport{}, e
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
			return encodedReport{}, err
		}
		if !ok {
			summarizeErrors(&out)
			engine.FinalizeLintResult(&out)
			ok, err = fits()
			if err != nil {
				return encodedReport{}, err
			}
			if !ok && out.Operation == "config validate" {
				if err := reduceConfigManifest(ctx, &out, format, bound); err != nil {
					return encodedReport{}, err
				}
				ok, err = fits()
				if err != nil {
					return encodedReport{}, err
				}
			}
			if !ok {
				return encodedReport{}, errReportBudget
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return encodedReport{}, err
	}
	var data []byte
	if format == "json" {
		data, err = json.Marshal(out)
		data = append(data, '\n')
	} else {
		var b bytes.Buffer
		err = renderText(ctx, &b, out)
		data = b.Bytes()
	}
	if err != nil {
		return encodedReport{}, err
	}
	if int64(len(data)) > bound {
		return encodedReport{}, errReportBudget
	}
	if err := ctx.Err(); err != nil {
		return encodedReport{}, err
	}
	return encodedReport{data: data, out: out}, nil
}

func cloneReport(r result.Result) result.Result {
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

func textField(s string) string {
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

func renderText(ctx context.Context, w io.Writer, r result.Result) error {
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
		if err := line("%s:%d:%d %s: %s [%s]\n", textField(d.Path), d.Range.Start.Line, d.Range.Start.Column, textField(d.Rule), textField(d.Message), fix); err != nil {
			return err
		}
	}
	for _, raw := range r.Notes {
		var n result.Note
		if err := json.Unmarshal(raw, &n); err != nil {
			return err
		}
		location := textField(n.Path)
		if n.Range != nil {
			location += fmt.Sprintf(":%d:%d", n.Range.Start.Line, n.Range.Start.Column)
		}
		if err := line("%s note %s %s: %s\n", location, textField(n.Code), textField(n.Rule), textField(n.Message)); err != nil {
			return err
		}
	}
	for _, raw := range r.SuppressedDiagnostics {
		var s result.SuppressedDiagnostic
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		d := s.Diagnostic
		if err := line("%s:%d:%d suppressed %s (%s): %s\n", textField(d.Path), d.Range.Start.Line, d.Range.Start.Column, textField(d.Rule), textField(s.SuppressedBy.Kind), textField(d.Message)); err != nil {
			return err
		}
	}
	for _, e := range r.Errors {
		location := ""
		if e.Path != "" {
			location = textField(e.Path)
			if e.Range != nil {
				location += fmt.Sprintf(":%d:%d", e.Range.Start.Line, e.Range.Start.Column)
			}
			location += " "
		}
		if err := line("%serror %s (%s): %s\n", location, textField(e.Code), textField(string(e.Category)), textField(e.Message)); err != nil {
			return err
		}
	}
	for _, c := range r.Coverage {
		location := textField(c.Path)
		if c.Path != "" && c.Range != nil {
			location += fmt.Sprintf(":%d:%d", c.Range.Start.Line, c.Range.Start.Column)
		}
		if err := line("coverage %s %s %s: %s\n", location, textField(c.Rule), textField(c.Reason), textField(c.Message)); err != nil {
			return err
		}
	}
	for _, s := range r.SkippedFiles {
		if err := line("skipped %s (%s): %s\n", textField(s.Path), textField(s.Reason), textField(s.Message)); err != nil {
			return err
		}
	}
	for _, s := range r.Interruptions {
		if err := line("interrupted %s %s (%s): %s\n", textField(s.Phase), textField(s.Path), textField(s.Reason), textField(s.Message)); err != nil {
			return err
		}
	}
	for _, o := range r.Omissions {
		count := "unknown"
		if o.Count != nil {
			count = fmt.Sprint(*o.Count)
		}
		if err := line("omitted %s %s %s (%s): %s\n", count, textField(o.Section), textField(o.Path), textField(o.Reason), textField(o.Message)); err != nil {
			return err
		}
	}
	for _, f := range r.Files {
		if f.Analysis != "complete" || r.Operation == "config validate" || r.Operation == "fix" {
			kind := "file"
			if r.Operation == "config validate" {
				kind = "definition"
			}
			if err := line("%s %s: analysis %s\n", kind, textField(f.Path), textField(f.Analysis)); err != nil {
				return err
			}
		}
	}
	if r.Operation == "inspect" {
		return line("Inspected %d files. %d findings, %d suppressed observations; %s (complete: %t).\n", len(r.CheckedFiles), len(r.Diagnostics), len(r.SuppressedDiagnostics), textField(r.Status), r.Complete)
	}
	if r.ConfigValidation != nil {
		s := r.ConfigValidation
		return line("Checked %d definitions: %d valid, %d invalid, %d known definitions skipped. %d skipped paths, %d errors; %s (scan complete: %t, complete: %t).\n", s.DefinitionsChecked, s.ValidDefinitions, s.InvalidDefinitions, s.SkippedDefinitions, len(r.SkippedFiles), len(r.Errors), textField(r.Status), s.ScanComplete, r.Complete)
	}
	return line("Checked %d files. Found %d violations. %d skipped, %d errors, %d coverage deficits; %s (complete: %t).\n", len(r.CheckedFiles), len(r.Diagnostics), len(r.SkippedFiles), len(r.Errors), len(r.Coverage), textField(r.Status), r.Complete)
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
		out.EffectiveLimits = append(out.EffectiveLimits, result.EffectiveLimit{Name: name, Value: reportLimit(limits, name), Unit: unit, Source: source})
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

func deadlineReport(ctx context.Context, original result.Result, format string, limits map[string]int64) (encodedReport, error) {
	if format == "vscode" {
		if err := ctx.Err(); err != nil {
			return encodedReport{}, err
		}
		return reservedVSCodeReport(ctx, original, limits)
	}
	if err := ctx.Err(); err != nil {
		return encodedReport{}, err
	}
	out := cloneReport(original)
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
	n, err := reportSize(ctx, out, format)
	if err != nil {
		return encodedReport{}, err
	}
	if n > reportLimit(limits, "response-bytes") {
		summarizeErrors(&out)
		engine.FinalizeLintResult(&out)
		n, err = reportSize(ctx, out, format)
		if err != nil {
			return encodedReport{}, err
		}
	}
	if n > reportLimit(limits, "response-bytes") && out.Operation == "config validate" {
		if err := reduceConfigManifest(ctx, &out, format, reportLimit(limits, "response-bytes")); err != nil {
			return encodedReport{}, err
		}
		n, err = reportSize(ctx, out, format)
		if err != nil {
			return encodedReport{}, err
		}
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
		err = renderText(ctx, &b, out)
		data = b.Bytes()
	}
	if err != nil {
		return encodedReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return encodedReport{}, err
	}
	return encodedReport{data: data, out: out}, nil
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
	n, err := reportSize(ctx, *out, format)
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
			e.Code = "config-error-summary"
			e.Message = fmt.Sprintf("%d encountered %s errors; located details omitted to satisfy response-bytes.", count, category)
			out.Errors = append(out.Errors, e)
		}
	}
	omit("/errors", original)
	engine.FinalizeLintResult(out)
	return nil
}

// emergencyConfigReport has work and output bounded by a fixed record count.
// Source identities and prose are replaced by exact omissions; actual scan and
// validity counts and every encountered error category remain authoritative.
func emergencyConfigReport(original result.Result, format string, limits map[string]int64) (encodedReport, error) {
	if format == "vscode" {
		return emergencyVSCodeReport(original, limits)
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
		if validationLimitApplicable(limit.Name) {
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
		err = renderText(context.Background(), &b, out)
		data = b.Bytes()
	}
	if err != nil {
		return encodedReport{}, err
	}
	if int64(len(data)) > reportLimit(limits, "response-bytes") {
		return encodedReport{}, errReportBudget
	}
	return encodedReport{data: data, out: out}, nil
}
