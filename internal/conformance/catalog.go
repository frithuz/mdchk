package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
)

// Fact is an expectation-independent syntax or metadata witness. Parent IDs
// preserve real ancestry even when authored assertions omit intervening nodes.
type Fact struct {
	ID      int            `json:"id"`
	Parent  int            `json:"parent"`
	Kind    string         `json:"kind"`
	Span    any            `json:"span"`
	Content []any          `json:"content"`
	Facts   map[string]any `json:"facts"`
}

type Actual struct {
	Result
	Catalog   []Fact           `json:"catalog"`
	Positions []map[string]int `json:"positions"`
}

// EvaluateActual retains the parse's operation for protection, rendering and
// catalog work. Failed work never returns partial successful HTML or facts.
func EvaluateActual(ctx context.Context, r parser.ParseResult, budget *parser.OperationBudget) Actual {
	if (r.Limit != nil) != (r.Interruption == parser.LimitExceededReason) {
		return Actual{Result: Result{SchemaVersion: 1, Status: "error", Reason: "invalid-outcome", Message: "inconsistent typed limit outcome"}, Catalog: []Fact{}, Positions: []map[string]int{}}
	}

	if r.Source.Len() > 16384 && r.Status == parser.Complete && r.Failure == nil && r.Interruption == "" {
		return Actual{Result: Result{SchemaVersion: 1, Status: "error", Reason: "actual-export", Message: "actual coordinate limit: 16384 bytes"}, Catalog: []Fact{}, Positions: []map[string]int{}}
	}

	out := Actual{Result: evaluate(r, ctx, budget), Catalog: []Fact{}, Positions: []map[string]int{}}
	if out.Status != "rendered" {
		return out
	}
	fail := func(err error) Actual {
		return Actual{Result: Result{SchemaVersion: 1, Status: "error", Reason: "actual-export", Message: err.Error()}, Catalog: []Fact{}, Positions: []map[string]int{}}
	}
	protected, err := r.Document.ProtectedNodes(ctx, budget)
	if err != nil {
		return fail(err)
	}
	literal, err := r.Document.LiteralSpans(ctx, budget)
	if err != nil {
		return fail(err)
	}
	_ = literal // Both completed, operation-bound authorities must be available.
	// Bound coordinate records before allocation; this test protocol deliberately
	// has a smaller export envelope than the parser's general input admission.
	if r.Source.Len() > 16384 {
		return fail(fmt.Errorf("actual coordinate limit: 16384 bytes"))
	}
	if err := budget.ChargeNodes(int64(r.Source.Len()) + 1); err != nil {
		return fail(err)
	}
	if err := budget.ChargeWork((int64(r.Source.Len()) + 1) * 256); err != nil {
		return fail(err)
	}
	if err := budget.ChargeWork(int64(r.Source.Len())*64 + 128); err != nil {
		return fail(err)
	}
	if err := budget.ChargeNodes(int64(r.Source.Len()) + 1); err != nil {
		return fail(err)
	}
	c := catalogBuilder{source: r.Source, budget: budget, ctx: ctx, protected: map[protectedKey]bool{}, refs: r.Document.References()}
	for _, n := range protected {
		c.protected[protectedKey{n.Kind(), n.Span()}] = true
	}
	c.definitions = make([]parser.ReferenceSpec, len(c.refs.Definitions))
	for i, definition := range c.refs.Definitions {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		// The aggregate copy is already source-bounded/precharged; retain one cached
		// immutable definition view instead of copying its mappings on every use.
		c.definitions[i] = definition.Properties()
	}

	if err := c.walk(r.Document.Root(), -1, nil); err != nil {
		return fail(err)
	}
	out.Catalog = c.facts
	index := parser.NewLineIndex(r.Source)
	// Export source coordinates independently of expected requested offsets.
	for offset := 0; offset <= r.Source.Len(); offset++ {
		pos, e := index.PositionBounded(ctx, budget, offset)
		if errors.Is(e, parser.ErrPosition) {
			continue
		}
		if e != nil {
			return fail(e)
		}
		out.Positions = append(out.Positions, map[string]int{"offset": offset, "line": pos.Line, "column": pos.Column})
	}
	// Reserve serialization/copy work, then enforce a finite wire-size envelope.
	if err := budget.ChargeWork(int64(r.Source.Len())*256 + int64(len(c.facts))*1024); err != nil {
		return fail(err)
	}
	data, err := json.Marshal(out)
	if err != nil {
		return fail(err)
	}
	if len(data) >= maxOutputBytes {
		return fail(fmt.Errorf("actual response exceeds %d bytes", maxOutputBytes))
	}
	if err := budget.ChargeWork(int64(len(data)) * 2); err != nil {
		return fail(err)
	}
	// Serialization is a bounded bulk copy without context callbacks. Check the
	// caller once more before publishing any successful HTML or actual facts.
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return out
}

type protectedKey struct {
	kind parser.Kind
	span parser.Span
}

type catalogBuilder struct {
	source       parser.Source
	budget       *parser.OperationBudget
	ctx          context.Context
	protected    map[protectedKey]bool
	refs         parser.ReferenceCollection
	definitions  []parser.ReferenceSpec
	facts        []Fact
	wireReserved int64
}

func (c *catalogBuilder) span(s parser.Span) any {
	if !s.Present() {
		return nil
	}
	raw, e := c.source.Text(s)
	if e != nil {
		panic(e)
	}
	return []any{s.Start(), s.End(), raw}
}

// Adjacent source ranges are coalesced only in the selected content view;
// transformations remain separately available in actual parts/typed facts.
func (c *catalogBuilder) content(parts []parser.Segment) []any {
	spans := []parser.Span{}
	for _, s := range parts {
		if !s.Source.Present() {
			continue
		}
		l := len(spans)
		if l > 0 && spans[l-1].End() == s.Source.Start() {
			spans[l-1], _ = c.source.Span(spans[l-1].Start(), s.Source.End())
		} else {
			spans = append(spans, s.Source)
		}
	}
	out := []any{}
	for _, s := range spans {
		out = append(out, c.span(s))
	}
	return out
}
func (c *catalogBuilder) parts(parts []parser.Segment) []any {
	out := []any{}
	for _, p := range parts {
		v := p.Semantic.Value
		if !p.Semantic.Present {
			v, _ = c.source.Text(p.Source)
		}
		out = append(out, map[string]any{"source": c.span(p.Source), "value": v, "transformation": transformation(c.source, p)})
	}
	return out
}
func (c *catalogBuilder) envelope(parts []parser.Segment) any {
	var first, last parser.Span
	for _, p := range parts {
		if p.Source.Present() {
			if !first.Present() {
				first = p.Source
			}
			last = p.Source
		}
	}
	if !first.Present() {
		return nil
	}
	s, _ := c.source.Span(first.Start(), last.End())
	return c.span(s)
}
func (c *catalogBuilder) mapped(parts []parser.Segment) string {
	var b strings.Builder
	for _, s := range parts {
		if s.Semantic.Present {
			b.WriteString(s.Semantic.Value)
		} else {
			v, _ := c.source.Text(s.Source)
			b.WriteString(v)
		}
	}
	return b.String()
}
func (c *catalogBuilder) walk(n *parser.Node, parent int, list *parser.ListProperties) error {
	// Reserve conservatively before cloned properties, fact maps or source
	// inventories allocate. The finite total bounds overlapping container copies.
	reserve := int64(4096) + int64(n.Span().End()-n.Span().Start())*256
	if reserve > int64(maxOutputBytes)-c.wireReserved {
		return fmt.Errorf("actual catalog byte reservation exceeded")
	}
	c.wireReserved += reserve
	if len(c.facts) >= 20000 {
		return fmt.Errorf("actual fact limit exceeded")
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if err := c.budget.ChargeWork(256 + int64(n.Span().End()-n.Span().Start())*32); err != nil {
		return err
	}
	if err := c.budget.ChargeNodes(1); err != nil {
		return err
	}
	p := n.Properties()
	// Reference destinations, titles and selected definitions live outside the
	// short use's envelope. Reserve their repeated wire/copy expansion separately.
	extra := int64(0)
	if p.Link != nil {
		extra = int64(len(p.Link.Destination.Semantic.Value)) * 16
		if p.Link.Title != nil {
			extra += int64(len(p.Link.Title.Semantic.Value)) * 16
		}
		if i, ok := c.refs.Lookup[p.Link.NormalizedLabel.Value]; p.Link.NormalizedLabel.Present && ok {
			d := c.definitions[i]
			extra += int64(d.Envelope.End()-d.Envelope.Start()) * 64
		}
	}
	if extra > int64(maxOutputBytes)-c.wireReserved {
		return fmt.Errorf("actual catalog byte reservation exceeded")
	}
	c.wireReserved += extra
	if err := c.budget.ChargeWork(extra); err != nil {
		return err
	}
	f := Fact{ID: len(c.facts), Parent: parent, Kind: string(n.Kind()), Span: c.span(n.Span()), Content: c.content(n.Content()), Facts: map[string]any{}}
	a := f.Facts
	// Full mapped contributions and marker roles remain available independently
	// of the coalesced selected content view, including raw HTML and NUL/tab maps.
	a["mappings"] = c.parts(n.Content())
	markers := []any{}
	for _, m := range n.Markers() {
		markers = append(markers, map[string]any{"role": m.Role, "source": c.span(m.Source)})
	}
	a["markers"] = markers
	for _, m := range n.Markers() {
		switch m.Role {
		case "opening", "closing", "ending":
			a[m.Role] = c.span(m.Source)
		}
	}
	if p.Literal.Present {
		a["value"] = p.Literal.Value
	}
	if p.Delimiter != nil {
		a["arity"] = p.Delimiter.Width
	}
	if c.protected[protectedKey{n.Kind(), n.Span()}] {
		a["protected"] = true
	}
	if n.Kind() == parser.CodeSpan {
		a["literal"] = p.Literal.Value
		a["normalization"] = "none"
		for _, part := range n.Content() {
			if part.Transformation == parser.Escape {
				a["normalization"] = "table-escaped-pipe"
			}
			if part.Transformation == parser.Normalized {
				a["normalization"] = "code-span"
			}
		}
	}
	if n.Kind() == parser.Text {
		a["parts"] = c.parts(n.Content())
	}
	if n.Kind() == parser.Paragraph {
		a["value"] = c.mapped(n.Content())
		// Physical terminal ending belongs to the recognized envelope, separately
		// from logical Content. This inspects only that fixed original suffix.
		raw, _ := c.source.Text(n.Span())
		width := 0
		if strings.HasSuffix(raw, "\r\n") {
			width = 2
		} else if strings.HasSuffix(raw, "\n") || strings.HasSuffix(raw, "\r") {
			width = 1
		}
		if width > 0 {
			s, _ := c.source.Span(n.Span().End()-width, n.Span().End())
			a["ending"] = c.span(s)
		}
	}
	if n.Kind() == parser.BlockQuote {
		prefix := []any{}
		for _, m := range n.Markers() {
			if m.Role == "quote-prefix" {
				prefix = append(prefix, c.span(m.Source))
			}
		}
		a["prefixes"] = prefix
	}
	if n.Kind() == parser.List {
		a["tight"] = p.List.Tight.Value
		list = p.List
	}
	if n.Kind() == parser.ListItem && p.Task != nil {
		f.Kind = "task-item"
		t := p.Task
		a["item"] = c.span(n.Span())
		a["paragraph"] = c.span(t.Paragraph)
		a["marker"] = c.span(t.Marker)
		a["check"] = c.span(t.Check)
		a["separator"] = c.span(t.Separator)
		a["checked"] = t.Checked
		if list == nil {
			return fmt.Errorf("task without list")
		}
		a["tight"] = list.Tight.Value
		if p.List != nil && p.List.Prefix != nil {
			a["list_marker"] = c.envelope(p.List.Prefix.Segments)
		}
		prefixes := []any{}
		for _, m := range n.Markers() {
			if m.Role == "item-indent" {
				prefixes = append(prefixes, c.span(m.Source))
			}
		}
		a["continuation_prefixes"] = prefixes
	}
	if p.Link != nil {
		l := p.Link
		origin := l.Origin
		if origin == "autolink" {
			origin = "angle"
		}
		if origin == "full" || origin == "collapsed" || origin == "shortcut" {
			origin = "reference"
		}
		a["origin"] = origin
		a["visible"] = c.envelope(l.Label.Segments)
		a["destination"] = l.Destination.Semantic.Value
		if origin == "reference" {
			a["parts"] = []any{map[string]any{"source": nil, "value": l.Destination.Semantic.Value, "transformation": "reference"}}
			i, ok := c.refs.Lookup[l.NormalizedLabel.Value]
			if !ok {
				return fmt.Errorf("missing reference binding")
			}
			d := c.definitions[i]
			a["binding"] = map[string]any{"definition": c.span(d.Envelope), "label": c.envelope(d.Label.Segments), "destination": c.envelope(d.Destination.Segments), "normalized_label": d.NormalizedLabel.Value}
		} else {
			a["parts"] = c.parts(l.Destination.Segments)
		}
	}
	if p.Table != nil {
		a["width"] = p.Table.Width
		rows, err := c.rows(n, p.Table)
		if err != nil {
			return err
		}
		a["rows"] = rows
	}
	c.facts = append(c.facts, f)
	// Metadata follows its owner before the next syntax sibling.
	if p.HTML != nil {
		for _, tag := range p.HTML.FilteredTags {
			if err := c.budget.ChargeNodes(1); err != nil {
				return err
			}
			span := c.envelope(tag.Prefix.Segments)
			if tag.Tag != nil {
				span = c.envelope(tag.Tag.Segments)
			}
			c.facts = append(c.facts, Fact{ID: len(c.facts), Parent: f.ID, Kind: "filtered-tag", Span: span, Content: []any{}, Facts: map[string]any{"name": tag.Name, "affected": c.span(tag.Affected), "protected": c.span(n.Span()), "closing": tag.Closing}})
		}
	}
	for _, child := range n.Children() {
		if err := c.walk(child, f.ID, list); err != nil {
			return err
		}
	}
	return nil
}
func (c *catalogBuilder) rows(n *parser.Node, t *parser.TableProperties) ([]any, error) {
	rows := []any{}
	for _, row := range n.Children() {
		p := row.Properties().Row
		if err := c.budget.ChargeNodes(16 + int64(p.RawCells)*8 + int64(p.Padding)); err != nil {
			return nil, err
		}
		if err := c.budget.ChargeWork(128 + int64(row.Span().End()-row.Span().Start())*32 + int64(p.Padding)*64); err != nil {
			return nil, err
		}
		cells := []any{}
		escapes := []any{}
		padding := []any{}
		for _, cell := range row.Children() {
			v := c.mapped(cell.Content())
			if p.Role == "delimiter" {
				var b strings.Builder
				for _, m := range cell.Markers() {
					raw, _ := c.source.Text(m.Source)
					b.WriteString(raw)
				}
				v = b.String()
			}
			cells = append(cells, map[string]any{"span": c.span(cell.Span()), "value": v, "padding": false})
			for _, e := range cell.Properties().Cell.EscapedPipes {
				escapes = append(escapes, c.span(e))
			}
		}
		for i := 0; i < p.Padding; i++ {
			padding = append(padding, map[string]any{"span": nil, "value": "", "padding": true})
		}
		sep := []any{}
		for _, s := range p.Separators {
			sep = append(sep, c.span(s))
		}
		rows = append(rows, map[string]any{"role": p.Role, "span": c.span(row.Span()), "cells": cells, "source_count": p.RawCells, "semantic_count": t.Width, "alignment": t.Alignments, "separators": sep, "outer": []bool{p.LeadingPipe.Present(), p.TrailingPipe.Present()}, "padding": padding, "escaped_pipes": escapes})
	}
	return rows, nil
}

// Serialization distinguishes the already recognized numeric entity mapping.
func transformation(source parser.Source, p parser.Segment) string {
	if p.Transformation == parser.Entity {
		raw, _ := source.Text(p.Source)
		if strings.HasPrefix(raw, "&#") {
			return "numeric-entity"
		}
		return "named-entity"
	}
	return string(p.Transformation)
}
