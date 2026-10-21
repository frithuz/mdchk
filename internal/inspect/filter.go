package inspect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// LineSelection identifies inclusive physical content lines. A terminal newline
// supplies an EOF coordinate, never an additional selectable line.
type LineSelection struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// ParseLines accepts exactly positive decimal N or N-M within the physical
// index. Empty means whole document, including the empty document's EOF anchor.
func ParseLines(index parser.LineIndex, text string) (*LineSelection, error) {
	if text == "" {
		return nil, nil
	}
	selection, err := ParseLineSelection(text)
	if err != nil {
		return nil, err
	}
	if selection.End > index.LineCount() {
		return nil, fmt.Errorf("invalid inspection lines %q: %w", text, parser.ErrPosition)
	}
	return selection, nil
}

// ParseLineSelection checks the strict line-range grammar without source access.
// ParseLines additionally validates its bounds against an immutable line index.
func ParseLineSelection(text string) (*LineSelection, error) {
	if text == "" {
		return nil, nil
	}
	// Two machine-sized decimal integers and one separator fit in 41 bytes.
	// Reject longer input before splitting or quoting an unbounded argument.
	if len(text) > 41 {
		return nil, fmt.Errorf("invalid inspection lines: %w", parser.ErrPosition)
	}
	parts := strings.Split(text, "-")
	if len(parts) > 2 {
		return nil, fmt.Errorf("invalid inspection lines %q", text)
	}
	number := func(s string) (int, error) {
		if s == "" || s[0] == '0' {
			return 0, parser.ErrPosition
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				return 0, parser.ErrPosition
			}
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return 0, parser.ErrPosition
		}
		return n, nil
	}
	first, err := number(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid inspection lines %q: %w", text, parser.ErrPosition)
	}
	last := first
	if len(parts) == 2 {
		last, err = number(parts[1])
	}
	if err != nil || last < first {
		return nil, fmt.Errorf("invalid inspection lines %q: %w", text, parser.ErrPosition)
	}
	return &LineSelection{first, last}, nil
}

// Region describes selected byte positions without clipping source facts.
type Region struct {
	ByteRange   result.ByteRange `json:"byte_range"`
	IncludesEOF bool             `json:"includes_eof"`
}

type selectedRegion struct {
	result.ByteRange
	eof bool
}

func (r selectedRegion) intersects(v result.ByteRange) bool {
	if v.Start == v.End {
		return r.Start <= v.Start && (v.Start < r.End || r.eof && v.Start == r.End)
	}
	return v.Start < r.End && r.Start < v.End
}
func (r selectedRegion) extends(v result.ByteRange) bool { return v.Start < r.Start || v.End > r.End }

// Filter constructs an independently owned presentation of a full Analyze
// result. It never parses or executes a callback. All intervals, full-source
// execution coverage, directive owners, source references and causal records
// remain original facts; selected enablement is separate from execution.
//
// Filtering uses the snapshot's residual meter. On interruption it returns the
// original full analysis unchanged with the typed cause: no selected view or
// fabricated zero counts is published. Callers must handle that error before
// presenting the original as a successfully selected result. Concurrent filters
// may read the same unmodified analysis and share the operation meter.
func Filter(ctx context.Context, s *Snapshot, full Analysis, lines string) (Analysis, error) {
	if ctx == nil || s == nil || s.budget == nil {
		return full, errors.New("nil inspection context or snapshot")
	}
	if full.cause != nil {
		return full, full.cause
	}
	b := builder{ctx: ctx, budget: s.budget, source: s.source, index: s.lines}
	if err := b.charge(int64(len(lines)) + 1); err != nil {
		return full, err
	}
	selection, err := ParseLines(s.lines, lines)
	if err != nil {
		return full, err
	}
	region := selectedRegion{ByteRange: result.ByteRange{Start: 0, End: s.source.Len()}, eof: true}
	if selection != nil {
		first, _ := s.lines.Line(selection.Start)
		last, _ := s.lines.Line(selection.End)
		region.Start = first.Content.Start()
		region.End = last.Ending.End()
		region.eof = selection.End == s.lines.LineCount()
	}
	if full.snapshot != s || len(full.wire) == 0 {
		return full, errors.New("inspection analysis does not belong to snapshot")
	}
	// Analyze retains its bounded original wire image privately. Public returned
	// data can be edited without changing this authority or copy-size accounting.
	work := int64(len(full.wire))*8 + 65536
	if err := b.charge(work); err != nil {
		return full, err
	}
	// Reserve decoded, typed-restored and filtered graph records before
	// construction, including fixed selection records.
	if err := s.budget.ChargeNodes(3*full.filterNodes + 16); err != nil {
		return full, err
	}
	raw := full.wire
	var out Analysis
	if err = json.Unmarshal(raw, &out); err != nil {
		return full, err
	}
	if err := b.restoreProjectionValues(&out.Projection, s); err != nil {
		return full, err
	}
	for _, record := range full.records {
		out.Result.RetainAnalysis(record)
	}
	p := &out.Projection
	p.File.SelectedLines = selection
	p.File.SelectedRegion = &Region{region.ByteRange, region.eof}
	if selection != nil && p.AST.Data != nil {
		var visit func(Node, bool) (Node, bool, error)
		visit = func(n Node, root bool) (Node, bool, error) {
			if err := b.charge(1); err != nil {
				return Node{}, false, err
			}
			children := make([]Node, 0, len(n.Children))
			for _, child := range n.Children {
				v, keep, e := visit(child, false)
				if e != nil {
					return Node{}, false, e
				}
				if keep {
					children = append(children, v)
				}
			}
			hit := region.intersects(n.ByteRange)
			if !root && !hit && len(children) == 0 {
				return Node{}, false, nil
			}
			n.ContextOnly = !hit || root || len(children) > 0 && region.extends(n.ByteRange)
			n.ExtendsRegion = region.extends(n.ByteRange)
			n.Children = children
			return n, true, nil
		}
		p.AST.Data.Root, _, err = visit(p.AST.Data.Root, true)
		if err != nil {
			return full, err
		}
		for i := range p.AST.Data.References {
			ref := &p.AST.Data.References[i]
			ref.ContextOnly = !region.intersects(ref.ByteRange)
		}
	}
	diagnostics := make([]result.Diagnostic, 0, len(out.Result.Diagnostics))
	for _, d := range out.Result.Diagnostics {
		if err := b.charge(1); err != nil {
			return full, err
		}
		if region.intersects(d.ByteRange) {
			d.ExtendsRegion = selection != nil && region.extends(d.ByteRange)
			diagnostics = append(diagnostics, d)
		}
	}
	out.Result.Diagnostics = diagnostics
	suppressed := make([]json.RawMessage, 0, len(out.Result.SuppressedDiagnostics))
	for _, raw := range out.Result.SuppressedDiagnostics {
		if err := b.charge(int64(len(raw)) + 1); err != nil {
			return full, err
		}
		var d result.SuppressedDiagnostic
		if err := json.Unmarshal(raw, &d); err != nil {
			return full, err
		}
		if region.intersects(d.Diagnostic.ByteRange) {
			d.Diagnostic.ExtendsRegion = selection != nil && region.extends(d.Diagnostic.ByteRange)
			v, e := json.Marshal(d)
			if e != nil {
				return full, e
			}
			suppressed = append(suppressed, v)
		}
	}
	out.Result.SuppressedDiagnostics = suppressed
	if p.Directives.Data != nil {
		for i := range *p.Directives.Data {
			if err := b.charge(1); err != nil {
				return full, err
			}
			d := &(*p.Directives.Data)[i]
			d.ContextOnly = selection != nil && !region.intersects(d.ByteRange)
		}
	}
	if p.RawCandidates.Data != nil {
		for i := range *p.RawCandidates.Data {
			if err := b.charge(1); err != nil {
				return full, err
			}
			d := &(*p.RawCandidates.Data)[i]
			d.ContextOnly = selection != nil && !region.intersects(d.ByteRange)
		}
	}
	label := func(n *result.Note) {
		n.Relevance = "whole-document"
		if selection != nil && n.ByteRange != nil {
			n.Relevance = "context"
			if n.ByteRange != nil && region.intersects(*n.ByteRange) {
				n.Relevance = "intersects"
			}
		}
	}
	for i := range out.Result.Notes {
		var n result.Note
		if err := json.Unmarshal(out.Result.Notes[i], &n); err != nil {
			return full, err
		}
		label(&n)
		out.Result.Notes[i], err = json.Marshal(n)
		if err != nil {
			return full, err
		}
	}
	if p.ParsingNotes.Data != nil {
		for i := range *p.ParsingNotes.Data {
			label(&(*p.ParsingNotes.Data)[i])
		}
	}
	p.RuleCounts = unavailable[json.RawMessage]("effective-state-unresolved")
	if p.Rules.Data == nil {
		p.RuleCounts.Reasons = append([]string{}, p.Rules.Reasons...)
		p.RuleCounts.CauseIDs = append([]string{}, p.Rules.CauseIDs...)
	} else {
		known, active, resolved := len(*p.Rules.Data), 0, true
		for i := range *p.Rules.Data {
			if err := b.charge(1); err != nil {
				return full, err
			}
			r := &(*p.Rules.Data)[i]
			if r.EffectiveState == "unresolved" {
				resolved = false
				continue
			}
			enabled := false
			for _, interval := range r.Intervals {
				if err := b.charge(1); err != nil {
					return full, err
				}
				if interval.Enabled && region.intersects(interval.ByteRange) {
					enabled = true
				}
			}
			r.SelectedEnabled = &enabled
			if enabled {
				active++
			}
		}
		if resolved {
			counts, _ := json.Marshal(struct {
				Known    int `json:"known"`
				Active   int `json:"active"`
				Inactive int `json:"inactive"`
			}{known, active, known - active})
			p.RuleCounts = available(json.RawMessage(counts))
		}
	}
	if err := b.charge(0); err != nil {
		return full, err
	}
	out.Result.Inspection, err = json.Marshal(p)
	if err != nil {
		return full, err
	}
	if err := b.charge(0); err != nil {
		return full, err
	}
	out.snapshot = s
	out.wire = full.wire
	out.records = full.records
	out.filterNodes = full.filterNodes
	return out, nil
}

// Count materialized JSON object/array records, excluding delimiters inside
// strings. Byte length measures copy/traversal work, not construction records.
func jsonStructures(raw []byte) int64 {
	var count int64
	quoted, escaped := false, false
	for _, c := range raw {
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
		} else if c == '{' || c == '[' {
			count++
		}
	}
	return count
}
