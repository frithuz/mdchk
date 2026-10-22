package cli

import (
	"context"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"sort"
)

type physicalDiffLine struct{ text, ending string }
type linePair struct{ old, new int }

// buildFixDiff uses unique-line patience anchors and common edges. Unmatched
// regions are whole replacements; no quadratic edit-distance table or trace is
// constructed. Sorting is O(n log n), storage O(n), and actual byte scans,
// comparisons and records consume the same remaining operation meter.
func buildFixDiff(ctx context.Context, budget *parser.OperationBudget, before, after parser.Source) (result.Diff, error) {
	charge := func(n int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return budget.ChargeWork(n)
	}
	split := func(source parser.Source) ([]physicalDiffLine, error) {
		if err := charge(int64(source.Len())*3 + 1); err != nil {
			return nil, err
		}
		span, _ := source.Span(0, source.Len())
		text, _ := source.Text(span)
		count := 0
		for i := 0; i < len(text); i++ {
			if text[i] == '\r' {
				count++
				if i+1 < len(text) && text[i+1] == '\n' {
					i++
				}
			} else if text[i] == '\n' {
				count++
			}
		}
		if len(text) > 0 && text[len(text)-1] != '\n' && text[len(text)-1] != '\r' {
			count++
		}
		if err := budget.ChargeNodes(int64(count)*16 + 1); err != nil {
			return nil, err
		}
		lines := make([]physicalDiffLine, 0, count)
		start := 0
		for i := 0; i < len(text); i++ {
			ending := ""
			end := i
			if text[i] == '\r' {
				ending = "cr"
				if i+1 < len(text) && text[i+1] == '\n' {
					ending = "crlf"
					i++
				}
			} else if text[i] == '\n' {
				ending = "lf"
			}
			if ending != "" {
				lines = append(lines, physicalDiffLine{text[start:end], ending})
				start = i + 1
			}
		}
		if start < len(text) {
			lines = append(lines, physicalDiffLine{text[start:], "none"})
		}
		return lines, nil
	}
	old, err := split(before)
	if err != nil {
		return result.Diff{}, err
	}
	next, err := split(after)
	if err != nil {
		return result.Diff{}, err
	}
	equal := func(a, b physicalDiffLine) bool { return a == b }
	prefix := 0
	for prefix < len(old) && prefix < len(next) && equal(old[prefix], next[prefix]) {
		prefix++
	}
	suffix := 0
	for suffix < len(old)-prefix && suffix < len(next)-prefix && equal(old[len(old)-1-suffix], next[len(next)-1-suffix]) {
		suffix++
	}
	if err := charge(int64(len(old)+len(next))*16 + 1); err != nil {
		return result.Diff{}, err
	}
	oldCounts := map[physicalDiffLine]int{}
	newCounts := map[physicalDiffLine]int{}
	newPositions := map[physicalDiffLine]int{}
	for i := prefix; i < len(old)-suffix; i++ {
		if err := charge(int64(len(old[i].text)) + 1); err != nil {
			return result.Diff{}, err
		}
		oldCounts[old[i]]++
	}
	for i := prefix; i < len(next)-suffix; i++ {
		if err := charge(int64(len(next[i].text)) + 1); err != nil {
			return result.Diff{}, err
		}
		newCounts[next[i]]++
		newPositions[next[i]] = i
	}
	pairs := []linePair{}
	for i := prefix; i < len(old)-suffix; i++ {
		if err := charge(int64(len(old[i].text)) + 1); err != nil {
			return result.Diff{}, err
		}
		if oldCounts[old[i]] == 1 && newCounts[old[i]] == 1 {
			pairs = append(pairs, linePair{i, newPositions[old[i]]})
		}
	}
	tails := []int{}
	previous := make([]int, len(pairs))
	var stop error
	for i, p := range pairs {
		at := sort.Search(len(tails), func(j int) bool {
			if stop == nil {
				stop = charge(1)
			}
			return pairs[tails[j]].new >= p.new
		})
		if stop != nil {
			return result.Diff{}, stop
		}
		previous[i] = -1
		if at > 0 {
			previous[i] = tails[at-1]
		}
		if at == len(tails) {
			tails = append(tails, i)
		} else {
			tails[at] = i
		}
	}
	anchors := make([]linePair, len(tails))
	if len(tails) > 0 {
		at := tails[len(tails)-1]
		for i := len(anchors) - 1; i >= 0; i-- {
			anchors[i] = pairs[at]
			at = previous[at]
		}
	}
	records := make([]result.DiffLine, 0, len(old)+len(next))
	add := func(kind string, line physicalDiffLine) {
		records = append(records, result.DiffLine{Kind: kind, Text: line.text, LineEnding: line.ending})
	}
	for i := 0; i < prefix; i++ {
		add("context", old[i])
	}
	a, b := prefix, prefix
	for _, p := range anchors {
		for a < p.old {
			add("remove", old[a])
			a++
		}
		for b < p.new {
			add("add", next[b])
			b++
		}
		add("context", old[a])
		a++
		b++
	}
	for a < len(old)-suffix {
		add("remove", old[a])
		a++
	}
	for b < len(next)-suffix {
		add("add", next[b])
		b++
	}
	for a < len(old) {
		add("context", old[a])
		a++
	}
	diff := result.Diff{Format: "line-hunks", ContextLines: 3, Hunks: []result.DiffHunk{}}
	oldAt, newAt := 1, 1
	for i := 0; i < len(records); {
		if records[i].Kind == "context" {
			oldAt++
			newAt++
			i++
			continue
		}
		start := max(0, i-3)
		end := i + 1
		for end < len(records) {
			if records[end].Kind != "context" {
				end++
				continue
			}
			j := end
			for j < len(records) && records[j].Kind == "context" {
				j++
			}
			if j < len(records) && j-end <= 6 {
				end = j + 1
				continue
			}
			end = min(len(records), end+3)
			break
		}
		context := i - start
		h := result.DiffHunk{OldStart: oldAt - context, NewStart: newAt - context, Lines: append([]result.DiffLine{}, records[start:end]...)}
		for _, line := range h.Lines {
			if line.Kind != "add" {
				h.OldLines++
			}
			if line.Kind != "remove" {
				h.NewLines++
			}
		}
		if h.OldLines == 0 {
			h.OldStart--
		}
		if h.NewLines == 0 {
			h.NewStart--
		}
		diff.Hunks = append(diff.Hunks, h)
		for _, line := range records[i:end] {
			if line.Kind != "add" {
				oldAt++
			}
			if line.Kind != "remove" {
				newAt++
			}
		}
		i = end
	}
	return diff, charge(0)
}
