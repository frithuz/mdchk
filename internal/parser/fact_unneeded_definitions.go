package parser

import (
	"context"
	"errors"
)

// UnneededDefinition is a detached original "[label]:" range. Shadowed
// distinguishes a later duplicate from a first definition no reference uses.
type UnneededDefinition struct {
	Span     Span
	Label    string
	Shadowed bool
}

// UnneededDefinitionInventory keeps source-ordered records. Shadowing depends
// only on definitions; unused records additionally need every parsed reference
// use. An unavailable set withholds all of its records.
type UnneededDefinitionInventory struct {
	Definitions []UnneededDefinition
	Shadowed    Availability
	Unused      Availability
}

// UnneededDefinitions classifies completed definitions against resolved
// full, collapsed and shortcut Link/Image nodes anywhere in the tree. Ignored
// entries use the parser's own label normalization and exempt only first
// definitions from unused records.
func (d *Document) UnneededDefinitions(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, ignored []string) (out UnneededDefinitionInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = UnneededDefinitionInventory{}
		}
	}()
	if err = headingFactCharge(ctx, budget, int64(len(d.references.Definitions)+len(ignored))+1, 0); err != nil {
		return out, err
	}
	if d.references.Availability != Available {
		return UnneededDefinitionInventory{Shadowed: Unavailable, Unused: Unavailable}, nil
	}
	err = nestedExampleLocked(ctx, budget, index, func(work *parserContext, _ LineIndex) error {
		out = UnneededDefinitionInventory{Shadowed: Available, Unused: d.references.LookupAvailability}
		if out.Unused != Available {
			out.Unused = Unavailable
		}
		used := make(map[int]bool)
		if out.Unused == Available {
			known, err := d.referenceUses(work, used)
			if err != nil {
				return err
			}
			if !known {
				out.Unused = Unavailable
			}
		}
		skip := make(map[string]bool)
		for _, entry := range ignored {
			meterWork(work, 1)
			if label := normalizeReferenceLabelContext(work, entry); label != "" {
				skip[label] = true
			}
		}
		first := make(map[string]int)
		for i, definition := range d.references.Definitions {
			meterWork(work, 4)
			if definition == nil || !definition.spec.NormalizedLabel.Present {
				out = UnneededDefinitionInventory{Shadowed: Unavailable, Unused: Unavailable}
				return nil
			}
			label := definition.spec.NormalizedLabel.Value
			meterWork(work, len(label))
			_, shadowed := first[label]
			if !shadowed {
				first[label] = i
			}
			if !shadowed && (out.Unused != Available || used[i] || skip[label]) {
				continue
			}
			span, err := definitionLabelSpan(index, definition.spec.Markers)
			if err != nil {
				return err
			}
			out.Definitions = nestedExampleAppend(work, out.Definitions, UnneededDefinition{Span: span, Label: label, Shadowed: shadowed})
		}
		return nil
	})
	if err != nil {
		return UnneededDefinitionInventory{}, err
	}
	return out, nil
}

// referenceUses marks the lookup index of every resolved reference node. A
// reference node without a resolved binding leaves the use set unknown.
func (d *Document) referenceUses(work *parserContext, used map[int]bool) (bool, error) {
	stack := meteredAppend(work, []*Node(nil), d.root)
	for len(stack) != 0 {
		meterWork(work, 1)
		if err := work.Err(); err != nil {
			return false, err
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			return false, ErrModel
		}
		stack = meteredAppend(work, stack, n.spec.Children...)
		stack = meteredAppend(work, stack, n.htmlTail...)
		if n.spec.Kind != Link && n.spec.Kind != Image {
			continue
		}
		p := n.spec.Properties.Link
		if p == nil {
			return false, nil
		}
		if !referenceOrigin(p.Origin) {
			continue
		}
		if p.Resolution != Available || !p.NormalizedLabel.Present {
			return false, nil
		}
		meterWork(work, len(p.NormalizedLabel.Value))
		i, found := d.references.Lookup[p.NormalizedLabel.Value]
		if !found || i < 0 || i >= len(d.references.Definitions) {
			return false, ErrModel
		}
		used[i] = true
	}
	return true, nil
}

// definitionLabelSpan selects the original label-open through
// definition-colon bytes of one definition.
func definitionLabelSpan(index LineIndex, markers []Marker) (Span, error) {
	var open, colon Span
	for _, m := range markers {
		switch m.Role {
		case "label-open":
			open = m.Source
		case "definition-colon":
			colon = m.Source
		}
	}
	if !open.present || !colon.present || open.source != index.source || colon.source != index.source || colon.end < open.start {
		return Span{}, ErrModel
	}
	return Span{source: index.source, start: open.start, end: colon.end, present: true}, nil
}
