// Correction facts for MD088.

package parser

import (
	"context"
	"slices"
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

// One ASCIIPunctuationCorrections call runs at most
// maxASCIIPunctuationReparses reparses and, after the first, reparses at most
// maxASCIIPunctuationReparseBytes source bytes in all, so its cost stays a
// fixed share of the operation budget whatever the document size. When either
// bound stops the search before every unit is decided, every candidate stays
// manual, so later fix rounds don't repeat the search over the rest.
const (
	maxASCIIPunctuationReparses     = 64
	maxASCIIPunctuationReparseBytes = 1 << 17
)

// ASCIIPunctuationCorrections returns the ASCIIPunctuation facts whose scalar
// can be rewritten to its ASCII spelling. These are left out before any
// reparse: en and em dashes inside a Heading, since the heading anchor keeps "-"
// but drops the scalar; a scalar after an odd run of backslashes, which would
// become an escape; every dash of a line shaped like a list marker, thematic
// break, setext underline or GFM delimiter row (see dashLine), or holding an
// escaped dash or a dash right after "<!" or "<!-", which would open a comment;
// and in GFM a scalar whose run of non-whitespace bytes holds "www.", "://" or
// "@", which a bare autolink could grow over. The rest qualify when reparsing
// the source with them rewritten gives the same tree, compared through
// SameMeaningAcrossASCIIPunctuation. The dashes of one line are tried as one
// unit, so a line is never half-rewritten. All units are first reparsed
// together; when that fails, a greedy bisection keeps the accepted set and
// tries it with each half of the remaining units before splitting further, so
// the returned facts always passed one reparse together, or are none when the
// reparse bounds above stop the search.
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
	// dash and -2 when they all stay manual.
	line := -1
	previous, h := 0, 0
	run, linked := Span{}, false // the last non-whitespace run checked for autolinks
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
			shaped, err := dashLine(ctx, budget, content, f.Span, options.GFM)
			if err != nil {
				return nil, err
			}
			if shaped {
				line = -2
			}
		}
		escaped, err := afterEscape(ctx, budget, content, f.Span)
		if err != nil {
			return nil, err
		}
		if options.GFM && !escaped && (f.Span.start >= run.end || f.Span.start < run.start || run.end == 0) {
			if run, linked, err = autolinkRun(ctx, budget, content, f.Span); err != nil {
				return nil, err
			}
		}
		escaped = escaped || options.GFM && linked
		if dash && (escaped || strings.HasSuffix(content[:f.Span.start], "<!") || strings.HasSuffix(content[:f.Span.start], "<!-")) {
			if line >= 0 {
				units[line] = nil
			}
			line = -2
		}
		if escaped || dash && (line == -2 || h < len(headings) && contains(headings[h], f.Span)) {
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
	units = slices.DeleteFunc(units, func(u []int) bool { return u == nil })
	accepted := make([]bool, len(candidates))
	reparses, allowed, cut := 0, min(maxASCIIPunctuationReparses, 1+maxASCIIPunctuationReparseBytes/max(len(content), 1)), false
	var search func([][]int) error
	search = func(units [][]int) error {
		if len(units) == 0 {
			return nil
		}
		if reparses == allowed {
			cut = true
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
	if err := search(units); err != nil || cut {
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
// or holds only dashes, hyphens, spaces and tabs, and in GFM also "|" and ":".
// Hyphens there would open a list item or form a thematic break, setext
// underline or table delimiter row.
func dashLine(ctx context.Context, budget *OperationBudget, content string, s Span, gfm bool) (bool, error) {
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
	cutset := " \t-–—"
	if gfm {
		cutset += "|:"
	}
	return strings.Trim(rest, cutset) == "", nil
}

// autolinkRun returns the run of non-whitespace bytes around s and whether it
// holds "www.", "://" or "@", the anchors of GFM bare autolinks.
func autolinkRun(ctx context.Context, budget *OperationBudget, content string, s Span) (Span, bool, error) {
	start := strings.LastIndexAny(content[:s.start], " \t\r\n") + 1
	end := len(content)
	if i := strings.IndexAny(content[s.end:], " \t\r\n"); i >= 0 {
		end = s.end + i
	}
	if err := headingFactCharge(ctx, budget, int64(end-start)*4+1, 0); err != nil {
		return Span{}, false, err
	}
	text := content[start:end]
	return Span{start: start, end: end}, strings.Contains(text, "www.") || strings.Contains(text, "://") || strings.Contains(text, "@"), nil
}

// afterEscape reports whether an odd run of backslashes ends right before s, so
// that the first byte of an ASCII mapping there would be escaped.
func afterEscape(ctx context.Context, budget *OperationBudget, content string, s Span) (bool, error) {
	i := s.start
	for i > 0 && content[i-1] == '\\' {
		i--
	}
	if err := headingFactCharge(ctx, budget, int64(s.start-i)+1, 0); err != nil {
		return false, err
	}
	return (s.start-i)%2 == 1, nil
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
