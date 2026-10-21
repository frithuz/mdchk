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

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

var errReportBudget = errors.New("essential result manifest exceeds response-bytes")

// reportResult owns presentation only. Its cleanup deadline starts after analysis;
// an expired analysis context cannot consume this independently reserved budget.
func reportResult(out result.Result, format string, stdout io.Writer, limits map[string]int64) int {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(reportLimit(limits, "reporting-time")))
	defer cancel()
	encoded, err := buildReport(ctx, out, format, limits)
	status := lintExit(out)
	if err != nil {
		if status == 14 {
			return status
		}
		return exitIO
	}
	status = lintExit(encoded.out)
	n, err := stdout.Write(encoded.data)
	if err != nil || n != len(encoded.data) {
		if status == 14 {
			return status
		}
		return exitIO
	}
	return status
}

type encodedReport struct {
	data []byte
	out  result.Result
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
	manifest := result.NewLint(out.Mode)
	owned := cloneReport(out)
	manifest.Files, manifest.CheckedFiles = owned.Files, owned.CheckedFiles
	setReportLimits(&manifest, limits)
	engine.FinalizeLintResult(&manifest)
	empty := result.NewLint(out.Mode)
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
		notes, suppressed, diagnostics, coverage, skipped := out.Notes, out.SuppressedDiagnostics, out.Diagnostics, out.Coverage, out.SkippedFiles
		families := []struct {
			section string
			length  int
			trim    func(int)
		}{
			{"/notes", len(out.Notes), func(n int) { out.Notes = notes[:n] }},
			{"/suppressed_diagnostics", len(out.SuppressedDiagnostics), func(n int) { out.SuppressedDiagnostics = suppressed[:n] }},
			{"/diagnostics", len(out.Diagnostics), func(n int) { out.Diagnostics = diagnostics[:n] }},
			{"/coverage", len(out.Coverage), func(n int) { out.Coverage = coverage[:n] }},
			{"/skipped_files", len(out.SkippedFiles), func(n int) { out.SkippedFiles = skipped[:n] }},
		}
		for _, f := range families {
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
	r.CheckedFiles = slices.Clone(r.CheckedFiles)
	r.SkippedFiles = slices.Clone(r.SkippedFiles)
	r.Diagnostics = slices.Clone(r.Diagnostics)
	r.Coverage = slices.Clone(r.Coverage)
	for i := range r.Coverage {
		r.Coverage[i].CauseIDs = slices.Clone(r.Coverage[i].CauseIDs)
	}
	r.Errors = slices.Clone(r.Errors)
	r.Interruptions = slices.Clone(r.Interruptions)
	r.Omissions = slices.Clone(r.Omissions)
	r.EffectiveLimits = slices.Clone(r.EffectiveLimits)
	r.Files = slices.Clone(r.Files)
	for i := range r.Files {
		r.Files[i].ErrorIDs = slices.Clone(r.Files[i].ErrorIDs)
		r.Files[i].CoverageIDs = slices.Clone(r.Files[i].CoverageIDs)
		r.Files[i].ChangeIDs = slices.Clone(r.Files[i].ChangeIDs)
	}
	return r
}

func textField(s string) string { return strings.NewReplacer("\r", "\\r", "\n", "\\n").Replace(s) }
func renderText(ctx context.Context, w io.Writer, r result.Result) error {
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
		if f.Analysis != "complete" {
			if err := line("file %s: analysis %s\n", textField(f.Path), textField(f.Analysis)); err != nil {
				return err
			}
		}
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
