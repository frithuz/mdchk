// Correction facts for MD088.

package parser

import (
	"context"
	"strings"
	"unicode/utf8"
)

// ASCIIPunctuationReplacement returns the ASCII spelling MD088 writes for one of
// its six scalars, and the empty string for any other rune.
func ASCIIPunctuationReplacement(r rune) string {
	switch r {
	case '‘', '’':
		return "'"
	case '“', '”':
		return `"`
	case '–':
		return "-"
	case '—':
		return "--"
	}
	return ""
}

// maxASCIIPunctuationReparses bounds the reparses of one
// ASCIIPunctuationCorrections call. Units still undecided when it is reached
// stay manual.
const maxASCIIPunctuationReparses = 64

// ASCIIPunctuationCorrections returns the ASCIIPunctuation facts whose scalar
// can be rewritten to its ASCII spelling. En and em dashes inside a Heading are
// left out, since the heading anchor keeps "-" but drops the scalar, and so are
// the dashes of a line shaped like a list marker, thematic break or setext
// underline (see dashLine), which no reparse could accept. The rest qualify when reparsing the source with them rewritten gives the same tree,
// compared through SameMeaningAcrossASCIIPunctuation. The dashes of one line
// are tried as one unit, so a thematic break, setext underline or delimiter row
// is never half-rewritten. All units are first reparsed together; when that
// fails, a greedy bisection keeps the accepted set and tries it with each half
// of the remaining units before splitting further, so the returned facts always
// passed one reparse together. At most maxASCIIPunctuationReparses reparses run.
func (d *Document) ASCIIPunctuationCorrections(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) ([]ASCIIPunctuationFact, error) {
	facts, err := d.ASCIIPunctuation(ctx, budget, index, options)
	if err != nil || len(facts) == 0 {
		return nil, err
	}
	headings, err := d.headingEnvelopes(ctx, budget)
	if err != nil {
		return nil, err
	}
	content := index.source.content
	var candidates []ASCIIPunctuationFact
	var units [][]int
	// line is the unit index of the current line's dashes, -1 before its first
	// dash and -2 when dashLine keeps them manual.
	line := -1
	previous, h := 0, 0
	for _, f := range facts {
		if err := headingFactCharge(ctx, budget, int64(f.Span.start-previous)+8, 1); err != nil {
			return nil, err
		}
		if strings.ContainsAny(content[previous:f.Span.start], "\r\n") {
			line = -1
		}
		previous = f.Span.start
		dash := f.Scalar == '–' || f.Scalar == '—'
		for h < len(headings) && headings[h].end <= f.Span.start {
			h++
		}
		if dash && line == -1 {
			shaped, err := dashLine(ctx, budget, content, f.Span)
			if err != nil {
				return nil, err
			}
			if shaped {
				line = -2
			}
		}
		if dash && (line == -2 || h < len(headings) && contains(headings[h], f.Span)) {
			continue
		}
		candidates = append(candidates, f)
		switch {
		case !dash:
			units = append(units, []int{len(candidates) - 1})
		case line == -1:
			line = len(units)
			units = append(units, []int{len(candidates) - 1})
		default:
			units[line] = append(units[line], len(candidates)-1)
		}
	}
	accepted := make([]bool, len(candidates))
	reparses := 0
	var search func([][]int) error
	search = func(units [][]int) error {
		if len(units) == 0 || reparses == maxASCIIPunctuationReparses {
			return nil
		}
		reparses++
		ok, err := d.asciiPunctuationPreserved(ctx, budget, options, candidates, accepted, units)
		if err != nil {
			return err
		}
		if ok {
			for _, u := range units {
				for _, i := range u {
					accepted[i] = true
				}
			}
			return nil
		}
		if len(units) == 1 {
			return nil
		}
		if err := search(units[:len(units)/2]); err != nil {
			return err
		}
		return search(units[len(units)/2:])
	}
	if err := search(units); err != nil {
		return nil, err
	}
	var out []ASCIIPunctuationFact
	for i, f := range candidates {
		if accepted[i] {
			out = append(out, f)
		}
	}
	return out, nil
}

// asciiPunctuationPreserved reparses the source with every accepted candidate
// and every candidate of units rewritten and compares the result with d.
func (d *Document) asciiPunctuationPreserved(ctx context.Context, budget *OperationBudget, options ParseOptions, candidates []ASCIIPunctuationFact, accepted []bool, units [][]int) (bool, error) {
	content := d.Source().content
	if err := headingFactCharge(ctx, budget, int64(len(content))*2+int64(len(candidates))*2+1, int64(len(candidates))+1); err != nil {
		return false, err
	}
	rewrite := make([]bool, len(candidates))
	copy(rewrite, accepted)
	for _, u := range units {
		for _, i := range u {
			rewrite[i] = true
		}
	}
	edited := make([]byte, 0, len(content))
	previous := 0
	for i, f := range candidates {
		if rewrite[i] {
			edited = append(append(edited, content[previous:f.Span.start]...), ASCIIPunctuationReplacement(f.Scalar)...)
			previous = f.Span.end
		}
	}
	edited = append(edited, content[previous:]...)
	after := ParseWithOptions(ctx, NewSource(edited), budget, options)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if after.Status != Complete || after.Document == nil || len(after.Uncertainty) != 0 {
		return false, nil
	}
	return d.SameMeaningAcrossASCIIPunctuation(ctx, budget, after.Document)
}

// dashLine reports whether the line holding the dash at s, after its leading
// spaces, tabs and ">", opens with an en dash before a space, tab or line end,
// or holds only dashes, hyphens, spaces and tabs. Hyphens there would open a
// list item or form a thematic break or setext underline.
func dashLine(ctx context.Context, budget *OperationBudget, content string, s Span) (bool, error) {
	start, end := strings.LastIndexAny(content[:s.start], "\r\n")+1, len(content)
	if i := strings.IndexAny(content[s.start:], "\r\n"); i >= 0 {
		end = s.start + i
	}
	if err := headingFactCharge(ctx, budget, int64(end-start)*2+1, 0); err != nil {
		return false, err
	}
	rest := strings.TrimLeft(content[start:end], " \t>")
	if r, width := utf8.DecodeRuneInString(rest); r == '–' && (width == len(rest) || rest[width] == ' ' || rest[width] == '\t') {
		return true, nil
	}
	return strings.Trim(rest, " \t-–—") == "", nil
}

// headingEnvelopes returns the envelope of every Heading in document order.
func (d *Document) headingEnvelopes(ctx context.Context, budget *OperationBudget) ([]Span, error) {
	var out []Span
	stack := []*Node{d.root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if err := headingFactCharge(ctx, budget, int64(len(n.spec.Children))+1, 0); err != nil {
			return nil, err
		}
		if n.Kind() == Heading {
			out = append(out, n.spec.Envelope)
			continue
		}
		for i := len(n.spec.Children) - 1; i >= 0; i-- {
			stack = append(stack, n.spec.Children[i])
		}
	}
	return out, nil
}

// equalAcrossASCIIPunctuation reports whether b is a with zero or more of
// MD088's six scalars replaced by their ASCII spelling.
func equalAcrossASCIIPunctuation(a, b string) bool {
	i, j := 0, 0
	for i < len(a) {
		if a[i] == 0xE2 {
			r, width := utf8.DecodeRuneInString(a[i:])
			if m := ASCIIPunctuationReplacement(r); m != "" && width == 3 && strings.HasPrefix(b[j:], m) {
				i, j = i+width, j+len(m)
				continue
			}
		}
		if j == len(b) || a[i] != b[j] {
			return false
		}
		i, j = i+1, j+1
	}
	return j == len(b)
}
