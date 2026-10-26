package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"time"

	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
)

// reportInspection reserves the single immutable identity/outcome before the
// independent reporting deadline. Its emergency image has a fixed number of
// sections and uses O(1) collection lengths, never an AST walk or source scan.
func reportInspection(original result.Result, p *inspection.Projection, format string, w io.Writer, limits map[string]int64) int {
	reserve := original
	reserve.Files = slices.Clone(original.Files)
	reserve.EffectiveLimits = slices.Clone(original.EffectiveLimits)
	reserve.Omissions = slices.Clone(original.Omissions)
	reserve.Interruptions = slices.Clone(original.Interruptions)
	var reserveProjection *inspection.Projection
	if p != nil {
		copy := *p
		reserveProjection = &copy
	}
	omitInspectionDetails(&reserve, reserveProjection, true)
	reserve.Interruptions = append(reserve.Interruptions, result.Interruption{ID: "reporting-interruption", Reason: "deadline-exceeded", Phase: "reporting", Message: "reporting-time exhausted while constructing result details"})
	inspectionOutcome(&reserve)
	setReportLimits(&reserve, limits)
	emergency, emergencyErr := encodeInspection(context.Background(), reserve, reserveProjection, format, limits)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(reportLimit(limits, "reporting-time")))
	defer cancel()
	encoded, err := buildInspectionReport(ctx, original, p, format, limits)
	if err != nil && ctx.Err() != nil && emergencyErr == nil {
		encoded, err = emergency, nil
	} else if errors.Is(err, errReportBudget) && emergencyErr == nil {
		reserve.Interruptions[len(reserve.Interruptions)-1] = result.Interruption{ID: "reporting-interruption", Reason: "limit-exceeded", Phase: "reporting", Message: "essential detail records exceed response-bytes; whole records omitted"}
		encoded, err = encodeInspection(context.Background(), reserve, reserveProjection, format, limits)
	}
	status := inspectionExit(original)
	if err != nil {
		if status == 14 {
			return status
		}
		return exitIO
	}
	status = inspectionExit(encoded.out)
	if n, err := w.Write(encoded.data); err != nil || n != len(encoded.data) {
		if status == 14 {
			return status
		}
		return exitIO
	}
	return status
}

func buildInspectionReport(ctx context.Context, original result.Result, p *inspection.Projection, format string, limits map[string]int64) (encodedReport, error) {
	out := cloneReport(original)
	var projected *inspection.Projection
	if p != nil {
		copy := *p
		projected = &copy
	}
	setReportLimits(&out, limits)
	encode := func() (encodedReport, error) { return encodeInspection(ctx, out, projected, format, limits) }
	full, err := encode()
	if err == nil {
		return full, nil
	}
	if ctx.Err() != nil {
		return encodedReport{}, ctx.Err()
	}
	if !errors.Is(err, errReportBudget) {
		return encodedReport{}, err
	}
	out.Interruptions = append(out.Interruptions, result.Interruption{ID: "reporting-interruption", Reason: "limit-exceeded", Phase: "reporting", Message: "result details exceed response-bytes; exact full size was not computed after admission failed"})
	// The optional AST is always the first reduction. No projected owner/cause
	// depends on an AST node; its full identity stays in inspection.file.
	if projected != nil && projected.AST.Data != nil {
		count := 1
		out.Omissions = append(out.Omissions, result.Omission{Section: "/inspection/ast", Reason: "output-limit", Message: "whole optional AST omitted to satisfy response-bytes", Count: &count})
		projected.AST = omittedSection[inspection.AST]()
	}
	inspectionOutcome(&out)
	reduced, err := encode()
	if err == nil {
		return reduced, nil
	}
	if ctx.Err() != nil {
		return encodedReport{}, ctx.Err()
	}
	if !errors.Is(err, errReportBudget) {
		return encodedReport{}, err
	}
	// Rules, directives, raw candidates and suppressed findings form a linked
	// group. Drop complete dependent sections together instead of dangling IDs.
	omitInspectionDetails(&out, projected, false)
	inspectionOutcome(&out)
	return encode()
}
func omittedSection[T any]() inspection.Section[T] {
	return inspection.Section[T]{Availability: "unavailable", Reasons: []string{"output-limit"}}
}
func omitInspectionDetails(out *result.Result, p *inspection.Projection, emergency bool) {
	omit := func(section string, n int) {
		if n > 0 {
			count := n
			out.Omissions = append(out.Omissions, result.Omission{Section: section, Reason: "output-limit", Message: "whole detail records omitted; acquired identity and outcome are retained", Count: &count})
		}
	}
	if p != nil {
		if p.AST.Data != nil {
			omit("/inspection/ast", 1)
			p.AST = omittedSection[inspection.AST]()
		}
		if p.DocumentContext.Data != nil {
			omit("/inspection/document_context", 1)
			p.DocumentContext = omittedSection[json.RawMessage]()
		}
		if p.Configuration.Data != nil {
			omit("/inspection/configuration", 1)
			p.Configuration = omittedSection[result.ConfigurationData]()
		}
		if p.Rules.Data != nil {
			omit("/inspection/rules", len(*p.Rules.Data))
			p.Rules = omittedSection[[]inspection.Rule]()
		}
		if p.RuleCounts.Data != nil {
			omit("/inspection/rule_counts", 1)
			p.RuleCounts = omittedSection[json.RawMessage]()
		}
		if p.Directives.Data != nil {
			omit("/inspection/directives", len(*p.Directives.Data))
			p.Directives = omittedSection[[]inspection.Directive]()
		}
		if p.RawCandidates.Data != nil {
			omit("/inspection/raw_candidates", len(*p.RawCandidates.Data))
			p.RawCandidates = omittedSection[[]inspection.RawCandidate]()
		}
		if p.ParsingNotes.Data != nil {
			omit("/inspection/parsing_notes", len(*p.ParsingNotes.Data))
			p.ParsingNotes = omittedSection[[]result.Note]()
		}
	}
	omit("/diagnostics", len(out.Diagnostics))
	out.Diagnostics = []result.Diagnostic{}
	omit("/suppressed_diagnostics", len(out.SuppressedDiagnostics))
	out.SuppressedDiagnostics = []json.RawMessage{}
	omit("/notes", len(out.Notes))
	out.Notes = []json.RawMessage{}
	omit("/coverage", len(out.Coverage))
	out.Coverage = []result.Coverage{}
	for i := range out.Files {
		out.Files[i].CoverageIDs = []string{}
	}
	if emergency {
		// Keep observation scope and operational precedence. At most four category
		// summaries per scope; counts are collected before starting the timer.
		var counts [2][4]int
		for _, e := range out.Errors {
			scope := 0
			if e.Scope == "operation" {
				scope = 1
			}
			for i, c := range []result.ErrorCategory{result.Usage, result.Configuration, result.IO, result.Internal} {
				if c == e.Category {
					counts[scope][i]++
				}
			}
		}
		omit("/errors", len(out.Errors))
		out.Errors = []result.Error{}
		for scope, items := range counts {
			for i, n := range items {
				if n > 0 {
					kind := "observation"
					if scope == 1 {
						kind = "operation"
					}
					out.Errors = append(out.Errors, result.Error{ID: fmt.Sprintf("report-error-%d-%d", scope, i), Category: []result.ErrorCategory{result.Usage, result.Configuration, result.IO, result.Internal}[i], Scope: kind, Code: "inspection-error-summary", Message: fmt.Sprintf("%d encountered errors; details omitted by reporting deadline", n)})
				}
			}
		}
		for i := range out.Files {
			out.Files[i].ErrorIDs = []string{}
		}
		// Removing error detail identities requires removing section cause links.
		if p != nil {
			clearInspectionCauses(p)
		}
	} else {
		// Coverage IDs can occur in an unavailable section. Error/interruption causes
		// remain valid; remove only links to the now omitted coverage family.
		if p != nil {
			retainInspectionCauses(p, *out)
		}
	}
}
func clearInspectionCauses(p *inspection.Projection) {
	p.Configuration.CauseIDs = nil
	p.Flavor.CauseIDs = nil
	p.DocumentContext.CauseIDs = nil
	p.AST.CauseIDs = nil
	p.Rules.CauseIDs = nil
	p.RuleCounts.CauseIDs = nil
	p.Directives.CauseIDs = nil
	p.RawCandidates.CauseIDs = nil
	p.Resources.CauseIDs = nil
	p.ParsingNotes.CauseIDs = nil
}
func retainInspectionCauses(p *inspection.Projection, out result.Result) {
	ids := map[string]bool{}
	for _, e := range out.Errors {
		ids[e.ID] = true
	}
	for _, s := range out.Interruptions {
		ids[s.ID] = true
	}
	keep := func(values []string) []string {
		v := []string{}
		for _, id := range values {
			if ids[id] {
				v = append(v, id)
			}
		}
		return v
	}
	p.Configuration.CauseIDs = keep(p.Configuration.CauseIDs)
	p.Flavor.CauseIDs = keep(p.Flavor.CauseIDs)
	p.DocumentContext.CauseIDs = keep(p.DocumentContext.CauseIDs)
	p.AST.CauseIDs = keep(p.AST.CauseIDs)
	p.Rules.CauseIDs = keep(p.Rules.CauseIDs)
	p.RuleCounts.CauseIDs = keep(p.RuleCounts.CauseIDs)
	p.Directives.CauseIDs = keep(p.Directives.CauseIDs)
	p.RawCandidates.CauseIDs = keep(p.RawCandidates.CauseIDs)
	p.Resources.CauseIDs = keep(p.Resources.CauseIDs)
	p.ParsingNotes.CauseIDs = keep(p.ParsingNotes.CauseIDs)
}

// inspectionJSON overrides the embedded RawMessage with the already-owned typed
// projection. Encoding is performed once, only after bounded size admission.
type inspectionJSON struct {
	Inspection *inspection.Projection `json:"inspection,omitempty"`
	result.Result
}

func inspectionJSONSize(ctx context.Context, out result.Result, p *inspection.Projection, bound int64) (int64, int64, error) {
	out.Inspection = nil
	remaining := bound - 1 // final newline
	sizer := jsonSizer{ctx: ctx, remaining: &remaining}
	n, err := sizer.value(reflect.ValueOf(out))
	if err != nil {
		return 0, 0, err
	}
	var projectionBytes int64
	if p != nil {
		const fieldBytes = 14 // comma, quoted inspection key and colon
		if err = sizer.charge(fieldBytes); err != nil {
			return 0, 0, err
		}
		projectionBytes, err = sizer.value(reflect.ValueOf(p))
		if err != nil {
			return 0, 0, err
		}
		n += fieldBytes + projectionBytes
	}
	return n + 1, projectionBytes, nil
}
func renderTypedInspectionReport(ctx context.Context, w io.Writer, out result.Result, p *inspection.Projection) error {
	if p != nil {
		if err := renderInspectionText(ctx, w, p); err != nil {
			return err
		}
	}
	out.Inspection = nil
	return renderText(ctx, w, out)
}
func encodeInspection(ctx context.Context, out result.Result, p *inspection.Projection, format string, limits map[string]int64) (encodedReport, error) {
	if err := ctx.Err(); err != nil {
		return encodedReport{}, err
	}
	bound := reportLimit(limits, "response-bytes")
	out.Inspection = nil
	var data []byte
	if format == "json" {
		size, projectionBytes, err := inspectionJSONSize(ctx, out, p, bound)
		if err != nil {
			return encodedReport{out: out}, err
		}
		if size > bound {
			return encodedReport{out: out}, errReportBudget
		}
		if err := ctx.Err(); err != nil {
			return encodedReport{}, err
		}
		data, err = json.Marshal(inspectionJSON{p, out})
		if err != nil {
			return encodedReport{}, err
		}
		data = append(data, '\n')
		if int64(len(data)) != size {
			return encodedReport{}, fmt.Errorf("inspection encoded size mismatch: got %d, expected %d", len(data), size)
		}
		// The explicit projection is the first wire field. Retain its admitted byte
		// view without a second serialization or allocation.
		if p != nil {
			const prefix = `{"inspection":`
			out.Inspection = data[len(prefix) : int64(len(prefix))+projectionBytes]
		}
	} else {
		counter := &reportCounter{limit: bound}
		if err := renderTypedInspectionReport(ctx, counter, out, p); err != nil {
			return encodedReport{out: out}, err
		}
		if counter.n > bound {
			return encodedReport{out: out}, errReportBudget
		}
		if err := ctx.Err(); err != nil {
			return encodedReport{}, err
		}
		var b bytes.Buffer
		if err := renderTypedInspectionReport(ctx, &b, out, p); err != nil {
			return encodedReport{}, err
		}
		data = b.Bytes()
	}
	if err := ctx.Err(); err != nil {
		return encodedReport{}, err
	}
	if int64(len(data)) > bound {
		return encodedReport{out: out}, errReportBudget
	}
	return encodedReport{data: data, out: out}, nil
}

// The generic renderer accepts wire-only callers. The inspection reporter uses
// its typed projection directly and never enters this decoding compatibility path.
func renderInspectionRawText(ctx context.Context, w io.Writer, raw json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var p inspection.Projection
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	return renderInspectionText(ctx, w, &p)
}

func renderInspectionText(ctx context.Context, w io.Writer, p *inspection.Projection) error {
	line := func(format string, args ...any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := fmt.Fprintf(w, format, args...)
		return err
	}
	f := p.File
	if err := line("File identity\n  Resolved path: %s\n  Binary version: %s\n  Size: %s\n  SHA-1: %s\n  SHA-256: %s\n  Normally excluded: %t (known: %t)\n  Physical lines: %d\n", textField(f.ResolvedPath), textField(f.BinaryVersion), textField(f.SizeDisplay), f.SHA1, f.SHA256, f.NormallyExcluded, f.NormallyExcludedKnown, f.PhysicalLineCount); err != nil {
		return err
	}
	selection := "whole file"
	if f.SelectedLines != nil {
		selection = fmt.Sprintf("%d-%d", f.SelectedLines.Start, f.SelectedLines.End)
	}
	if err := line("Selected lines: %s\n", selection); err != nil {
		return err
	}
	section := func(label string, v any) error {
		if err := line("%s\n  ", label); err != nil {
			return err
		}
		if err := writeInspectionJSON(ctx, w, v); err != nil {
			return err
		}
		return line("\n")
	}
	for _, item := range []struct {
		label string
		data  any
	}{{"Configuration and provenance", p.Configuration}, {"Markdown flavor", p.Flavor}, {"Document context", p.DocumentContext}, {"Rule counts", p.RuleCounts}, {"Directives", p.Directives}, {"Raw directive candidates", p.RawCandidates}, {"Resources", p.Resources}, {"Parsing notes", p.ParsingNotes}} {
		if err := section(item.label, item.data); err != nil {
			return err
		}
	}
	if err := line("Abstract syntax tree: %s (reasons: %s)\n", textField(p.AST.Availability), textField(fmt.Sprint(p.AST.Reasons))); err != nil {
		return err
	}
	if p.AST.Data != nil {
		var walk func(inspection.Node, int) error
		walk = func(n inspection.Node, depth int) error {
			if err := line("  %*s%s %s [%d:%d-%d:%d] bytes %d-%d context=%t extends_region=%t properties=", depth*2, "", textField(n.ID), textField(n.Kind), n.Range.Start.Line, n.Range.Start.Column, n.Range.End.Line, n.Range.End.Column, n.ByteRange.Start, n.ByteRange.End, n.ContextOnly, n.ExtendsRegion); err != nil {
				return err
			}
			if err := writeInspectionJSON(ctx, w, n.Properties); err != nil {
				return err
			}
			if err := line("\n"); err != nil {
				return err
			}
			for _, child := range n.Children {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		if err := walk(p.AST.Data.Root, 0); err != nil {
			return err
		}
		if p.AST.Data.Frontmatter != nil {
			if err := section("  Opaque frontmatter", p.AST.Data.Frontmatter); err != nil {
				return err
			}
		}
		if len(p.AST.Data.References) > 0 {
			if err := section("  Reference definitions", p.AST.Data.References); err != nil {
				return err
			}
		}
	}
	if err := line("Rules run: %s (reasons: %s)\n", textField(p.Rules.Availability), textField(fmt.Sprint(p.Rules.Reasons))); err != nil {
		return err
	}
	if p.Rules.Data != nil {
		for _, r := range *p.Rules.Data {
			if err := section("  "+r.Rule, r); err != nil {
				return err
			}
		}
	}
	return line("Findings and suppressed findings\n")
}

func writeInspectionJSON(ctx context.Context, w io.Writer, v any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if counter, ok := w.(*reportCounter); ok {
		remaining := counter.limit - counter.n
		sizer := jsonSizer{ctx: ctx, terminalSafe: true}
		if counter.limit > 0 {
			sizer.remaining = &remaining
		}
		n, err := sizer.value(reflect.ValueOf(v))
		if err != nil {
			return err
		}
		counter.n += n
		return ctx.Err()
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	_, err = io.WriteString(w, textField(string(raw)))
	return err
}
