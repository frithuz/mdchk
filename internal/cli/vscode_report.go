package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"

	"github.com/frithuz/mdchk/internal/result"
)

// renderVSCode borrows the canonical result synchronously without changing its
// snapshots or coordinates. Only the producer decides which saved/candidate
// findings are authoritative. Dynamic messages escape colons so the documented
// greedy path capture cannot consume a second matcher header inside a message.
func renderVSCode(ctx context.Context, stdout, stderr io.Writer, r result.Result) error {
	field := func(w io.Writer, s string, message bool) error { return vscodeField(ctx, w, s, message) }
	line := func(path string, at result.Position, code, message string) error {
		if err := field(stdout, path, false); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(stdout, ":%d:%d: error: ", at.Line, at.Column); err != nil {
			return err
		}
		if err := field(stdout, code, true); err != nil {
			return err
		}
		if _, err := io.WriteString(stdout, ": "); err != nil {
			return err
		}
		if err := field(stdout, message, true); err != nil {
			return err
		}
		_, err := io.WriteString(stdout, "\n")
		return err
	}
	operational := func(parts ...string) error {
		for i, p := range parts {
			if i > 0 {
				if _, err := io.WriteString(stderr, " "); err != nil {
					return err
				}
			}
			if err := field(stderr, p, true); err != nil {
				return err
			}
		}
		_, err := io.WriteString(stderr, "\n")
		return err
	}
	for _, d := range r.Diagnostics {
		if err := line(d.Path, d.Range.Start, d.Rule, d.Message); err != nil {
			return err
		}
	}
	for _, e := range r.Errors {
		if e.Category == result.Configuration && e.Path != "" && e.Range != nil {
			if err := line(e.Path, e.Range.Start, e.Code, e.Message); err != nil {
				return err
			}
		} else if err := operational("error", e.Path, e.Code, string(e.Category), e.Message); err != nil {
			return err
		}
	}
	for _, c := range r.Coverage {
		if c.Path != "" && c.Range != nil {
			if err := line(c.Path, c.Range.Start, "coverage", c.Message); err != nil {
				return err
			}
		} else if err := operational("coverage", c.Path, c.Rule, c.Reason, c.Message); err != nil {
			return err
		}
	}
	for _, s := range r.Interruptions {
		fields := []string{"interrupted", s.Phase, s.Path, s.Reason, s.Message}
		if s.Limit != nil {
			fields = append(fields, "limit", s.Limit.Name, "observed", strconv.FormatInt(s.Limit.Observed, 10), "allowed", strconv.FormatInt(s.Limit.Allowed, 10), s.Limit.Unit)
		}
		if err := operational(fields...); err != nil {
			return err
		}
	}
	for _, o := range r.Omissions {
		count := "unknown"
		if o.Count != nil {
			count = strconv.Itoa(*o.Count)
		}
		if err := operational("omitted", count, o.Section, o.Path, o.Reason, o.Message); err != nil {
			return err
		}
	}
	partialFix := r.Operation == "fix" && (!r.Complete || len(r.Errors) > 0 || len(r.Interruptions) > 0)
	for _, f := range r.Files {
		if partialFix || f.WriteStatus == "failed" || f.WriteStatus == "unknown" || f.WriteStatus == "refused" {
			if err := operational("outcome", f.Path, f.WriteStatus); err != nil {
				return err
			}
		}
		for _, name := range f.Leftovers {
			if err := operational("owned staging leftover", f.Path, name); err != nil {
				return err
			}
		}
	}
	if !r.Complete {
		if err := operational("incomplete"); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Stream escaping uses bounded chunks and checkpoints even within one large
// scalar. Ordinary path colons/backslashes remain unchanged for Windows paths.
func vscodeField(ctx context.Context, w io.Writer, s string, message bool) error {
	var chunk [1024]byte
	n := 0
	flush := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if n > 0 {
			if _, err := w.Write(chunk[:n]); err != nil {
				return err
			}
			n = 0
		}
		return nil
	}
	for _, r := range s {
		var encoded string
		switch {
		case r == ':' && message:
			encoded = `\x3a`
		case vscodeBidiControl(r):
			encoded = fmt.Sprintf(`\u%04x`, r)
		case terminalControl(r):
			encoded = textField(string(r))
		default:
			encoded = string(r)
		}
		if n+len(encoded) > len(chunk) {
			if err := flush(); err != nil {
				return err
			}
		}
		n += copy(chunk[n:], encoded)
	}
	return flush()
}

func encodeVSCode(ctx context.Context, out result.Result, bound int64) (encodedReport, error) {
	if bound < 0 {
		return encodedReport{}, errReportBudget
	}
	counter := &reportCounter{limit: max(bound, 1)}
	if err := renderVSCode(ctx, counter, counter, out); err != nil {
		return encodedReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return encodedReport{}, err
	}
	if counter.n > bound {
		return encodedReport{}, errReportBudget
	}
	var stdout, stderr bytes.Buffer
	// The counting pass admits both streams before allocating either payload.
	if err := renderVSCode(ctx, &stdout, &stderr, out); err != nil {
		return encodedReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return encodedReport{}, err
	}
	return encodedReport{data: stdout.Bytes(), stderr: stderr.Bytes(), out: out}, nil
}

func vscodeOmit(out *result.Result, section string, count int) {
	if count > 0 {
		out.Omissions = append(out.Omissions, result.Omission{Section: section, Reason: "output-limit", Message: "whole records omitted from bounded reporter", Count: &count})
	}
}

// The reserve is admitted before the independent reporting timer. Keep the
// canonical write manifest by immutable borrow; never clone or render diffs.
// Category summaries avoid walking arbitrary error prose on this path.
func emergencyVSCodeReport(original result.Result, limits map[string]int64) (encodedReport, error) {
	return reservedVSCodeReport(context.Background(), original, limits)
}

func reservedVSCodeReport(ctx context.Context, original result.Result, limits map[string]int64) (encodedReport, error) {
	if err := ctx.Err(); err != nil {
		return encodedReport{}, err
	}
	out := original
	out.Omissions = nil
	out.Interruptions = nil
	vscodeOmit(&out, "/omissions", len(original.Omissions))
	if original.Operation == "config validate" && len(original.Interruptions) > 0 {
		// Configuration scanning has at most one canonical operation interruption.
		// Retain bounded semantic attribution separately from the reporting deadline;
		// source prose/path are optional detail, not authority for the stop reason.
		stop := original.Interruptions[0]
		stop.Path = ""
		stop.Message = "Configuration scanning was interrupted; detailed reporting was interrupted as well."
		if stop.Limit != nil {
			limit := *stop.Limit
			stop.Limit = &limit
		}
		out.Interruptions = append(out.Interruptions, stop)
		one := 1
		out.Omissions = append(out.Omissions, result.Omission{Section: "/interruptions/0", Reason: "output-limit", Message: "source-derived interruption detail replaced by bounded semantic summary", Count: &one})
		vscodeOmit(&out, "/interruptions", len(original.Interruptions)-1)
	} else {
		vscodeOmit(&out, "/interruptions", len(original.Interruptions))
	}
	out.EffectiveLimits = slices.Clone(original.EffectiveLimits)
	setReportLimits(&out, limits)
	out.Diagnostics = nil
	out.Coverage = nil
	out.Errors = vscodeErrorSummaries(original)
	vscodeOmit(&out, "/diagnostics", len(original.Diagnostics))
	vscodeOmit(&out, "/coverage", len(original.Coverage))
	vscodeOmit(&out, "/errors", len(original.Errors))
	out.Interruptions = append(out.Interruptions, result.Interruption{Phase: "reporting", Reason: "deadline-exceeded", Message: "reporting-time exhausted while constructing result details"})
	// Preserve canonical status/categories without re-finalizing borrowed data.
	markVSCodeIncomplete(&out)
	return encodeVSCode(ctx, out, reportLimit(limits, "response-bytes"))
}

func buildVSCodeReport(ctx context.Context, original result.Result, limits map[string]int64) (encodedReport, error) {
	if err := ctx.Err(); err != nil {
		return encodedReport{}, err
	}
	out := original
	out.Omissions = slices.Clone(original.Omissions)
	out.Interruptions = slices.Clone(original.Interruptions)
	out.EffectiveLimits = slices.Clone(original.EffectiveLimits)
	setReportLimits(&out, limits)
	bound := reportLimit(limits, "response-bytes")
	full, err := encodeVSCode(ctx, out, bound)
	if err == nil || ctx.Err() != nil {
		return full, err
	}
	// All non-finding error and write evidence is essential. Omit whole ordered
	// detail records, retaining the longest prefix that fits alongside notices.
	if !errors.Is(err, errReportBudget) {
		return encodedReport{}, err
	}
	out.Interruptions = append(out.Interruptions, result.Interruption{Phase: "reporting", Reason: "limit-exceeded", Message: "result details exceed response-bytes"})
	for _, section := range []string{"/diagnostics", "/coverage"} {
		length := len(out.Diagnostics)
		if section == "/coverage" {
			length = len(out.Coverage)
		}
		if length == 0 {
			continue
		}
		diagnostics, coverage := out.Diagnostics, out.Coverage
		trim := func(n int) {
			if section == "/diagnostics" {
				out.Diagnostics = diagnostics[:n]
			} else {
				out.Coverage = coverage[:n]
			}
		}
		trim(0)
		vscodeOmit(&out, section, length)
		index := len(out.Omissions) - 1
		markVSCodeIncomplete(&out)
		if _, e := encodeVSCode(ctx, out, bound); e == nil {
			lo, hi := 0, length
			for lo < hi {
				mid := lo + (hi-lo+1)/2
				trim(mid)
				count := length - mid
				out.Omissions[index].Count = &count
				if _, e = encodeVSCode(ctx, out, bound); e == nil {
					lo = mid
				} else if ctx.Err() != nil {
					return encodedReport{}, e
				} else {
					hi = mid - 1
				}
			}
			trim(lo)
			count := length - lo
			out.Omissions[index].Count = &count
			markVSCodeIncomplete(&out)
			return encodeVSCode(ctx, out, bound)
		} else if ctx.Err() != nil {
			return encodedReport{}, e
		}
	}
	// Replace only detailed error prose with explicit category summaries. Essential
	// target outcomes/leftover names cannot be silently removed to make room.
	out.Errors = vscodeErrorSummaries(original)
	vscodeOmit(&out, "/errors", len(original.Errors))
	markVSCodeIncomplete(&out)
	return encodeVSCode(ctx, out, bound)
}

// Existing operational errors keep their operation-specific precedence. Reporting
// loss cannot convert a configuration failure or an internal failure to clean.
func markVSCodeIncomplete(out *result.Result) {
	status := lintExit(*out)
	out.Complete = false
	if status == 0 || status == 10 || status == exitIncomplete {
		out.Status = "incomplete"
	}
}

func vscodeErrorSummaries(out result.Result) []result.Error {
	summaries := out.ReportingErrorSummaries()
	for i := range summaries {
		summaries[i].Code = "reporting-error-summary"
		summaries[i].Message = "Encountered " + string(summaries[i].Category) + " errors; detailed records omitted by bounded reporting."
	}
	return summaries
}

// This presentation-local extension covers the complete Unicode Bidi_Control
// property. Legacy text/JSON escaping is deliberately unchanged.
func vscodeBidiControl(r rune) bool {
	return r == 0x061c || r == 0x200e || r == 0x200f || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069
}
