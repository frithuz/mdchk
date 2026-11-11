package parser

import (
	"context"
	"errors"
)

// FenceStyleFact is a detached original opening marker and its required
// delimiter. It grants no permission to replace either fence.
type FenceStyleFact struct {
	Span     Span
	Expected string
}

// FenceStyleInventory withholds every observation if necessary context is unknown.
type FenceStyleInventory struct {
	Facts        []FenceStyleFact
	Availability Availability
}

type fenceStyleTarget struct {
	node  *Node
	style string // Empty means unavailable owner or delimiter metadata.
}

// FenceStyles borrows completed original syntax and its existing allowance. The
// first eligible fenced owner sets the delimiter before suppression or focus.
// Indented owners and opaque payload never supply delimiter candidates.
func (d *Document) FenceStyles(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, style string) (out FenceStyleInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = FenceStyleInventory{}
		} else if out.Availability != Available {
			out.Facts = nil
		}
	}()
	charge := func(work, nodes int64) error { return headingFactCharge(ctx, budget, work, nodes) }
	if err = charge(3*int64(len(style))+3, 0); err != nil {
		return out, err
	}
	if style != "consistent" && style != "backtick" && style != "tilde" {
		return out, ErrModel
	}
	walk := shellPromptWalk{ctx: ctx, budget: budget, source: index.source, root: d.root}
	if err = walk.push(d.root); err != nil {
		return out, err
	}
	var selected []fenceStyleTarget
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
			if err = charge(2*int64(len(code.Style))+2, 0); err != nil {
				return out, err
			}
			switch code.Style {
			case "indented":
				continue // Known indented owners need no fence metadata.
			case "fenced":
			default:
				return out, ErrModel
			}
			if err = charge(16, 0); err != nil {
				return out, err
			}
			if code.Fence.Present {
				switch code.Fence.Value {
				case '`':
					actual = "backtick"
				case '~':
					actual = "tilde"
				default:
					return out, ErrModel
				}
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
		selected = append(selected, fenceStyleTarget{node: n, style: actual})
	}
	if err = charge(12, 0); err != nil {
		return out, err
	}
	// Every exclusion subset of identical known styles is still consistent.
	// Unknown metadata disables this proof; it cannot elect a baseline.
	if len(selected) == 0 || style == "consistent" && allSame {
		return FenceStyleInventory{Availability: Available}, nil
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
				return FenceStyleInventory{Availability: Unavailable}, nil
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
			return FenceStyleInventory{Availability: Unavailable}, nil
		}
		if expected == "" {
			expected = target.style
		}
		if target.style == expected {
			continue
		}
		span, known, e := fenceStyleMarker(ctx, budget, index, n)
		if e != nil {
			return out, e
		}
		if !known {
			return FenceStyleInventory{Availability: Unavailable}, nil
		}
		out.Facts, err = shellPromptGrow(ctx, budget, out.Facts)
		if err != nil {
			return out, err
		}
		if err = charge(1, 1); err != nil {
			return out, err
		}
		out.Facts = append(out.Facts, FenceStyleFact{Span: span, Expected: expected})
	}
	out.Availability = Available
	return out, nil
}

// Only a selected mismatch needs the complete original opening mapping.
func fenceStyleMarker(ctx context.Context, budget *OperationBudget, index LineIndex, n *Node) (Span, bool, error) {
	if err := headingFactCharge(ctx, budget, 24, 0); err != nil {
		return Span{}, false, err
	}
	code := n.spec.Properties.Code
	if code == nil || !code.Fence.Present || !code.Width.Present || len(n.spec.Markers) == 0 || !n.spec.Markers[0].Source.present {
		return Span{}, false, nil
	}
	span, _, err := fencedLanguageOpening(ctx, budget, index, n)
	return span, err == nil, err
}
