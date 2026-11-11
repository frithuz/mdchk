package parser

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// UndefinedReferenceInventory contains only detached original candidate
// envelopes whose label misses the completed definition lookup. Unknown
// provenance or definitions atomically withhold the complete inventory.
type UndefinedReferenceInventory struct {
	Spans        []Span
	Availability Availability
}

type undefinedReferenceCandidate struct {
	start, end, keyStart, keyEnd int
}

type undefinedReferenceScanner struct {
	*swappedLinkScanner
	candidates []undefinedReferenceCandidate
}

// bracket consumes one top-level bracket group once. Nested groups are not
// flat; an unmatched opening consumes the island remainder.
func (u *undefinedReferenceScanner) bracket(start int) (end int, flat bool, err error) {
	depth := 0
	flat = true
	for i := start; ; i++ {
		if err = u.charge(8, 0); err != nil {
			return 0, false, err
		}
		if i == len(u.units) {
			return i, false, nil
		}
		switch u.units[i].syntax {
		case '[':
			depth++
			if depth > 1 {
				flat = false
			}
		case ']':
			depth--
			if depth == 0 {
				return i + 1, flat, nil
			}
		default:
			// Escaped, decoded and other payload cannot establish delimiters.
		}
	}
}

// flush records full and collapsed candidates of one island. Chains and
// candidates adjacent to links stay excluded rather than guessed.
func (u *undefinedReferenceScanner) flush() error {
	units := u.units
	if u.unknown {
		// No candidate is recorded under unknown raw state; any possible one
		// withholds the inventory.
		for i := 1; i < len(units); i++ {
			if err := u.charge(4, 0); err != nil {
				return err
			}
			if units[i-1].syntax == ']' && units[i].syntax == '[' {
				u.uncertain = true
			}
		}
		return nil
	}
	for i := 0; ; {
		if err := u.charge(8, 0); err != nil {
			return err
		}
		if i == len(units) {
			return nil
		}
		if units[i].syntax != '[' {
			i++
			continue
		}
		end, flat, err := u.bracket(i)
		if err != nil {
			return err
		}
		if !flat || end == len(units) || units[end].syntax != '[' || units[end-1].end != units[end].start {
			i = end
			continue
		}
		last, flatLabel, err := u.bracket(end)
		if err != nil {
			return err
		}
		if !flatLabel {
			i = last
			continue
		}
		if err = u.charge(12, 0); err != nil {
			return err
		}
		first := i
		if first > 0 && units[first-1].syntax == '!' {
			first--
		}
		eligible := u.linkEnd == 0 || units[first].start != u.linkEnd
		if first > 0 && (units[first-1].syntax == ']' || units[first-1].syntax == ')') {
			eligible = false
		}
		if last < len(units) && (units[last].syntax == '[' || units[last].syntax == '(') {
			eligible = false
		}
		if eligible {
			c := undefinedReferenceCandidate{start: units[first].start, end: units[last-1].end, keyStart: units[end].end, keyEnd: units[last-1].start}
			if last-end == 2 {
				c.keyStart, c.keyEnd = units[i].end, units[end-1].start
			}
			u.candidates, err = swappedLinkGrow(u.swappedLinkScanner, u.candidates)
			if err != nil {
				return err
			}
			if err = u.charge(1, 1); err != nil {
				return err
			}
			u.candidates = append(u.candidates, c)
		}
		i = last
	}
}

// UndefinedReferences observes full and collapsed reference candidates whose
// labels have no definition. Parsed constructs win; original ownership and
// the completed definition lookup remain binding.
func (d *Document) UndefinedReferences(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out UndefinedReferenceInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = UndefinedReferenceInventory{}
		} else if out.Availability != Available {
			out.Spans = nil
		}
	}()
	// Exact adjacent outer bytes are necessary even with transformed payload.
	if err = headingFactCharge(ctx, budget, int64(index.source.Len())+1, 0); err != nil {
		return out, err
	}
	if d.references.Availability != Available || d.references.LookupAvailability != Available {
		return UndefinedReferenceInventory{Availability: Unavailable}, nil
	}
	if !strings.Contains(index.source.content, "][") {
		return UndefinedReferenceInventory{Availability: Available}, nil
	}
	u := undefinedReferenceScanner{swappedLinkScanner: &swappedLinkScanner{ctx: ctx, budget: budget, source: index.source}}
	u.island = u.flush
	known, err := d.proseIslands(u.swappedLinkScanner, index)
	if err != nil {
		return out, err
	}
	if !known || u.uncertain {
		return UndefinedReferenceInventory{Availability: Unavailable}, nil
	}
	// Label grammar, folding and lookup use the parser's own metered helpers.
	// Source ordering is explicit; private HTML tails need not have global
	// preorder offsets. Reserve comparisons before sorting the detached output.
	var spans []Span
	err = nestedExampleLocked(ctx, budget, index, func(work *parserContext, _ LineIndex) error {
		for _, c := range u.candidates {
			meterWork(work, 4)
			// An interrupted grammar scan rejects its label; report the stop instead.
			end, valid := referenceLabelEndContext(work, index.source.content, c.keyStart)
			if err := work.Err(); err != nil {
				return err
			}
			if !valid || end != c.keyEnd {
				continue
			}
			normalized := normalizeReferenceLabelContext(work, index.source.content[c.keyStart:c.keyEnd])
			meterWork(work, len(normalized))
			if _, found := d.references.Lookup[normalized]; found {
				continue
			}
			spans = nestedExampleAppend(work, spans, Span{source: index.source, start: c.start, end: c.end, present: true})
		}
		slices.SortFunc(spans, func(a, b Span) int {
			meterWork(work, 4)
			if a.start < b.start {
				return -1
			}
			if a.start > b.start {
				return 1
			}
			return 0
		})
		return nil
	})
	if err != nil {
		return out, err
	}
	return UndefinedReferenceInventory{Spans: spans, Availability: Available}, nil
}
