package parser

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// DestinationWhitespaceInventory contains only detached original candidate
// envelopes whose unparsed destination holds literal whitespace. Unknown
// provenance atomically withholds the complete inventory.
type DestinationWhitespaceInventory struct {
	Spans        []Span
	Availability Availability
}

type destinationWhitespaceScanner struct {
	*swappedLinkScanner
}

func destinationWhitespaceBlank(u swappedLinkUnit) bool {
	return u.exact && (u.value == " " || u.value == "\t")
}

// destination reports whether trimmed content holds a qualifying literal
// whitespace run that does not open an angle destination or a broken title.
func (w *destinationWhitespaceScanner) destination(units []swappedLinkUnit) (bool, error) {
	lo, hi := 0, len(units)
	for lo < hi && destinationWhitespaceBlank(units[lo]) {
		if err := w.charge(1, 0); err != nil {
			return false, err
		}
		lo++
	}
	for hi > lo && destinationWhitespaceBlank(units[hi-1]) {
		if err := w.charge(1, 0); err != nil {
			return false, err
		}
		hi--
	}
	if lo == hi || units[lo].exact && units[lo].value == "<" {
		return false, nil
	}
	// Trimming leaves a non-blank unit on both sides of every interior run.
	for i := lo + 1; i < hi; {
		if err := w.charge(2, 0); err != nil {
			return false, err
		}
		if !destinationWhitespaceBlank(units[i]) {
			i++
			continue
		}
		run := i
		for destinationWhitespaceBlank(units[i]) {
			if err := w.charge(1, 0); err != nil {
				return false, err
			}
			i++
		}
		if units[run-1].exact && units[run-1].value == `\` {
			continue
		}
		return !units[i].exact || units[i].value != `"` && units[i].value != "'", nil
	}
	return false, nil
}

// flush records flat inline-looking candidates of one island. Chains and
// candidates adjacent to links stay excluded rather than guessed.
func (w *destinationWhitespaceScanner) flush() error {
	units := w.units
	if w.unknown {
		// No candidate is recorded under unknown raw state; any possible one
		// withholds the inventory.
		for i := 1; i < len(units); i++ {
			if err := w.charge(4, 0); err != nil {
				return err
			}
			if units[i-1].syntax == ']' && units[i].syntax == '(' && units[i-1].end == units[i].start {
				w.uncertain = true
			}
		}
		return nil
	}
	for i := 0; ; {
		if err := w.charge(8, 0); err != nil {
			return err
		}
		if i == len(units) {
			return nil
		}
		if units[i].syntax != '[' {
			i++
			continue
		}
		end, flat, err := w.group(i)
		if err != nil {
			return err
		}
		if !flat || end == len(units) || units[end].syntax != '(' || units[end-1].end != units[end].start {
			i = end
			continue
		}
		last, flatDestination, err := w.group(end)
		if err != nil {
			return err
		}
		if !flatDestination {
			i = last
			continue
		}
		if err = w.charge(12, 0); err != nil {
			return err
		}
		first := i
		if first > 0 && units[first-1].syntax == '!' {
			first--
		}
		eligible := w.linkEnd == 0 || units[first].start != w.linkEnd
		if first > 0 && (units[first-1].syntax == ']' || units[first-1].syntax == ')') {
			eligible = false
		}
		if last < len(units) && (units[last].syntax == '(' || units[last].syntax == '[' || units[last].syntax == ')' || units[last].syntax == ']') {
			eligible = false
		}
		if eligible {
			found, e := w.destination(units[end+1 : last-1])
			if e != nil {
				return e
			}
			if found {
				w.spans, err = swappedLinkGrow(w.swappedLinkScanner, w.spans)
				if err != nil {
					return err
				}
				if err = w.charge(1, 1); err != nil {
					return err
				}
				w.spans = append(w.spans, Span{source: w.source, start: units[first].start, end: units[last-1].end, present: true})
			}
		}
		i = last
	}
}

// DestinationWhitespace observes unparsed inline-looking candidates whose
// destination holds literal whitespace. Parsed constructs win; original
// ownership and allowance remain binding.
func (d *Document) DestinationWhitespace(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out DestinationWhitespaceInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = DestinationWhitespaceInventory{}
		} else if out.Availability != Available {
			out.Spans = nil
		}
	}()
	// Exact adjacent outer bytes are necessary even with transformed payload.
	if err = headingFactCharge(ctx, budget, int64(index.source.Len())+1, 0); err != nil {
		return out, err
	}
	if !strings.Contains(index.source.content, "](") {
		return DestinationWhitespaceInventory{Availability: Available}, nil
	}
	w := destinationWhitespaceScanner{swappedLinkScanner: &swappedLinkScanner{ctx: ctx, budget: budget, source: index.source}}
	w.island = w.flush
	known, err := d.proseIslands(w.swappedLinkScanner, index)
	if err != nil {
		return out, err
	}
	if !known || w.uncertain {
		return DestinationWhitespaceInventory{Availability: Unavailable}, nil
	}
	// Source ordering is explicit; private HTML tails need not have global
	// preorder offsets. Reserve comparisons before sorting the detached output.
	err = nestedExampleLocked(ctx, budget, index, func(work *parserContext, _ LineIndex) error {
		slices.SortFunc(w.spans, func(a, b Span) int {
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
	return DestinationWhitespaceInventory{Spans: w.spans, Availability: Available}, nil
}
