// Package headingstyle produces and checks the MD003 corrections that rewrite
// only a heading's delimiters to the expected syntax style.
package headingstyle

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/result"
)

// Edit is one delimiter change of a rewrite, in original coordinates.
type Edit struct {
	Span        parser.Span
	Replacement string
}

// Actual names a heading's style as MD003 does.
func Actual(h parser.HeadingFact) string {
	if h.Closed {
		return "atx_closed"
	}
	return h.Style
}

// Rewrite returns the delimiter-only edits that turn h into the target style,
// in source order, and the delimiter span whose context decides eligibility.
// It reports false for every case the correction policy defers: Setext targets
// above level 2, empty Setext targets, multiline Setext text, list containers
// or lazy prefixes before a new underline, content whose trailing hashes would
// become a closing sequence, content that could start another block, and an
// ATX heading joining a preceding paragraph line.
func Rewrite(ctx context.Context, source parser.Source, h parser.HeadingFact, target string, checkpoint func(context.Context, int64) error) ([]Edit, parser.Span, bool, error) {
	actual := Actual(h)
	if actual == target || (target != "atx" && target != "atx_closed" && target != "setext") {
		return nil, parser.Span{}, false, nil
	}
	if err := checkpoint(ctx, int64(h.Envelope.End()-h.Envelope.Start())+2); err != nil {
		return nil, parser.Span{}, false, err
	}
	all, err := source.Span(0, source.Len())
	if err != nil {
		return nil, parser.Span{}, false, err
	}
	text, err := source.Text(all)
	if err != nil {
		return nil, parser.Span{}, false, err
	}
	content, err := source.Text(h.Label)
	if err != nil {
		return nil, parser.Span{}, false, err
	}
	span := func(start, end int) parser.Span {
		s, _ := source.Span(start, end)
		return s
	}
	if h.Style == "setext" {
		// The text is the single line holding the label; the underline line follows
		// its line ending and runs to the envelope end.
		if strings.ContainsAny(content, "\r\n") || content == "" || strings.HasSuffix(content, "#") {
			return nil, parser.Span{}, false, nil
		}
		underline := h.Label.End()
		for underline < h.Envelope.End() && text[underline] != '\n' && text[underline] != '\r' {
			underline++
		}
		if strings.HasPrefix(text[underline:], "\r\n") {
			underline += 2
		} else if underline < h.Envelope.End() {
			underline++
		}
		if underline >= h.Envelope.End() {
			return nil, parser.Span{}, false, nil
		}
		hashes := strings.Repeat("#", h.Level)
		edits := []Edit{{span(h.Label.Start(), h.Label.Start()), hashes + " "}}
		if target == "atx_closed" {
			edits = append(edits, Edit{span(h.Label.End(), h.Label.End()), " " + hashes})
		}
		edits = append(edits, Edit{span(underline, h.Envelope.End()), ""})
		return edits, span(underline, lineEnd(text, underline, h.Envelope.End())), true, nil
	}
	open := h.Envelope.Start()
	for open < h.Envelope.End() && text[open] == ' ' {
		open++
	}
	openEnd := open
	for openEnd < h.Envelope.End() && text[openEnd] == '#' {
		openEnd++
	}
	if openEnd-open != h.Level {
		return nil, parser.Span{}, false, nil
	}
	delimiter := span(open, openEnd)
	// contentEnd is where an open heading's content stops: the label end, or the
	// opening run end of an empty heading. closingEnd ends the parser's closing run.
	contentEnd, closingEnd := h.Label.End(), h.Label.End()
	if h.Closed {
		gap := h.ClosedClosingGap
		if !h.HasRawContent {
			gap = h.ClosedOpeningGap
		}
		contentEnd, closingEnd = gap.Start(), gap.End()
		for closingEnd < h.Envelope.End() && text[closingEnd] == '#' {
			closingEnd++
		}
		if closingEnd == gap.End() {
			return nil, parser.Span{}, false, nil
		}
	}
	switch target {
	case "atx_closed":
		return []Edit{{span(contentEnd, contentEnd), " " + strings.Repeat("#", h.Level)}}, delimiter, true, nil
	case "atx":
		if h.HasRawContent && closingHashes(content) {
			return nil, parser.Span{}, false, nil
		}
		return []Edit{{span(contentEnd, closingEnd), ""}}, delimiter, true, nil
	}
	if h.Level > 2 || !h.HasRawContent || !setextSafe(content) || slices.ContainsFunc(h.Containers, func(c parser.HeadingContainer) bool { return c.Kind != parser.BlockQuote }) {
		return nil, parser.Span{}, false, nil
	}
	line := open
	for line > 0 && text[line-1] != '\n' && text[line-1] != '\r' {
		line--
	}
	prefix := text[line:open]
	if strings.Trim(prefix, " >") != "" || strings.Count(prefix, ">") != len(h.Containers) {
		return nil, parser.Span{}, false, nil
	}
	if err := checkpoint(ctx, int64(open-line)+1); err != nil {
		return nil, parser.Span{}, false, err
	}
	// The line before must not be paragraph text the new Setext text would join.
	if line > 0 {
		previous := line - 1
		if previous > 0 && text[previous] == '\n' && text[previous-1] == '\r' {
			previous--
		}
		start := previous
		for start > 0 && text[start-1] != '\n' && text[start-1] != '\r' {
			start--
		}
		if err := checkpoint(ctx, int64(previous-start)+1); err != nil {
			return nil, parser.Span{}, false, err
		}
		if strings.Trim(text[start:previous], " \t>") != "" {
			return nil, parser.Span{}, false, nil
		}
	}
	end := lineEnd(text, open, h.Envelope.End())
	ending := text[end:h.Envelope.End()]
	if ending == "" {
		return nil, parser.Span{}, false, nil
	}
	marker := "="
	if h.Level == 2 {
		marker = "-"
	}
	edits := []Edit{{span(open, h.Label.Start()), ""}}
	if h.Closed {
		edits = append(edits, Edit{span(contentEnd, closingEnd), ""})
	}
	edits = append(edits, Edit{span(h.Envelope.End(), h.Envelope.End()), prefix + strings.Repeat(marker, max(3, utf8.RuneCountInString(content))) + ending})
	return edits, delimiter, true, nil
}

// lineEnd returns the offset of the line ending that follows at, or limit.
func lineEnd(text string, at, limit int) int {
	for at < limit && text[at] != '\n' && text[at] != '\r' {
		at++
	}
	return at
}

// closingHashes reports content whose final hash run an open ATX heading would
// read as a closing sequence.
func closingHashes(content string) bool {
	trimmed := strings.TrimRight(content, "#")
	return len(trimmed) != len(content) && (trimmed == "" || strings.HasSuffix(trimmed, " ") || strings.HasSuffix(trimmed, "\t"))
}

// setextSafe rejects content that could open another block or read as an
// underline once it stands alone on a line.
func setextSafe(content string) bool {
	if strings.ContainsAny(content[:1], "-+*>#=`~<|_") || content[0] >= '0' && content[0] <= '9' {
		return false
	}
	return !closingHashes(content)
}

// Target reads the expected style from an MD003 finding message.
func Target(message string) string {
	return strings.TrimSuffix(strings.TrimPrefix(message, "Heading syntax must be "), ".")
}

// Groups regenerates one group for each eligible active MD003 finding from this
// round's heading facts.
func Groups(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	if len(findings) == 0 {
		return nil, nil
	}
	headings, err := c.Headings(ctx)
	if err != nil {
		return nil, err
	}
	byStart := make(map[int]parser.HeadingFact, len(headings))
	for _, h := range headings {
		if err := c.Checkpoint(ctx, 1); err != nil {
			return nil, err
		}
		byStart[h.Envelope.Start()] = h
	}
	groups := make([]fix.Group, 0, len(findings))
	for _, d := range findings {
		if err := c.Reserve(ctx, 128, 16); err != nil {
			return nil, err
		}
		h, ok := byStart[d.ByteRange.Start]
		if !ok || h.Envelope.End() != d.ByteRange.End {
			continue
		}
		rewrite, _, ok, err := Rewrite(ctx, c.Current(), h, Target(d.Message), c.Checkpoint)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		edits := make([]fix.Edit, len(rewrite))
		for i, e := range rewrite {
			expected, err := c.Current().Text(e.Span)
			if err != nil {
				return nil, err
			}
			edits[i] = fix.Edit{Span: e.Span, Expected: []byte(expected), Replacement: []byte(e.Replacement)}
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD003-style-%d", h.Envelope.Start()), Rule: "MD003", Provenance: "MD003 heading delimiter style", Anchor: h.Envelope, Edits: edits, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, nil
}

// styleRound holds the facts every MD003 group of a round shares.
type styleRound struct {
	rewrites  map[int][]Edit
	preserved bool
}

// Check authorizes one group against the before parse, never against producer
// data. The round proof requires that the candidate reparses with the same
// block structure and the same headings, levels, visible text, containers and
// anchors, and that the round's edits are exactly the canonical rewrites of the
// headings whose style changed. The group must then be its heading's rewrite.
func Check(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("heading-style", func() (any, error) { return styleProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(styleRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	want, ok := round.rewrites[g.Anchor.Start()]
	if g.Rule != "MD003" || !ok || len(g.Edits) != len(want) {
		return fix.Unsafe, nil
	}
	for i, e := range g.Edits {
		expected, err := c.Before.Current().Text(want[i].Span)
		if err != nil {
			return fix.Unknown, err
		}
		if err := c.Before.Checkpoint(ctx, int64(len(expected)+len(e.Replacement))+1); err != nil {
			return fix.Unknown, err
		}
		if e.Span != want[i].Span || string(e.Expected) != expected || string(e.Replacement) != want[i].Replacement {
			return fix.Unsafe, nil
		}
	}
	return fix.Preserved, nil
}

func styleProof(ctx context.Context, c fix.CheckContext) (styleRound, error) {
	failed := styleRound{}
	unchanged, err := c.OnlyEditsChanged(ctx)
	if err != nil || !unchanged {
		return failed, err
	}
	if same, err := sameBlocks(ctx, c); err != nil || !same {
		return failed, err
	}
	before, err := c.Before.Headings(ctx)
	if err != nil {
		return failed, err
	}
	after, err := c.After.Headings(ctx)
	if err != nil {
		return failed, err
	}
	if len(before) != len(after) {
		return failed, nil
	}
	rewrites := map[int][]Edit{}
	var expected []Edit
	for i, b := range before {
		a := after[i]
		if err := c.Before.Checkpoint(ctx, int64(len(b.Visible)+len(a.Visible)+len(b.Containers))+1); err != nil {
			return failed, err
		}
		if b.Level != a.Level || b.HasRawContent != a.HasRawContent || b.Visible != a.Visible || len(b.Containers) != len(a.Containers) {
			return failed, nil
		}
		for j, container := range b.Containers {
			if container.Kind != a.Containers[j].Kind {
				return failed, nil
			}
		}
		if Actual(b) == Actual(a) {
			continue
		}
		edits, _, ok, err := Rewrite(ctx, c.Before.Current(), b, Actual(a), c.Before.Checkpoint)
		if err != nil || !ok {
			return failed, err
		}
		rewrites[b.Envelope.Start()] = edits
		expected = append(expected, edits...)
	}
	slices.SortFunc(expected, func(x, y Edit) int {
		if n := cmp.Compare(x.Span.Start(), y.Span.Start()); n != 0 {
			return n
		}
		return cmp.Compare(x.Span.End(), y.Span.End())
	})
	actual := c.Edits()
	if len(actual) != len(expected) {
		return failed, nil
	}
	for i, e := range actual {
		if err := c.Before.Checkpoint(ctx, int64(len(e.Edit.Replacement))+1); err != nil {
			return failed, err
		}
		if e.Edit.Span != expected[i].Span || string(e.Edit.Replacement) != expected[i].Replacement {
			return failed, nil
		}
	}
	if same, err := sameHeadingAnchors(ctx, c); err != nil || !same {
		return failed, err
	}
	return styleRound{rewrites, true}, nil
}

// sameBlocks compares the block node trees of both parses: kinds, nesting and
// order. Inline content is compared through the heading facts instead.
func sameBlocks(ctx context.Context, c fix.CheckContext) (bool, error) {
	var walk func(a, b *parser.Node) (bool, error)
	walk = func(a, b *parser.Node) (bool, error) {
		if a.Kind() != b.Kind() {
			return false, nil
		}
		if a.Kind() == parser.Heading || a.Kind() == parser.Paragraph || a.Kind() == parser.TableCell {
			return true, nil
		}
		x, y := a.Children(), b.Children()
		if err := c.Before.Checkpoint(ctx, int64(len(x)+len(y))+1); err != nil {
			return false, err
		}
		if len(x) != len(y) {
			return false, nil
		}
		for i := range x {
			if same, err := walk(x[i], y[i]); err != nil || !same {
				return same, err
			}
		}
		return true, nil
	}
	before, after := c.Before.Parsed().Document, c.After.Parsed().Document
	if before == nil || after == nil {
		return false, nil
	}
	return walk(before.Root(), after.Root())
}

func sameHeadingAnchors(ctx context.Context, c fix.CheckContext) (bool, error) {
	before, availability, err := c.Before.HeadingAnchors(ctx)
	if err != nil || availability != resource.Available {
		return false, err
	}
	after, availability, err := c.After.HeadingAnchors(ctx)
	if err != nil || availability != resource.Available {
		return false, err
	}
	if len(before) != len(after) {
		return false, nil
	}
	for i, b := range before {
		if err := c.Before.Checkpoint(ctx, int64(len(b.Base.Value)+len(after[i].Base.Value))+1); err != nil {
			return false, err
		}
		if b.Base != after[i].Base {
			return false, nil
		}
	}
	return true, nil
}
