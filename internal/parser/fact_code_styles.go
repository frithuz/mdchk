// Facts for MD046.

package parser

import (
	"context"
	"errors"
)

// CodeStyleFact is a detached original representation marker and its required
// style. It is an observation, never permission to convert a code block.
type CodeStyleFact struct {
	Span     Span
	Expected string
}

// CodeStyleInventory withholds every observation if necessary context is unknown.
type CodeStyleInventory struct {
	Facts        []CodeStyleFact
	Availability Availability
}

type codeStyleTarget struct {
	node  *Node
	style string // Empty means unavailable original Code metadata.
}

// CodeStyles borrows original completed syntax and its existing allowance. The
// first eligible block sets consistent style before suppression or focus. No
// payload, language, container prefix replay or replacement parser is involved.
func (d *Document) CodeStyles(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, style string) (out CodeStyleInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = CodeStyleInventory{}
		} else if out.Availability != Available {
			out.Facts = nil
		}
	}()
	charge := func(work, nodes int64) error { return headingFactCharge(ctx, budget, work, nodes) }
	if err = charge(3*int64(len(style))+3, 0); err != nil {
		return out, err
	}
	if style != "consistent" && style != "fenced" && style != "indented" {
		return out, ErrModel
	}
	walk := shellPromptWalk{ctx: ctx, budget: budget, source: index.source, root: d.root}
	if err = walk.push(d.root); err != nil {
		return out, err
	}
	var selected []codeStyleTarget
	first, allSame, previous := "", true, 0
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
		if err = charge(16, 0); err != nil {
			return out, err
		}
		if n.spec.Envelope.start < previous {
			return out, ErrModel
		}
		previous = n.spec.Envelope.end
		actual := ""
		if code := n.spec.Properties.Code; code != nil {
			actual = code.Style
			if err = charge(2*int64(len(actual))+2, 0); err != nil {
				return out, err
			}
			if actual != "fenced" && actual != "indented" {
				return out, ErrModel
			}
		}
		if err = charge(32, 0); err != nil {
			return out, err
		}
		if actual == "" {
			allSame = false
		} else if first == "" {
			first = actual
		} else if actual != first {
			allSame = false
		}
		if style != "consistent" && actual == style {
			continue
		}
		selected, err = shellPromptGrow(ctx, budget, selected)
		if err != nil {
			return out, err
		}
		if err = charge(1, 1); err != nil {
			return out, err
		}
		selected = append(selected, codeStyleTarget{node: n, style: actual})
	}
	if err = charge(12, 0); err != nil {
		return out, err
	}
	// Every exclusion subset of identical known styles is still consistent.
	// Unknown metadata disables this proof; it cannot elect a baseline.
	if len(selected) == 0 || style == "consistent" && allSame {
		return CodeStyleInventory{Availability: Available}, nil
	}
	if err = walk.push(d.root); err != nil {
		return out, err
	}
	expected := style
	if style == "consistent" {
		expected = ""
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
				return CodeStyleInventory{Availability: Unavailable}, nil
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
		if err = charge(20, 0); err != nil {
			return out, err
		}
		if raw.name != "" {
			continue
		}
		if target.style == "" {
			return CodeStyleInventory{Availability: Unavailable}, nil
		}
		if expected == "" {
			expected = target.style
		}
		if target.style == expected {
			continue
		}
		span, known, e := codeStyleMarker(ctx, budget, index, n, target.style)
		if e != nil {
			return out, e
		}
		if !known {
			return CodeStyleInventory{Availability: Unavailable}, nil
		}
		out.Facts, err = shellPromptGrow(ctx, budget, out.Facts)
		if err != nil {
			return out, err
		}
		if err = charge(1, 1); err != nil {
			return out, err
		}
		out.Facts = append(out.Facts, CodeStyleFact{Span: span, Expected: expected})
	}
	out.Availability = Available
	return out, nil
}

func codeStyleMarker(ctx context.Context, budget *OperationBudget, index LineIndex, n *Node, style string) (Span, bool, error) {
	charge := func(work int64) error { return headingFactCharge(ctx, budget, work, 0) }
	if err := charge(24); err != nil {
		return Span{}, false, err
	}
	if len(n.spec.Markers) == 0 || !n.spec.Markers[0].Source.present {
		return Span{}, false, nil
	}
	if style == "fenced" {
		code := n.spec.Properties.Code
		if !code.Fence.Present || !code.Width.Present {
			return Span{}, false, nil
		}
		span, _, err := fencedLanguageOpening(ctx, budget, index, n)
		return span, err == nil, err
	}
	marker := n.spec.Markers[0]
	span := marker.Source
	if err := charge(int64(len(marker.Role)) + 16); err != nil {
		return Span{}, false, err
	}
	if style != "indented" || marker.Role != "indent-prefix" || span.SourceToken() != index.source.Token() || span.start != n.spec.Envelope.start || span.end <= span.start || span.end > n.spec.Envelope.end {
		return Span{}, false, ErrModel
	}
	line, err := fencedSpacingLine(ctx, budget, index, span.start)
	if err != nil {
		return Span{}, false, err
	}
	if err = charge(2); err != nil {
		return Span{}, false, err
	}
	if span.end > index.lines[line].end {
		return Span{}, false, ErrModel
	}
	// The original builder already consumed exactly four intrinsic columns.
	// Validate its source spelling without replaying partially consumed TABs.
	for at := span.start; ; at++ {
		if err = charge(1); err != nil {
			return Span{}, false, err
		}
		if at == span.end {
			break
		}
		if index.source.content[at] != ' ' && index.source.content[at] != '\t' {
			return Span{}, false, ErrModel
		}
	}
	return span, true, nil
}
