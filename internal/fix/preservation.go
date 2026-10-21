package fix

import (
	"context"
	"reflect"
	"slices"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// editMapping maps unchanged source facts, with insertion bias selecting original
// bytes (right at starts, left at ends). Facts intersecting replacements have no
// unchanged mapping. This is not a best-effort line diff.
type editMapping struct {
	before, after parser.Source
	edits         []SharedEdit
	meter         meter
}

func (m editMapping) offset(at int, right bool) (int, bool, error) {
	delta := 0
	for _, e := range m.edits {
		if err := m.meter.work(1); err != nil {
			return 0, false, err
		}
		a, b := e.Edit.Span.Start(), e.Edit.Span.End()
		if at < a {
			break
		}
		if a == b {
			if at > a || right {
				delta += len(e.Edit.Replacement)
			}
			continue
		}
		if at > a && at < b {
			return 0, false, nil
		}
		if at >= b {
			delta += len(e.Edit.Replacement) - (b - a)
		}
	}
	return at + delta, true, nil
}
func (m editMapping) span(s parser.Span) (parser.Span, bool, error) {
	if !s.Present() {
		return parser.Span{}, true, nil
	}
	if _, err := m.before.Text(s); err != nil {
		return parser.Span{}, false, nil
	}
	for _, e := range m.edits {
		if err := m.meter.work(1); err != nil {
			return parser.Span{}, false, err
		}
		if intersects(e.Edit.Span, s) {
			return parser.Span{}, false, nil
		}
	}
	a, ok, err := m.offset(s.Start(), true)
	if err != nil || !ok {
		return parser.Span{}, false, err
	}
	b, ok, err := m.offset(s.End(), false)
	if err != nil || !ok || b < a {
		return parser.Span{}, false, err
	}
	mapped, err := m.after.Span(a, b)
	return mapped, err == nil, nil
}
func sameMapped(m editMapping, a, b parser.Span) (bool, error) {
	s, ok, err := m.span(a)
	return ok && s == b, err
}
func commonPreservation(ctx context.Context, before, after validationState, p StructuralPlan, m meter) (bool, error) {
	old, new := before.trace.Parsed(), after.trace.Parsed()
	if old.Status != parser.Complete || new.Status != parser.Complete || old.Document == nil || new.Document == nil || len(old.Uncertainty) > 0 || len(new.Uncertainty) > 0 || !before.trace.Directives().Complete() || !after.trace.Directives().Complete() {
		return false, nil
	}
	mapping := editMapping{old.Source, new.Source, p.edits, m}
	// Reserve defensive accessor collections and comparisons before constructing
	// views. Actual byte scans and each mapping comparison are charged separately.
	if err := m.reserve(int64(old.Source.Len()+new.Source.Len())*8+16, 0); err != nil {
		return false, err
	}
	oc, err := old.Document.Comments(ctx, m.budget)
	if err != nil {
		return false, err
	}
	nc, err := new.Document.Comments(ctx, m.budget)
	if err != nil {
		return false, err
	}
	if len(oc) != len(nc) {
		return false, nil
	}
	for i, c := range oc {
		if err := m.work(1); err != nil {
			return false, err
		}
		if c.Protected != nc[i].Protected {
			return false, nil
		}
		for k, a := range []parser.Span{c.Envelope, c.Open, c.Close} {
			b := []parser.Span{nc[i].Envelope, nc[i].Open, nc[i].Close}[k]
			ok, err := sameMapped(mapping, a, b)
			if err != nil || !ok {
				return false, err
			}
		}
		a, _ := old.Source.Text(c.Envelope)
		b, _ := new.Source.Text(nc[i].Envelope)
		if a != b {
			return false, nil
		}
	}
	// Byte preservation alone cannot protect fence binding. Require the same
	// protected node kind and mapped binding. Code delimiter changes still require
	// identical mapped payload/info facts and semantics; HTML stays exact. A
	// trusted rule checker proves the intended delimiter correction separately.
	on, err := old.Document.ProtectedNodes(ctx, m.budget)
	if err != nil {
		return false, err
	}
	nn, err := new.Document.ProtectedNodes(ctx, m.budget)
	if err != nil {
		return false, err
	}
	if len(on) != len(nn) {
		return false, nil
	}
	for i, n := range on {
		if n.Kind() != nn[i].Kind() {
			return false, nil
		}
		if n.Kind() == parser.CodeBlock || n.Kind() == parser.CodeSpan {
			ok, err := sameCodeBinding(mapping, n, nn[i])
			if err != nil || !ok {
				return false, err
			}
			continue
		}
		ok, err := sameMapped(mapping, n.Span(), nn[i].Span())
		if err != nil || !ok {
			return false, err
		}
		a, _ := old.Source.Text(n.Span())
		b, _ := new.Source.Text(nn[i].Span())
		if err := m.work(int64(len(a)+len(b)) + 1); err != nil {
			return false, err
		}
		if a != b {
			return false, nil
		}
	}
	if ok, err := sameDirectives(mapping, before.trace.Directives(), after.trace.Directives(), before.target.Plan.Rules()); err != nil || !ok {
		return false, err
	}
	return true, m.work(0)
}
func sameDirectives(m editMapping, old, new engine.DirectiveResult, rules map[string]engine.ResolvedRule) (bool, error) {
	if err := old.ReservePreservationViews(m.meter.ctx, m.meter.budget); err != nil {
		return false, err
	}
	if err := new.ReservePreservationViews(m.meter.ctx, m.meter.budget); err != nil {
		return false, err
	}
	a, b := old.Candidates(), new.Candidates()
	if len(a) != len(b) {
		return false, nil
	}
	for i, x := range a {
		y := b[i]
		if err := m.meter.work(int64(len(x.Raw)+len(y.Raw)+len(x.Payload)+len(y.Payload)) + 1); err != nil {
			return false, err
		}
		if x.Raw != y.Raw || x.Payload != y.Payload || x.Operation != y.Operation || x.Disposition != y.Disposition || !reflect.DeepEqual(x.IDs, y.IDs) || len(x.Named) != len(y.Named) || len(x.Effects) != len(y.Effects) {
			return false, nil
		}
		for k, s := range []parser.Span{x.Span, x.Closing} {
			ok, err := sameMapped(m, s, []parser.Span{y.Span, y.Closing}[k])
			if err != nil || !ok {
				return false, err
			}
		}
		for k, t := range x.Named {
			if err := m.meter.work(int64(len(t.Name)) + 1); err != nil {
				return false, err
			}
			ok, err := sameMapped(m, t.Span, y.Named[k].Span)
			if err != nil || !ok || t.Name != y.Named[k].Name {
				return false, err
			}
		}
		for k, e := range x.Effects {
			f := y.Effects[k]
			ok, err := sameMapped(m, e.Token, f.Token)
			if err != nil || !ok || e.Rule != f.Rule || e.Disposition != f.Disposition || !reflect.DeepEqual(e.Restrictions, f.Restrictions) {
				return false, err
			}
		}
	}
	for id := range rules {
		if err := m.meter.work(int64(len(id)) + 1); err != nil {
			return false, err
		}
		ai, bi := old.Intervals(id), new.Intervals(id)
		if len(ai) != len(bi) {
			return false, nil
		}
		for i, x := range ai {
			y := bi[i]
			if x.Enabled != y.Enabled || x.Owner != y.Owner {
				return false, nil
			}
			ok, err := sameMapped(m, x.Token, y.Token)
			if err != nil || !ok {
				return false, err
			}
			start := 0
			if x.Owner >= 0 {
				start = b[x.Owner].Closing.End()
			}
			end := m.after.Len()
			if i+1 < len(ai) {
				next := ai[i+1]
				if next.Owner >= 0 {
					end = b[next.Owner].Closing.End()
				}
			}
			if y.Start != start || y.End != end {
				return false, nil
			}
		}
	}
	return true, nil
}

// Trusted registration names the rules whose newly introduced reliable findings
// are forbidden by this correction. Existing findings match by mapped byte range,
// rule, severity and message, independently of generated IDs/Unicode positions.
func preserveFindings(c CheckContext, rules []string) (bool, error) {
	if len(rules) == 0 {
		return true, nil
	}
	if err := reserveEncoded(c.Before.meter, c.Before.analysis); err != nil {
		return false, err
	}
	if err := reserveEncoded(c.Before.meter, c.After.analysis); err != nil {
		return false, err
	}
	before, after := c.Before.Analysis(), c.After.Analysis()
	type key struct {
		rule, severity, message string
		start, end              int
	}
	old := map[key]bool{}
	for _, d := range before.Diagnostics {
		if err := c.Before.meter.reserve(int64(len(d.Rule)+len(d.Message)+len(d.Severity)+len(rules))+1, 1); err != nil {
			return false, err
		}
		if !slices.Contains(rules, d.Rule) {
			continue
		}
		span, err := c.Before.current.Span(d.ByteRange.Start, d.ByteRange.End)
		if err != nil {
			return false, err
		}
		mapped, ok, err := c.MapUnchanged(span)
		if err != nil {
			return false, err
		}
		if ok {
			old[key{d.Rule, d.Severity, d.Message, mapped.Start(), mapped.End()}] = true
		}
	}
	for _, d := range after.Diagnostics {
		if err := c.Before.meter.work(int64(len(d.Rule)+len(d.Message)+len(d.Severity)+len(rules)) + 1); err != nil {
			return false, err
		}
		if slices.Contains(rules, d.Rule) && !old[key{d.Rule, d.Severity, d.Message, d.ByteRange.Start, d.ByteRange.End}] {
			return false, nil
		}
	}
	return true, nil
}

// Delimiters may change under trusted rule proof, but cannot change which exact
// original payload bytes belong to the code node or their semantic interpretation.
// An interior fence becoming a closer therefore fails independently of the proof.
func sameCodeBinding(m editMapping, a, b *parser.Node) (bool, error) {
	start, ok, err := m.offset(a.Span().Start(), true)
	if err != nil || !ok {
		return false, err
	}
	end, ok, err := m.offset(a.Span().End(), false)
	if err != nil || !ok || start != b.Span().Start() || end != b.Span().End() {
		return false, err
	}
	ap, bp := a.Properties(), b.Properties()
	if ap.Literal != bp.Literal {
		return false, nil
	}
	if (ap.Code == nil) != (bp.Code == nil) {
		return false, nil
	}
	if ap.Code != nil {
		if ap.Code.Style != bp.Code.Style {
			return false, nil
		}
		ok, err := sameCodeValue(m, ap.Code.Info, bp.Code.Info)
		if err != nil || !ok {
			return false, err
		}
	}
	return sameSegments(m, a.Content(), b.Content())
}
func sameCodeValue(m editMapping, a, b *parser.Value) (bool, error) {
	if (a == nil) != (b == nil) {
		return false, nil
	}
	if a == nil {
		return true, nil
	}
	if a.Semantic != b.Semantic {
		return false, nil
	}
	return sameSegments(m, a.Segments, b.Segments)
}
func sameSegments(m editMapping, a, b []parser.Segment) (bool, error) {
	if len(a) != len(b) {
		return false, nil
	}
	for i, s := range a {
		if err := m.meter.reserve(1, 1); err != nil {
			return false, err
		}
		if s.Semantic != b[i].Semantic || s.Transformation != b[i].Transformation {
			return false, nil
		}
		ok, err := sameMapped(m, s.Source, b[i].Source)
		if err != nil || !ok {
			return false, err
		}
		if s.Source.Present() {
			x, _ := m.before.Text(s.Source)
			y, _ := m.after.Text(b[i].Source)
			if err := m.meter.work(int64(len(x) + len(y))); err != nil {
				return false, err
			}
			if x != y {
				return false, nil
			}
		}
	}
	return true, nil
}
