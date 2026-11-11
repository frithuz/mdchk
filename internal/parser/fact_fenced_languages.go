// Facts for MD040.

package parser

import (
	"context"
	"errors"
	"unicode/utf8"
)

// FencedLanguageFact owns an original observation: a missing-language point or
// the original spelling of a disallowed token. It grants no correction authority.
type FencedLanguageFact struct {
	Span    Span
	Missing bool
}

// FencedLanguageInventory owns detached facts. Unknown necessary context
// withholds every fact, including provisional observations from earlier blocks.
type FencedLanguageInventory struct {
	Facts        []FencedLanguageFact
	Availability Availability
}

type fencedLanguageTarget struct {
	node       *Node
	known      bool
	missing    bool
	start, end int // Offsets in the already-decoded semantic Info, never source.
}

// FencedLanguages borrows completed original syntax and the original allowance.
// Allowed is a synchronous read-only borrow, never retained or mutated. Selection
// precedes raw ownership so known allowed labels need no diagnostic mapping or
// unrelated HTML context. There is no payload scan, attribute parser or reparse.
func (d *Document) FencedLanguages(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, allowed []string) (out FencedLanguageInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = FencedLanguageInventory{}
		} else if out.Availability != Available {
			out.Facts = nil
		}
	}()
	charge := func(work, nodes int64) error { return headingFactCharge(ctx, budget, work, nodes) }
	walk := shellPromptWalk{ctx: ctx, budget: budget, source: index.source, root: d.root}
	if err = walk.push(d.root); err != nil {
		return out, err
	}
	var selected []fencedLanguageTarget
	for {
		n, e := walk.next()
		if e != nil {
			return out, e
		}
		if n == nil {
			break
		}
		if err = charge(1, 0); err != nil {
			return out, err
		}
		if n.spec.Kind != CodeBlock {
			continue
		}
		target, needed, e := fencedLanguageSelect(ctx, budget, n, allowed)
		if e != nil {
			return out, e
		}
		if !needed {
			continue
		}
		selected, err = shellPromptGrow(ctx, budget, selected)
		if err != nil {
			return out, err
		}
		if err = charge(1, 1); err != nil {
			return out, err
		}
		selected = append(selected, target)
	}
	if err = charge(1, 0); err != nil {
		return out, err
	}
	if len(selected) == 0 {
		return FencedLanguageInventory{Availability: Available}, nil
	}
	// Reuse traversal capacity and the existing original HTML ownership scanner.
	if err = walk.push(d.root); err != nil {
		return out, err
	}
	raw := conflictRawState{}
	var htmlWork *parserContext
	next := 0
	for {
		if err = charge(1, 0); err != nil {
			return out, err
		}
		if next == len(selected) {
			break
		}
		n, e := walk.next()
		if e != nil {
			return out, e
		}
		if n == nil {
			return out, ErrModel
		}
		if err = charge(2, 0); err != nil {
			return out, err
		}
		if n.spec.Kind == Image {
			continue
		}
		if n.spec.Kind == HTMLBlock {
			if htmlWork == nil {
				if err = charge(2, 2); err != nil {
					return out, err
				}
				htmlWork = &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
			}
			var available bool
			raw, _, available, err = conflictHTMLRaw(htmlWork, index.source, n, raw)
			if err != nil {
				return out, err
			}
			if !available {
				return FencedLanguageInventory{Availability: Unavailable}, nil
			}
			continue
		}
		if err = charge(int64(len(raw.name))+int64(len(n.rawTag))+1, 0); err != nil {
			return out, err
		}
		if n.rawTag != "" {
			if raw.name == "" && !n.rawClosing {
				raw = conflictRawState{name: n.rawTag, start: n.spec.Envelope.start}
			} else if raw.name == n.rawTag && n.rawClosing {
				raw = conflictRawState{}
			}
		}
		if err = charge(1, 0); err != nil {
			return out, err
		}
		if n != selected[next].node {
			continue
		}
		target := selected[next]
		next++
		if raw.name != "" {
			continue
		}
		if !target.known {
			return FencedLanguageInventory{Availability: Unavailable}, nil
		}
		opening, lineEnd, e := fencedLanguageOpening(ctx, budget, index, n)
		if e != nil {
			return out, e
		}
		span := Span{source: index.source, start: opening.end, end: opening.end, present: true}
		if !target.missing {
			var known bool
			span, known, err = fencedLanguageSpan(ctx, budget, index, target, opening.end, lineEnd)
			if err != nil {
				return out, err
			}
			if !known {
				return FencedLanguageInventory{Availability: Unavailable}, nil
			}
		}
		out.Facts, err = shellPromptGrow(ctx, budget, out.Facts)
		if err != nil {
			return out, err
		}
		if err = charge(1, 1); err != nil {
			return out, err
		}
		out.Facts = append(out.Facts, FencedLanguageFact{Span: span, Missing: target.missing})
	}
	out.Availability = Available
	return out, nil
}

func fencedLanguageSelect(ctx context.Context, budget *OperationBudget, n *Node, allowed []string) (target fencedLanguageTarget, needed bool, err error) {
	target.node = n
	charge := func(work int64) error { return headingFactCharge(ctx, budget, work, 0) }
	if err = charge(16); err != nil {
		return target, false, err
	}
	code := n.spec.Properties.Code
	if code == nil {
		return target, true, nil
	}
	if err = charge(2*int64(len(code.Style)) + 2); err != nil {
		return target, false, err
	}
	if code.Style == "indented" {
		return target, false, nil
	}
	if code.Style != "fenced" {
		return target, false, ErrModel
	}
	if code.Info == nil || !code.Info.Semantic.Present {
		return target, true, nil
	}
	text := code.Info.Semantic.Value
	for {
		if err = charge(2); err != nil {
			return target, false, err
		}
		if target.start == len(text) || text[target.start] != ' ' && text[target.start] != '\t' {
			break
		}
		target.start++
	}
	target.known = true
	if err = charge(2); err != nil {
		return target, false, err
	}
	if target.start == len(text) || text[target.start] == '{' {
		target.missing = true
		return target, true, nil
	}
	target.end = target.start
	for {
		if err = charge(2); err != nil {
			return target, false, err
		}
		if target.end == len(text) || text[target.end] == ' ' || text[target.end] == '\t' {
			break
		}
		target.end++
	}
	if err = charge(1); err != nil {
		return target, false, err
	}
	if len(allowed) == 0 {
		return target, false, nil
	}
	for i := 0; ; i++ {
		if err = charge(2); err != nil {
			return target, false, err
		}
		if i == len(allowed) {
			break
		}
		if len(allowed[i]) != target.end-target.start {
			continue
		}
		if err = charge(int64(target.end-target.start) + 1); err != nil {
			return target, false, err
		}
		if text[target.start:target.end] == allowed[i] {
			return target, false, nil
		}
	}
	return target, true, nil
}

// The actual fenced builder stores its opening marker first. Only that marker
// is needed here; payload indentation and the closing marker cannot affect info.
func fencedLanguageOpening(ctx context.Context, budget *OperationBudget, index LineIndex, n *Node) (Span, int, error) {
	charge := func(work int64) error { return headingFactCharge(ctx, budget, work, 0) }
	if err := charge(16); err != nil {
		return Span{}, 0, err
	}
	code := n.spec.Properties.Code
	if code == nil || !code.Fence.Present || code.Fence.Value != '`' && code.Fence.Value != '~' || !code.Width.Present || code.Width.Value < 3 || len(n.spec.Markers) == 0 {
		return Span{}, 0, ErrModel
	}
	marker := n.spec.Markers[0]
	if err := charge(int64(len(marker.Role)) + 16); err != nil {
		return Span{}, 0, err
	}
	span := marker.Source
	if marker.Role != "opening" || !span.present || span.SourceToken() != index.source.Token() || span.start < n.spec.Envelope.start || span.end > n.spec.Envelope.end || span.end-span.start != code.Width.Value {
		return Span{}, 0, ErrModel
	}
	line, err := fencedSpacingLine(ctx, budget, index, n.spec.Envelope.start)
	if err != nil {
		return Span{}, 0, err
	}
	if err = charge(2); err != nil {
		return Span{}, 0, err
	}
	if span.start < index.lines[line].start || span.end > index.lines[line].end {
		return Span{}, 0, ErrModel
	}
	for at := span.start; ; at++ {
		if err = charge(1); err != nil {
			return Span{}, 0, err
		}
		if at == span.end {
			break
		}
		if index.source.content[at] != code.Fence.Value {
			return Span{}, 0, ErrModel
		}
	}
	return span, index.lines[line].end, nil
}

// Map semantic offsets through original segments, slicing only Exact mappings.
// A contributing transformation is atomic; no entity/escape is decoded again.
// Mappings after the selected token and unused attribute mappings are irrelevant.
func fencedLanguageSpan(ctx context.Context, budget *OperationBudget, index LineIndex, target fencedLanguageTarget, openingEnd, lineEnd int) (Span, bool, error) {
	charge := func(work int64) error { return headingFactCharge(ctx, budget, work, 0) }
	info := target.node.spec.Properties.Code.Info
	at, previous := 0, openingEnd
	var out Span
	for i := 0; ; i++ {
		if err := charge(1); err != nil {
			return Span{}, false, err
		}
		if at >= target.end {
			break
		}
		if i == len(info.Segments) {
			return Span{}, false, nil
		}
		part := info.Segments[i]
		if err := charge(16); err != nil {
			return Span{}, false, err
		}
		if !part.Semantic.Present {
			return Span{}, false, nil
		}
		text := part.Semantic.Value
		if len(text) > len(info.Semantic.Value)-at {
			return Span{}, false, ErrModel
		}
		if err := charge(int64(len(text)) + 1); err != nil {
			return Span{}, false, err
		}
		if text != info.Semantic.Value[at:at+len(text)] {
			return Span{}, false, ErrModel
		}
		from, to := max(target.start-at, 0), min(target.end-at, len(text))
		at += len(text)
		if from >= to {
			continue
		}
		if err := charge(24); err != nil {
			return Span{}, false, err
		}
		if part.Transformation == MappingUnavailable || part.Transformation == Synthetic {
			return Span{}, false, nil
		}
		span := part.Source
		if !span.present || span.SourceToken() != index.source.Token() || span.start < previous || span.end <= span.start || span.end > lineEnd {
			return Span{}, false, ErrModel
		}
		switch part.Transformation {
		case Exact:
			if err := charge(int64(len(text)) + 1); err != nil {
				return Span{}, false, err
			}
			if span.end-span.start != len(text) || index.source.content[span.start:span.end] != text {
				return Span{}, false, ErrModel
			}
			span.start += from
			span.end = part.Source.start + to
		case Entity, Escape, Normalized, PartialTab:
			// Original private provenance, not a reconstructed semantic byte span.
		default:
			return Span{}, false, ErrModel
		}
		if err := charge(4); err != nil {
			return Span{}, false, err
		}
		if !utf8.RuneStart(index.source.content[span.start]) || span.end < index.source.Len() && !utf8.RuneStart(index.source.content[span.end]) {
			return Span{}, false, ErrModel
		}
		if !out.present {
			out = span
		} else {
			out.end = span.end
		}
		previous = span.end
	}
	if err := charge(1); err != nil {
		return Span{}, false, err
	}
	if !out.present {
		return Span{}, false, nil
	}
	return out, true, nil
}
