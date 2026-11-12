// Correction facts for MD050.

package parser

import "context"

// BoldMarkerCorrections returns the expected bold marker for style and the
// Strong facts of DelimiterStyle whose two-byte markers differ from it and can
// both be rewritten to it. For consistent, the expected marker is that of the
// minimum Strong opening, as MD050 analysis selects it, and zero when there is
// none. A fact qualifies only when no byte next to either marker is an asterisk
// or underscore, and reparsing the source with both markers rewritten gives the
// same tree, with only that Strong's marker changed, and the same delimiter
// inventory at the same offsets. All candidates are first reparsed together;
// only when that fails is each one reparsed alone.
func (d *Document) BoldMarkerCorrections(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, style string) ([]DelimiterFact, byte, error) {
	inventory, err := d.DelimiterStyle(ctx, budget, index, options)
	if err != nil {
		return nil, 0, err
	}
	var expected byte
	switch style {
	case "asterisk":
		expected = '*'
	case "underscore":
		expected = '_'
	case "consistent":
		first := index.source.Len()
		for _, f := range inventory.Valid {
			if f.Kind == Strong && f.Opening.start < first {
				first, expected = f.Opening.start, f.Marker
			}
		}
	default:
		return nil, 0, ErrModel
	}
	content := index.source.content
	marker := func(b byte) bool { return b == '*' || b == '_' }
	isolated := func(s Span) bool {
		return (s.start == 0 || !marker(content[s.start-1])) && (s.end == len(content) || !marker(content[s.end]))
	}
	var candidates []DelimiterFact
	for _, f := range inventory.Valid {
		if err := headingFactCharge(ctx, budget, 5, 0); err != nil {
			return nil, 0, err
		}
		if f.Kind != Strong || f.Width != 2 || f.Marker == expected || expected == 0 {
			continue
		}
		if f.Opening.end-f.Opening.start != 2 || f.Closing.end-f.Closing.start != 2 || !isolated(f.Opening) || !isolated(f.Closing) {
			continue
		}
		candidates = append(candidates, f)
	}
	if len(candidates) == 0 {
		return nil, expected, nil
	}
	if ok, err := d.boldMarkersPreserved(ctx, budget, options, inventory.Valid, candidates, expected); err != nil || ok {
		return candidates, expected, err
	}
	if len(candidates) == 1 {
		return nil, expected, nil
	}
	var out []DelimiterFact
	for _, f := range candidates {
		ok, err := d.boldMarkersPreserved(ctx, budget, options, inventory.Valid, []DelimiterFact{f}, expected)
		if err != nil {
			return nil, 0, err
		}
		if ok {
			out = append(out, f)
		}
	}
	return out, expected, nil
}

// boldMarkersPreserved reparses the source with the markers of every fact in
// rewrite set to expected and compares the result with d.
func (d *Document) boldMarkersPreserved(ctx context.Context, budget *OperationBudget, options ParseOptions, before, rewrite []DelimiterFact, expected byte) (bool, error) {
	source := d.Source()
	if err := headingFactCharge(ctx, budget, int64(source.Len())*2+int64(len(before))*2+1, int64(len(before))+1); err != nil {
		return false, err
	}
	content := source.Bytes()
	changed := make(map[int]bool, len(rewrite))
	for _, f := range rewrite {
		changed[f.Opening.start] = true
		for _, s := range [2]Span{f.Opening, f.Closing} {
			content[s.start], content[s.start+1] = expected, expected
		}
	}
	after := ParseWithOptions(ctx, NewSource(content), budget, options)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if after.Status != Complete || after.Document == nil || len(after.Uncertainty) != 0 {
		return false, nil
	}
	if same, err := d.SameMeaningAcrossBoldMarkers(ctx, budget, after.Document); err != nil || !same {
		return false, err
	}
	inventory, err := after.Document.DelimiterStyle(ctx, budget, NewLineIndex(after.Source), options)
	if err != nil {
		return false, err
	}
	if len(inventory.Valid) != len(before) {
		return false, nil
	}
	for i, f := range before {
		g := inventory.Valid[i]
		want := f.Marker
		if f.Kind == Strong && changed[f.Opening.start] {
			want = expected
		}
		if g.Kind != f.Kind || g.Width != f.Width || g.Marker != want {
			return false, nil
		}
		for j, s := range [3]Span{f.Envelope, f.Opening, f.Closing} {
			t := [3]Span{g.Envelope, g.Opening, g.Closing}[j]
			if s.start != t.start || s.end != t.end {
				return false, nil
			}
		}
	}
	return true, nil
}
